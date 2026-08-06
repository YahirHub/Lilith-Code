// Package textsearch provides Lilith's pure-Go repository text search fallback.
// It intentionally implements the subset needed by code_search and the portable
// shell; native ripgrep keeps priority whenever it is installed.
package textsearch

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lilith/li/internal/gitzip"
)

const (
	defaultLimit       = 100
	defaultMaxLine     = 500
	defaultMaxFileSize = 16 << 20
)

// Options configures a search. Root is used only to render stable relative
// paths; Path may be absolute or relative to Root.
type Options struct {
	Root       string
	Path       string
	Pattern    string
	Glob       string
	Literal    bool
	IgnoreCase bool
	Context    int
	Limit      int
	MaxLine    int
	Hidden     bool
}

// Result is a deterministic, rg-like line-oriented search response.
type Result struct {
	Text       string
	Matches    int
	Files      int
	Truncated  bool
	SkippedBin int
}

// Search walks files and returns path:line:text matches. The fallback skips
// VCS/dependency/build directories and binary/oversized files by default.
func Search(ctx context.Context, opts Options) (Result, error) {
	if strings.TrimSpace(opts.Pattern) == "" {
		return Result{}, errors.New("empty pattern")
	}
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return Result{}, err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	target := strings.TrimSpace(opts.Path)
	if target == "" {
		target = "."
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)

	matcher, err := newMatcher(opts.Pattern, opts.Literal, opts.IgnoreCase)
	if err != nil {
		return Result{}, err
	}
	if err := validateGlob(opts.Glob); err != nil {
		return Result{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	maxLine := opts.MaxLine
	if maxLine <= 0 {
		maxLine = defaultMaxLine
	}
	contextLines := opts.Context
	if contextLines < 0 {
		contextLines = 0
	}

	info, err := os.Stat(target)
	if err != nil {
		return Result{}, err
	}
	files := make([]string, 0, 64)
	if !info.IsDir() {
		// An explicitly requested file is searched even when repository ignore
		// rules would normally hide it, matching ripgrep's explicit-path behavior.
		if matchGlob(root, target, opts.Glob) {
			files = append(files, target)
		}
	} else {
		initialMatcher, ignoreEnabled, err := matcherForDirectory(root, target)
		if err != nil {
			return Result{}, err
		}
		matchers := map[string]gitzip.Matcher{target: initialMatcher}
		err = filepath.WalkDir(target, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, fs.ErrPermission) {
					return nil
				}
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if filePath == target {
				return nil
			}
			parentMatcher := matchers[filepath.Dir(filePath)]
			rel := relativeSlash(root, filePath)
			if entry.IsDir() {
				if shouldSkipDir(entry.Name(), opts.Hidden) || (ignoreEnabled && parentMatcher.Ignored(rel, true)) {
					return filepath.SkipDir
				}
				childMatcher, err := addDirectoryIgnoreFiles(parentMatcher, filePath, rel)
				if err != nil {
					return err
				}
				matchers[filePath] = childMatcher
				return nil
			}
			// Match ripgrep's safe default: do not follow symbolic links unless the
			// caller explicitly targets one as Path. This also prevents a repository
			// search from escaping its tree through a linked directory.
			if !entry.Type().IsRegular() || (!opts.Hidden && strings.HasPrefix(entry.Name(), ".")) || (ignoreEnabled && parentMatcher.Ignored(rel, false)) {
				return nil
			}
			if matchGlob(root, filePath, opts.Glob) {
				files = append(files, filePath)
			}
			return nil
		})
		if err != nil {
			return Result{}, err
		}
	}
	sort.Strings(files)

	var out strings.Builder
	result := Result{}
	for _, filePath := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		lines, binary, err := readSearchLines(ctx, filePath)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				continue
			}
			return result, err
		}
		if binary {
			result.SkippedBin++
			continue
		}
		remaining := limit - result.Matches
		matched := make([]int, 0, min(remaining+1, 64))
		for i, line := range lines {
			if !matcher(line) {
				continue
			}
			matched = append(matched, i)
			// One extra match is sufficient to prove truncation without retaining
			// every matching line from a large, dense file.
			if len(matched) > remaining {
				break
			}
		}
		if len(matched) == 0 {
			continue
		}
		result.Files++
		display := displayPath(root, filePath)
		emitted := map[int]bool{}
		for _, idx := range matched {
			if result.Matches >= limit {
				result.Truncated = true
				break
			}
			start := idx - contextLines
			if start < 0 {
				start = 0
			}
			end := idx + contextLines
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for lineNo := start; lineNo <= end; lineNo++ {
				if emitted[lineNo] {
					continue
				}
				emitted[lineNo] = true
				sep := "-"
				if lineNo == idx {
					sep = ":"
				}
				fmt.Fprintf(&out, "%s%s%d%s%s\n", display, sep, lineNo+1, sep, clipLine(lines[lineNo], maxLine))
			}
			result.Matches++
		}
		if result.Truncated {
			break
		}
	}
	result.Text = strings.TrimSuffix(out.String(), "\n")
	return result, nil
}

