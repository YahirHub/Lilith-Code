//go:build !windows

package imageocr

import (
	"context"
	"errors"
)

func recognizeNative(context.Context, []byte, []string) (string, int, int, []Word, error) {
	return "", 0, 0, nil, errors.New("native OCR is only available on Windows")
}
