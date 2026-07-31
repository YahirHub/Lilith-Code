//go:build windows

package imageocr

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lilith/li/internal/imageocr/winrt"
)

func recognizeNative(ctx context.Context, data []byte, languages []string) (string, int, int, []Word, error) {
	// WinRT apartment initialization is thread-local. Keep every COM call on
	// the same OS thread for the complete recognition sequence.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	_ = languages // WinRT uses the recognition languages installed in the user profile.
	if err := ctx.Err(); err != nil {
		return "", 0, 0, nil, err
	}
	if err := winrt.Initialize(); err != nil {
		return "", 0, 0, nil, err
	}

	stream, err := winrt.CreateInMemoryRandomAccessStream()
	if err != nil {
		return "", 0, 0, nil, errors.New("create WinRT memory stream: " + err.Error())
	}
	defer stream.Release()

	writerFactory, err := winrt.GetDataWriterFactory()
	if err != nil {
		return "", 0, 0, nil, errors.New("get WinRT DataWriter factory: " + err.Error())
	}
	defer writerFactory.Release()

	writer, err := writerFactory.CreateDataWriter(stream)
	if err != nil {
		return "", 0, 0, nil, errors.New("create WinRT DataWriter: " + err.Error())
	}
	defer writer.Release()
	writer.WriteBytes(data)

	storeOp, err := writer.StoreAsync()
	if err != nil {
		return "", 0, 0, nil, errors.New("store image in WinRT stream: " + err.Error())
	}
	if err := waitAsync(ctx, storeOp, 30*time.Second); err != nil {
		storeOp.Release()
		return "", 0, 0, nil, errors.New("store image in WinRT stream: " + err.Error())
	}
	storeOp.Release()

	flushOp, err := writer.FlushAsync()
	if err != nil {
		return "", 0, 0, nil, errors.New("flush WinRT stream: " + err.Error())
	}
	if err := waitAsync(ctx, flushOp, 30*time.Second); err != nil {
		flushOp.Release()
		return "", 0, 0, nil, errors.New("flush WinRT stream: " + err.Error())
	}
	flushOp.Release()
	writer.DetachStream()

	_, _, _ = syscallN(stream.VTable().Seek, uintptr(unsafe.Pointer(stream)), 0)

	decoderStatics, err := winrt.GetBitmapDecoderStatics()
	if err != nil {
		return "", 0, 0, nil, errors.New("get WinRT BitmapDecoder: " + err.Error())
	}
	defer decoderStatics.Release()

	var createOp *winrt.IAsyncOperation
	hr, _, _ := syscallN(
		decoderStatics.VTable().CreateAsync,
		uintptr(unsafe.Pointer(decoderStatics)),
		uintptr(unsafe.Pointer(stream)),
		uintptr(unsafe.Pointer(&createOp)),
	)
	if hr != 0 || createOp == nil {
		return "", 0, 0, nil, errors.New("start WinRT BitmapDecoder")
	}
	defer createOp.Release()
	if err := waitAsync(ctx, createOp, 30*time.Second); err != nil {
		return "", 0, 0, nil, errors.New("decode image with WinRT: " + err.Error())
	}
	decoderInspectable, err := createOp.GetResults()
	if err != nil || decoderInspectable == nil {
		return "", 0, 0, nil, errors.New("get WinRT BitmapDecoder result")
	}
	defer decoderInspectable.Release()

	var framePtr unsafe.Pointer
	if hr := decoderInspectable.QueryInterface(&winrt.IID_IBitmapFrameWithSoftwareBitmap, &framePtr); hr != 0 || framePtr == nil {
		return "", 0, 0, nil, errors.New("get WinRT software bitmap frame")
	}
	frame := (*winrt.IBitmapFrameWithSoftwareBitmap)(framePtr)
	defer frame.Release()

	var bitmapOp *winrt.IAsyncOperation
	hr, _, _ = syscallN(
		frame.VTable().GetSoftwareBitmapAsync,
		uintptr(unsafe.Pointer(frame)),
		uintptr(unsafe.Pointer(&bitmapOp)),
	)
	if hr != 0 || bitmapOp == nil {
		return "", 0, 0, nil, errors.New("start WinRT software bitmap conversion")
	}
	defer bitmapOp.Release()
	if err := waitAsync(ctx, bitmapOp, 30*time.Second); err != nil {
		return "", 0, 0, nil, errors.New("convert image to WinRT software bitmap: " + err.Error())
	}
	bitmapInspectable, err := bitmapOp.GetResults()
	if err != nil || bitmapInspectable == nil {
		return "", 0, 0, nil, errors.New("get WinRT software bitmap result")
	}
	defer bitmapInspectable.Release()
	bitmap := (*winrt.ISoftwareBitmap)(unsafe.Pointer(bitmapInspectable))
	width, height := int(bitmap.GetPixelWidth()), int(bitmap.GetPixelHeight())
	if width <= 0 || height <= 0 {
		return "", 0, 0, nil, errors.New("WinRT returned invalid image dimensions")
	}

	ocrStatics, err := winrt.GetOcrEngineStatics()
	if err != nil {
		return "", 0, 0, nil, errors.New("get Windows OCR engine: " + err.Error())
	}
	defer ocrStatics.Release()
	engine, err := ocrStatics.TryCreateFromUserProfileLanguages()
	if err != nil || engine == nil {
		return "", 0, 0, nil, errors.New("create Windows OCR engine: install an OCR language pack in Windows Settings")
	}
	defer engine.Release()

	recognizeOp, err := engine.RecognizeAsync(bitmap)
	if err != nil {
		return "", 0, 0, nil, errors.New("start Windows OCR: " + err.Error())
	}
	defer recognizeOp.Release()
	if err := waitAsync(ctx, recognizeOp, 60*time.Second); err != nil {
		return "", 0, 0, nil, errors.New("run Windows OCR: " + err.Error())
	}
	resultInspectable, err := recognizeOp.GetResults()
	if err != nil || resultInspectable == nil {
		return "", 0, 0, nil, errors.New("get Windows OCR result")
	}
	defer resultInspectable.Release()
	ocrResult := (*winrt.IOcrResult)(unsafe.Pointer(resultInspectable))

	lines, err := ocrResult.GetLines()
	if err != nil {
		return "", 0, 0, nil, errors.New("read Windows OCR lines: " + err.Error())
	}
	defer lines.Release()

	wordsOut := make([]Word, 0)
	var text strings.Builder
	for i := uint32(0); i < lines.GetSize(); i++ {
		if err := ctx.Err(); err != nil {
			return "", 0, 0, nil, err
		}
		lineInspectable, err := lines.GetAt(i)
		if err != nil || lineInspectable == nil {
			continue
		}
		line := (*winrt.IOcrLine)(unsafe.Pointer(lineInspectable))
		lineText := strings.TrimSpace(line.GetText())
		lineWords, wordsErr := line.GetWords()
		if wordsErr == nil && lineWords != nil {
			for j := uint32(0); j < lineWords.GetSize(); j++ {
				wordInspectable, err := lineWords.GetAt(j)
				if err != nil || wordInspectable == nil {
					continue
				}
				word := (*winrt.IOcrWord)(unsafe.Pointer(wordInspectable))
				rect := word.GetBoundingRect()
				wordText := strings.TrimSpace(word.GetText())
				if wordText != "" {
					wordsOut = append(wordsOut, Word{
						Text: wordText,
						Box:  Box{X: float64(rect.X), Y: float64(rect.Y), Width: float64(rect.Width), Height: float64(rect.Height)},
					})
				}
				word.Release()
			}
			lineWords.Release()
		}
		if lineText != "" {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(lineText)
		}
		line.Release()
	}
	return text.String(), width, height, wordsOut, nil
}

func waitAsync(ctx context.Context, op *winrt.IAsyncOperation, timeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// IAsyncOperation.Wait polls the COM status. Keeping it on this goroutine
	// prevents releasing the COM object while another goroutine still uses it.
	if err := op.Wait(timeout); err != nil {
		return err
	}
	return ctx.Err()
}

func syscallN(trap uintptr, args ...uintptr) (uintptr, uintptr, error) {
	return syscall.SyscallN(trap, args...)
}