// SearchReader searches a stream for grep-like pipeline use.
func SearchReader(ctx context.Context, name string, r io.Reader, pattern string, literal, ignoreCase bool, contextLines, limit, maxLine int) (Result, error) {
	matcher, err := newMatcher(pattern, literal, ignoreCase)
	if err != nil {
		return Result{}, err
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if maxLine <= 0 {
		maxLine = defaultMaxLine
	}
	if contextLines < 0 {
		contextLines = 0
	}
	lines, err := scanLines(ctx, r, defaultMaxFileSize)
	if err != nil {
		return Result{}, err
	}
	if name == "" {
		name = "(standard input)"
	}
	var out strings.Builder
	result := Result{}
	emitted := map[int]bool{}
	for idx, line := range lines {
		if !matcher(line) {
			continue
		}
		if result.Matches >= limit {
			result.Truncated = true
			break
		}
		start := idx - contextLines
		if start < 0 {
			start = 0
		}
		end := idx + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for lineNo := start; lineNo <= end; lineNo++ {
			if emitted[lineNo] {
				continue
			}
			emitted[lineNo] = true
			sep := "-"
			if lineNo == idx {
				sep = ":"
			}
			fmt.Fprintf(&out, "%s%s%d%s%s\n", name, sep, lineNo+1, sep, clipLine(lines[lineNo], maxLine))
		}
		result.Matches++
	}
	if result.Matches > 0 {
		result.Files = 1
	}
	result.Text = strings.TrimSuffix(out.String(), "\n")
	return result, nil
}

func newMatcher(pattern string, literal, ignoreCase bool) (func(string) bool, error) {
	if literal {
		needle := pattern
		if ignoreCase {
			needle = strings.ToLower(needle)
			return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
		}
		return func(line string) bool { return strings.Contains(line, needle) }, nil
	}
	if ignoreCase {
		pattern = "(?i:" + pattern + ")"
	}
	rx, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	return rx.MatchString, nil
}

func readSearchLines(ctx context.Context, filePath string) ([]string, bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > defaultMaxFileSize {
		return nil, true, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	prefix := make([]byte, 8<<10)
	n, readErr := f.Read(prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, false, readErr
	}
	prefix = prefix[:n]
	if bytes.IndexByte(prefix, 0) >= 0 || !utf8.Valid(prefix) {
		return nil, true, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, false, err
	}
	lines, err := scanLines(ctx, f, defaultMaxFileSize)
	return lines, false, err
}

func scanLines(ctx context.Context, r io.Reader, maxBytes int64) ([]string, error) {
	reader := bufio.NewReader(io.LimitReader(r, maxBytes+1))
	lines := make([]string, 0, 128)
	var consumed int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := reader.ReadString('\n')
		consumed += int64(len(line))
		if consumed > maxBytes {
			return nil, fmt.Errorf("input exceeds portable search limit of %d bytes", maxBytes)
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if len(line) > 0 || err == nil {
			lines = append(lines, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func matcherForDirectory(root, dir string) (gitzip.Matcher, bool, error) {
	m := gitzip.NewMatcher(nil)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// Ignore files are repository-relative. When callers intentionally search
		// outside Root, do not apply unrelated project rules.
		return m, false, nil
	}
	m, err = addDirectoryIgnoreFiles(m, root, "")
	if err != nil {
		return gitzip.Matcher{}, false, err
	}
	if rel == "." {
		return m, true, nil
	}
	current := root
	base := ""
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, filepath.FromSlash(part))
		base = path.Join(base, part)
		m, err = addDirectoryIgnoreFiles(m, current, base)
		if err != nil {
			return gitzip.Matcher{}, false, err
		}
	}
	return m, true, nil
}

func addDirectoryIgnoreFiles(m gitzip.Matcher, dir, base string) (gitzip.Matcher, error) {
	for _, name := range gitzip.IgnoreFileNames {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			m = m.AddContent(string(data), filepath.ToSlash(base))
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return gitzip.Matcher{}, err
		}
	}
	return m, nil
}

func relativeSlash(root, filePath string) string {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(rel)
}

func shouldSkipDir(name string, hidden bool) bool {
	switch strings.ToLower(name) {
	case ".git", ".hg", ".svn", ".li", ".lilith", "node_modules", "vendor", "target", "dist", ".next", ".nuxt", "__pycache__", ".venv", "venv", "coverage":
		return true
	}
	return !hidden && strings.HasPrefix(name, ".")
}

func matchGlob(root, filePath, pattern string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return true
	}
	rel := strings.TrimPrefix(displayPath(root, filePath), "./")
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(rel))
		return ok
	}
	return matchGlobSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func validateGlob(pattern string) error {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
	}
	return nil
}

func matchGlobSegments(pattern, name []string) bool {
	type state struct{ pattern, name int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(pi, ni int) bool {
		key := state{pi, ni}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var ok bool
		switch {
		case pi == len(pattern):
			ok = ni == len(name)
		case pattern[pi] == "**":
			ok = match(pi+1, ni) || (ni < len(name) && match(pi, ni+1))
		case ni < len(name):
			segmentMatch, _ := path.Match(pattern[pi], name[ni])
			ok = segmentMatch && match(pi+1, ni+1)
		}
		memo[key] = ok
		return ok
	}
	return match(0, 0)
}

func displayPath(root, filePath string) string {
	rel, err := filepath.Rel(root, filePath)
	if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return filepath.ToSlash(rel)
	}
	if err == nil && rel == "." {
		return filepath.ToSlash(filepath.Base(filePath))
	}
	return filepath.ToSlash(filePath)
}

func clipLine(line string, max int) string {
	if max <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= max {
		return line
	}
	return string(runes[:max]) + "…"
}
