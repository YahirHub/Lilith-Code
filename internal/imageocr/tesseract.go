package imageocr

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lilith/li/internal/toolchain"
)

func recognizeTesseract(ctx context.Context, path string, languages []string, configDir string) (string, int, int, []Word, error) {
	binary := lookupTesseract(configDir)
	if binary == "" {
		return "", 0, 0, nil, errors.New("tesseract executable not found in the configured tools directory or PATH")
	}
	args := []string{path, "stdout"}
	if lang := normalizeTesseractLanguages(languages); lang != "" {
		args = append(args, "-l", lang)
	}
	// Sparse-text mode works better for application screenshots and preserves
	// independent labels that do not form a prose paragraph.
	args = append(args, "--psm", "11", "tsv")
	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", 0, 0, nil, fmt.Errorf("tesseract failed: %s", msg)
	}
	return parseTesseractTSV(out)
}

func lookupTesseract(configDir string) string {
	executable := "tesseract"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	var candidates []string
	if toolsDir := strings.TrimSpace(os.Getenv("LI_TOOLS_DIR")); toolsDir != "" {
		candidates = append(candidates, filepath.Join(toolsDir, executable))
	}
	if strings.TrimSpace(configDir) != "" {
		candidates = append(candidates, filepath.Join(configDir, "tools", "bin", executable))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	return toolchain.Lookup("tesseract")
}

func normalizeTesseractLanguages(languages []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(languages))
	for _, language := range languages {
		language = strings.TrimSpace(language)
		if language == "" || seen[language] {
			continue
		}
		seen[language] = true
		out = append(out, language)
	}
	return strings.Join(out, "+")
}

func parseTesseractTSV(data []byte) (string, int, int, []Word, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	first := true
	width, height := 0, 0
	words := make([]Word, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			if strings.HasPrefix(strings.ToLower(line), "level\t") {
				continue
			}
		}
		fields := strings.SplitN(line, "\t", 12)
		if len(fields) < 11 {
			continue
		}
		level, _ := strconv.Atoi(fields[0])
		left, errLeft := strconv.Atoi(fields[6])
		top, errTop := strconv.Atoi(fields[7])
		boxWidth, errWidth := strconv.Atoi(fields[8])
		boxHeight, errHeight := strconv.Atoi(fields[9])
		if errLeft != nil || errTop != nil || errWidth != nil || errHeight != nil {
			continue
		}
		if level == 1 && boxWidth > 0 && boxHeight > 0 {
			width, height = boxWidth, boxHeight
		}
		if level != 5 || len(fields) < 12 {
			continue
		}
		text := strings.TrimSpace(fields[11])
		if text == "" {
			continue
		}
		confidence, _ := strconv.ParseFloat(fields[10], 64)
		words = append(words, Word{
			Text:       text,
			Confidence: confidence,
			Box: Box{
				X:      float64(left),
				Y:      float64(top),
				Width:  float64(boxWidth),
				Height: float64(boxHeight),
			},
		})
	}
	if err := scanner.Err(); err != nil {
		return "", 0, 0, nil, err
	}
	if len(words) == 0 && (width <= 0 || height <= 0) {
		return "", width, height, nil, errors.New("tesseract returned neither words nor image dimensions")
	}
	return readingOrderText(words), width, height, words, nil
}
