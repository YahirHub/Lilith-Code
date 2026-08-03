package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MaxNativeWriteBytes limits a single structured write tool call. Larger
	// documents should be built with multiple append_file calls so providers do
	// not have to stream one enormous JSON argument.
	MaxNativeWriteBytes = 1 << 20
	// MaxNativeFileBytes bounds the final file produced by append_file.
	MaxNativeFileBytes = 64 << 20
)

type fileWriteReport struct {
	Bytes  int
	Lines  int
	SHA256 string
	Mode   fs.FileMode
}

type fileInspection struct {
	Exists bool
	Size   int64
	SHA256 string
	Mode   fs.FileMode
}

func inspectFile(path string, withHash bool) (fileInspection, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileInspection{}, nil
	}
	if err != nil {
		return fileInspection{}, err
	}
	if info.IsDir() {
		return fileInspection{}, fmt.Errorf("target is a directory: %s", path)
	}
	result := fileInspection{Exists: true, Size: info.Size(), Mode: info.Mode().Perm()}
	if !withHash {
		return result, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fileInspection{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fileInspection{}, err
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

func contentReport(content []byte, mode fs.FileMode) fileWriteReport {
	sum := sha256.Sum256(content)
	lines := bytes.Count(content, []byte{'\n'})
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}
	return fileWriteReport{
		Bytes:  len(content),
		Lines:  lines,
		SHA256: hex.EncodeToString(sum[:]),
		Mode:   mode.Perm(),
	}
}

func formatWriteReport(action, rel string, expected, written int, report fileWriteReport) string {
	return fmt.Sprintf(
		"%s %s\nbytes_expected: %d\nbytes_written: %d\nlines: %d\nsha256: %s\natomic: yes\nper_call_limit: 1 MiB\nper_call_limit_bytes: %d\nlarger_document_tool: append_file",
		action, rel, expected, written, report.Lines, report.SHA256, MaxNativeWriteBytes,
	)
}

func existingMode(path string, fallback fs.FileMode) (fs.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("target is a directory: %s", path)
	}
	return info.Mode().Perm(), nil
}

// atomicWriteFile writes into a temporary file in the destination directory,
// flushes it, and then atomically replaces the destination. Cancellation or a
// write failure removes only the temporary file and leaves the previous target
// untouched.
func atomicWriteFile(ctx context.Context, full string, content []byte, fallbackMode fs.FileMode) (fileWriteReport, error) {
	return atomicCommitFile(ctx, full, content, fallbackMode, true)
}

// atomicCreateFile publishes a fully written temporary file only when the
// destination is still absent. This preserves create-only semantics even when
// another process creates the path after Lilith's preflight check.
func atomicCreateFile(ctx context.Context, full string, content []byte, fallbackMode fs.FileMode) (fileWriteReport, error) {
	return atomicCommitFile(ctx, full, content, fallbackMode, false)
}

func atomicCommitFile(ctx context.Context, full string, content []byte, fallbackMode fs.FileMode, replaceExisting bool) (fileWriteReport, error) {
	if real, err := filepath.EvalSymlinks(full); err == nil {
		full = real
	}
	if len(content) > MaxNativeFileBytes {
		return fileWriteReport{}, fmt.Errorf("content is %d bytes; maximum final file size is %d bytes", len(content), MaxNativeFileBytes)
	}
	if err := ctx.Err(); err != nil {
		return fileWriteReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fileWriteReport{}, err
	}
	mode, err := existingMode(full, fallbackMode)
	if err != nil {
		return fileWriteReport{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".lilith-write-*")
	if err != nil {
		return fileWriteReport{}, err
	}
	tmpName := tmp.Name()
	keepTemp := true
	defer func() {
		_ = tmp.Close()
		if keepTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return fileWriteReport{}, err
	}
	const chunkSize = 64 << 10
	for offset := 0; offset < len(content); {
		if err := ctx.Err(); err != nil {
			return fileWriteReport{}, err
		}
		end := offset + chunkSize
		if end > len(content) {
			end = len(content)
		}
		n, err := tmp.Write(content[offset:end])
		if err != nil {
			return fileWriteReport{}, err
		}
		if n == 0 {
			return fileWriteReport{}, errors.New("temporary file write made no progress")
		}
		offset += n
	}
	if err := tmp.Sync(); err != nil {
		return fileWriteReport{}, err
	}
	if err := tmp.Close(); err != nil {
		return fileWriteReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return fileWriteReport{}, err
	}
	sourceMoved, err := commitFileAtomic(tmpName, full, replaceExisting)
	if err != nil {
		if !replaceExisting && errors.Is(err, fs.ErrExist) {
			return fileWriteReport{}, fmt.Errorf("destination appeared during atomic create: %s: %w", full, fs.ErrExist)
		}
		return fileWriteReport{}, err
	}
	// On Unix, a no-replace commit uses a hard link. Remove the temporary
	// directory entry before syncing the parent; the destination keeps the fully
	// written inode. Rename/MoveFileEx consumes the source name itself.
	if sourceMoved {
		keepTemp = false
	} else if removeErr := os.Remove(tmpName); removeErr == nil {
		keepTemp = false
	}
	// The destination has already been atomically replaced. Directory syncing is
	// best-effort because some filesystems do not support fsync on directories;
	// reporting a failure here could make callers retry an append that already
	// succeeded and duplicate content.
	_ = syncParentDirectory(filepath.Dir(full))
	report := contentReport(content, mode)
	verification, err := inspectFile(full, true)
	if err != nil {
		return fileWriteReport{}, err
	}
	if !verification.Exists || verification.Size != int64(report.Bytes) {
		return fileWriteReport{}, fmt.Errorf("atomic write verification failed: expected %d bytes, destination has %d", report.Bytes, verification.Size)
	}
	if verification.SHA256 != report.SHA256 {
		return fileWriteReport{}, fmt.Errorf("atomic write verification failed: expected sha256 %s, destination has %s", report.SHA256, verification.SHA256)
	}
	return report, nil
}

func fileSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateExpectedSHA(expected string, current []byte, rel string) error {
	return validateExpectedSHAValue(expected, fileSHA256(current), rel)
}

func validateExpectedSHAValue(expected, actual, rel string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return nil
	}
	actual = strings.ToLower(strings.TrimSpace(actual))
	if expected != actual {
		return fmt.Errorf("FILE_CHANGED: %s no longer matches expected_sha256 (expected %s, current %s). Re-read the file before writing", rel, expected, actual)
	}
	return nil
}
