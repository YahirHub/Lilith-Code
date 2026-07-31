package imageocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strings"
)

const maxImageBytes = 64 << 20

var (
	nativeOCR    nativeRecognizer = recognizeNative
	tesseractOCR                  = recognizeTesseract
)

// Recognize runs the platform-native OCR backend first and falls back to a
// locally installed Tesseract executable. No native library is linked into the
// Lilith process, so CGO_ENABLED=0 remains supported.
func Recognize(ctx context.Context, opts Options) (Result, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return Result{}, errors.New("image path is required")
	}
	data, err := readBounded(path, maxImageBytes)
	if err != nil {
		return Result{}, err
	}

	img, _, decodeErr := image.Decode(bytes.NewReader(data))
	decodedWidth, decodedHeight := 0, 0
	if decodeErr == nil {
		bounds := img.Bounds()
		decodedWidth, decodedHeight = bounds.Dx(), bounds.Dy()
	}

	text, width, height, words, nativeErr := nativeOCR(ctx, data, opts.Languages)
	backend := "windows.media.ocr"
	if nativeErr != nil {
		text, width, height, words, err = tesseractOCR(ctx, path, opts.Languages, opts.ConfigDir)
		backend = "tesseract.tsv"
		if err != nil {
			return Result{}, fmt.Errorf("OCR unavailable: native backend: %v; Tesseract fallback: %w", nativeErr, err)
		}
	}
	if width <= 0 {
		width = decodedWidth
	}
	if height <= 0 {
		height = decodedHeight
	}
	if width <= 0 || height <= 0 {
		return Result{}, errors.New("OCR completed but image dimensions could not be determined")
	}
	words = sanitizeWords(words, width, height)
	if strings.TrimSpace(text) == "" {
		text = readingOrderText(words)
	}

	result := Result{
		Backend: backend,
		Width:   width,
		Height:  height,
		Text:    strings.TrimSpace(text),
		Words:   words,
	}
	if decodeErr == nil {
		result.Separators = detectSeparators(img)
	}
	return result, nil
}

func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > max {
		return nil, fmt.Errorf("image exceeds %d MiB", max>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("image exceeds %d MiB", max>>20)
	}
	return data, nil
}

func sanitizeWords(words []Word, width, height int) []Word {
	out := make([]Word, 0, len(words))
	for _, word := range words {
		word.Text = strings.TrimSpace(word.Text)
		if word.Text == "" || word.Box.Width <= 0 || word.Box.Height <= 0 {
			continue
		}
		word.Box.X = clamp(word.Box.X, 0, float64(width))
		word.Box.Y = clamp(word.Box.Y, 0, float64(height))
		word.Box.Width = clamp(word.Box.Width, 0, float64(width)-word.Box.X)
		word.Box.Height = clamp(word.Box.Height, 0, float64(height)-word.Box.Y)
		if word.Box.Width == 0 || word.Box.Height == 0 {
			continue
		}
		out = append(out, word)
	}
	return out
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
