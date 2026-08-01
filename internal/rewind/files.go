package rewind

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	fallbackMaxFileBytes  = int64(32 << 20)
	fallbackMaxTotalBytes = int64(512 << 20)
)

var fallbackExcludedDirs = map[string]bool{
	".git":         true,
	".lilith":      true,
	".cache":       true,
	"node_modules": true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"target":       true,
}

func captureFileWorkspace(projectPath, blobDir string) (WorkspaceSnapshot, error) {
	return captureFileWorkspaceContext(context.Background(), projectPath, blobDir)
}

func captureFileWorkspaceContext(ctx context.Context, projectPath, blobDir string) (WorkspaceSnapshot, error) {
	root, err := filepath.Abs(projectPath)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("abrir workspace: %w", err)
	}
	if !info.IsDir() {
		return WorkspaceSnapshot{}, fmt.Errorf("el workspace no es un directorio: %s", root)
	}
	if err := os.MkdirAll(blobDir, dirMode); err != nil {
		return WorkspaceSnapshot{}, err
	}
	snapshot := WorkspaceSnapshot{Kind: workspaceFiles, Root: root, WorkingRel: "."}
	var total int64
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			rel, _ := filepath.Rel(root, path)
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: filepath.ToSlash(rel), Reason: walkErr.Error()})
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if fallbackExcludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: err.Error()})
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: err.Error()})
				return nil
			}
			snapshot.Files = append(snapshot.Files, FileEntry{Path: relSlash, Mode: uint32(info.Mode()), Symlink: true, LinkTarget: target})
			return nil
		}
		if !info.Mode().IsRegular() {
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: "tipo de archivo no regular"})
			return nil
		}
		if info.Size() > fallbackMaxFileBytes {
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: fmt.Sprintf("archivo mayor de %d MiB", fallbackMaxFileBytes>>20)})
			return nil
		}
		if total+info.Size() > fallbackMaxTotalBytes {
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: fmt.Sprintf("snapshot excede %d MiB", fallbackMaxTotalBytes>>20)})
			return nil
		}
		hash, err := storeBlobContext(ctx, path, blobDir)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			snapshot.Skipped = append(snapshot.Skipped, SkippedEntry{Path: relSlash, Reason: err.Error()})
			return nil
		}
		total += info.Size()
		snapshot.Files = append(snapshot.Files, FileEntry{Path: relSlash, Mode: uint32(info.Mode()), Size: info.Size(), SHA256: hash})
		return nil
	})
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	sort.Slice(snapshot.Files, func(i, j int) bool { return snapshot.Files[i].Path < snapshot.Files[j].Path })
	return snapshot, nil
}

func storeBlob(path, blobDir string) (string, error) {
	return storeBlobContext(context.Background(), path, blobDir)
}

func storeBlobContext(ctx context.Context, path, blobDir string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx: ctx, reader: f}); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dst := filepath.Join(blobDir, sum[:2], sum)
	if _, err := os.Stat(dst); err == nil {
		return sum, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err := copyTo(dst, contextReader{ctx: ctx, reader: f}); err != nil {
		return "", err
	}
	return sum, nil
}

func restoreFileWorkspace(snapshot WorkspaceSnapshot, blobDir, destinationRoot string) error {
	return restoreFileWorkspaceContext(context.Background(), snapshot, blobDir, destinationRoot)
}

func restoreFileWorkspaceContext(ctx context.Context, snapshot WorkspaceSnapshot, blobDir, destinationRoot string) error {
	root, err := filepath.Abs(destinationRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	target := make(map[string]FileEntry, len(snapshot.Files))
	for _, entry := range snapshot.Files {
		target[filepath.ToSlash(entry.Path)] = entry
	}
	current, err := fallbackCurrentFilesContext(ctx, root)
	if err != nil {
		return err
	}
	for _, rel := range current {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, ok := target[rel]; ok {
			continue
		}
		path, err := safeJoin(root, rel)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("eliminar %s: %w", rel, err)
		}
	}
	for _, entry := range snapshot.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := safeJoin(root, entry.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		_ = os.Remove(path)
		if entry.Symlink {
			if err := os.Symlink(entry.LinkTarget, path); err != nil {
				return fmt.Errorf("restaurar symlink %s: %w", entry.Path, err)
			}
			continue
		}
		if len(entry.SHA256) < 2 {
			return fmt.Errorf("blob inválido para %s", entry.Path)
		}
		blob := filepath.Join(blobDir, entry.SHA256[:2], entry.SHA256)
		f, err := os.Open(blob)
		if err != nil {
			return fmt.Errorf("abrir blob de %s: %w", entry.Path, err)
		}
		mode := os.FileMode(entry.Mode).Perm()
		if mode == 0 {
			mode = 0o600
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = f.Close()
			return err
		}
		_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: f})
		closeOutErr := out.Close()
		closeInErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
	}
	removeEmptyParents(root, current)
	return nil
}

func fallbackCurrentFiles(root string) ([]string, error) {
	return fallbackCurrentFilesContext(context.Background(), root)
}

func fallbackCurrentFilesContext(ctx context.Context, root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if fallbackExcludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return r.reader.Read(p)
}

func formatSkipped(snapshot WorkspaceSnapshot) string {
	if len(snapshot.Skipped) == 0 {
		return ""
	}
	parts := make([]string, 0, len(snapshot.Skipped))
	for _, item := range snapshot.Skipped {
		parts = append(parts, item.Path+": "+item.Reason)
	}
	return strings.Join(parts, "; ")
}
