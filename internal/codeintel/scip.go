package codeintel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const scipOutputLimit = 32 << 20

func findSCIPIndex(root string) string {
	candidates := []string{
		filepath.Join(root, "index.scip"),
		filepath.Join(root, ".scip", "index.scip"),
		filepath.Join(root, "scip", "index.scip"),
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		resolvedRoot = filepath.Clean(root)
	}
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		resolved, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			continue
		}
		resolvedInfo, statErr := os.Stat(resolved)
		if statErr != nil || !resolvedInfo.Mode().IsRegular() {
			continue
		}
		resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
		if pathEscapesRoot(resolvedRel, relErr) {
			continue
		}
		rel, relErr := filepath.Rel(root, candidate)
		if !pathEscapesRoot(rel, relErr) {
			return filepath.ToSlash(rel)
		}
	}
	return ""
}

// SCIPSearch is an optional large-repository fallback. It consumes an existing
// SCIP index through an installed scip CLI and never generates or downloads an
// index on the user's behalf.
func (m *Manager) SCIPSearch(ctx context.Context, query string, limit int) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("SCIP search requires a non-empty query")
	}
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	profile := m.RefreshProfile()
	if profile.SCIPIndex == "" {
		return nil, errors.New("no index.scip was found in this repository")
	}
	executable := profile.Environment.Tools["scip"]
	if executable == "" {
		return nil, errors.New("an index.scip exists but the scip CLI is not installed")
	}
	indexPath := profile.SCIPIndex
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(m.root, filepath.FromSlash(indexPath))
	}
	output, err := runSCIPPrint(ctx, executable, indexPath, m.root)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(query)
	var matches []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		matches = append(matches, line)
		if len(matches) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return matches, err
	}
	return matches, nil
}

func runSCIPPrint(ctx context.Context, executable, indexPath, root string) ([]byte, error) {
	attempts := [][]string{
		{"print", "--json", indexPath},
		{"print", "--json", "--from", indexPath},
		{"print", indexPath},
	}
	var lastErr error
	for _, args := range attempts {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Dir = root
		var output limitedBuffer
		output.limit = scipOutputLimit
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Run(); err == nil {
			return output.Bytes(), nil
		} else {
			lastErr = fmt.Errorf("%s %s: %w: %s", filepath.Base(executable), strings.Join(args, " "), err, strings.TrimSpace(output.String()))
		}
	}
	return nil, lastErr
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}
