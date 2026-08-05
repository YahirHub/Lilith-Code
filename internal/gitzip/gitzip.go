// Package gitzip creates deployment archives while honoring repository ignore
// files. It intentionally uses only the standard library for local archives.
package gitzip

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var IgnoreFileNames = []string{".gitignore", ".lilithignore", ".codewolfignore", ".codebuffignore", ".manicodeignore"}

type Format string

const (
	FormatZIP   Format = "zip"
	FormatTAR   Format = "tar"
	FormatTARGZ Format = "tar.gz"
)

type Options struct {
	SourceRoot, OutputPath         string
	Format                         Format
	ExtraExcludes                  []string
	IncludePaths                   []string
	IncludeProtectedEnv, Overwrite bool
	CompressionLevel               int
}
type Result struct {
	SourcePath           string `json:"source_path"`
	OutputPath           string `json:"output_path"`
	Format               Format `json:"format"`
	Files                int    `json:"files"`
	Directories          int    `json:"directories"`
	Symlinks             int    `json:"symlinks"`
	Bytes                int64  `json:"bytes"`
	Ignored              int    `json:"ignored"`
	ProtectedEnvExcluded int    `json:"protected_env_excluded"`
}
type Entry struct {
	Relative, Absolute string
	Info               os.FileInfo
	LinkTarget         string
}
type rule struct {
	negate, dirOnly bool
	regex           *regexp.Regexp
}

func InferFormat(explicit, output string, fallback Format) Format {
	if explicit != "" {
		return Format(explicit)
	}
	lower := strings.ToLower(output)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return FormatTARGZ
	}
	if strings.HasSuffix(lower, ".tar") {
		return FormatTAR
	}
	if strings.HasSuffix(lower, ".zip") {
		return FormatZIP
	}
	return fallback
}
func Extension(f Format) string {
	if f == FormatTARGZ {
		return "tar.gz"
	}
	return string(f)
}
func Create(ctx context.Context, opt Options) (Result, error) {
	source, err := filepath.Abs(opt.SourceRoot)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, errors.New("source_path debe ser un directorio")
	}
	opt.SourceRoot = source
	if opt.Format == "" {
		opt.Format = InferFormat("", opt.OutputPath, FormatZIP)
	}
	if opt.Format != FormatZIP && opt.Format != FormatTAR && opt.Format != FormatTARGZ {
		return Result{}, fmt.Errorf("formato no compatible: %s", opt.Format)
	}
	if opt.OutputPath == "" {
		opt.OutputPath = filepath.Join(source, filepath.Base(source)+"."+Extension(opt.Format))
	} else if !filepath.IsAbs(opt.OutputPath) {
		opt.OutputPath = filepath.Join(source, opt.OutputPath)
	}
	opt.OutputPath = filepath.Clean(opt.OutputPath)
	if !opt.Overwrite {
		if _, err = os.Stat(opt.OutputPath); err == nil {
			return Result{}, errors.New("el archivo de salida ya existe; usa overwrite=true")
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(opt.OutputPath), ".lilith-gitzip-*.tmp")
	if err != nil {
		return Result{}, err
	}
	tmpPath := tmp.Name()
	if err = tmp.Close(); err != nil {
		return Result{}, err
	}
	defer os.Remove(tmpPath)
	entries, res, err := Scan(ctx, source, opt.OutputPath, tmpPath, opt.ExtraExcludes, opt.IncludePaths, opt.IncludeProtectedEnv)
	if err != nil {
		return Result{}, err
	}
	res.OutputPath = opt.OutputPath
	res.Format = opt.Format
	switch opt.Format {
	case FormatZIP:
		err = writeZIP(tmpPath, entries, opt.CompressionLevel)
	case FormatTAR:
		err = writeTAR(tmpPath, entries, false, opt.CompressionLevel)
	case FormatTARGZ:
		err = writeTAR(tmpPath, entries, true, opt.CompressionLevel)
	}
	if err != nil {
		return Result{}, err
	}
	if err = os.Rename(tmpPath, opt.OutputPath); err != nil {
		if opt.Overwrite {
			_ = os.Remove(opt.OutputPath)
			err = os.Rename(tmpPath, opt.OutputPath)
		}
		if err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

func Scan(ctx context.Context, source, output, tmp string, extra, includes []string, includeEnv bool) ([]Entry, Result, error) {
	selector, err := NewSelector(includes)
	if err != nil {
		return nil, Result{}, err
	}
	baseRules := []rule{}
	for _, p := range append([]string{".git/", ".git"}, extra...) {
		if r, ok := compileRule(p, ""); ok {
			baseRules = append(baseRules, r)
		}
	}
	for _, candidate := range []string{output, tmp} {
		if rel, err := filepath.Rel(source, candidate); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			if r, ok := compileRule("/"+filepath.ToSlash(rel), ""); ok {
				baseRules = append(baseRules, r)
			}
		}
	}
	type item struct {
		abs, rel string
		rules    []rule
	}
	queue := []item{{source, "", baseRules}}
	var entries []Entry
	res := Result{SourcePath: source}
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return nil, res, ctx.Err()
		default:
		}
		cur := queue[0]
		queue = queue[1:]
		rules := append([]rule(nil), cur.rules...)
		for _, name := range IgnoreFileNames {
			data, err := os.ReadFile(filepath.Join(cur.abs, name))
			if err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if r, ok := compileRule(strings.TrimSuffix(line, "\r"), filepath.ToSlash(cur.rel)); ok {
						rules = append(rules, r)
					}
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, res, err
			}
		}
		list, err := os.ReadDir(cur.abs)
		if err != nil {
			return nil, res, err
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
		for _, de := range list {
			rel := filepath.ToSlash(filepath.Join(cur.rel, de.Name()))
			abs := filepath.Join(cur.abs, de.Name())
			info, err := os.Lstat(abs)
			if err != nil {
				return nil, res, err
			}
			isDir := info.IsDir()
			ignored := matchRules(rules, rel, isDir)
			if strings.EqualFold(rel, ".git") || strings.HasPrefix(strings.ToLower(rel), ".git/") {
				ignored = true
			}
			if !includeEnv && IsProtectedEnv(rel) {
				res.ProtectedEnvExcluded++
				ignored = true
			}
			if ignored {
				res.Ignored++
				continue
			}
			selected := selector.Includes(rel, isDir)
			e := Entry{Relative: rel, Absolute: abs, Info: info}
			if info.Mode()&os.ModeSymlink != 0 {
				if !selected {
					res.Ignored++
					continue
				}
				e.LinkTarget, _ = os.Readlink(abs)
				res.Symlinks++
			} else if isDir {
				queue = append(queue, item{abs, rel, rules})
				if !selected {
					continue
				}
				res.Directories++
			} else {
				if !selected {
					res.Ignored++
					continue
				}
				res.Files++
				res.Bytes += info.Size()
			}
			entries = append(entries, e)
		}
	}
	return entries, res, nil
}

// Selector limits an archive to explicit source-relative paths. Empty means
// everything. Patterns are root-relative and accept * / ** / ? globs; selecting
// a directory also selects every descendant below it.
type Selector struct {
	all      bool
	patterns []*regexp.Regexp
}

func NewSelector(patterns []string) (Selector, error) {
	selector := Selector{all: len(patterns) == 0}
	for _, raw := range patterns {
		raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		raw = strings.TrimPrefix(raw, "./")
		raw = strings.Trim(raw, "/")
		if raw == "" || raw == "." {
			selector.all = true
			selector.patterns = nil
			return selector, nil
		}
		if strings.HasPrefix(raw, "../") || raw == ".." {
			return Selector{}, fmt.Errorf("include_paths no permite salir de source_path: %s", raw)
		}
		expr := "^" + globRegex(raw) + "(?:/.*)?$"
		re, err := regexp.Compile(expr)
		if err != nil {
			return Selector{}, fmt.Errorf("include_paths inválido %q: %w", raw, err)
		}
		selector.patterns = append(selector.patterns, re)
	}
	return selector, nil
}

func (s Selector) Includes(rel string, _ bool) bool {
	if s.all || len(s.patterns) == 0 {
		return true
	}
	rel = strings.Trim(strings.TrimPrefix(filepath.ToSlash(rel), "./"), "/")
	for _, re := range s.patterns {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

func compileRule(raw, base string) (rule, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return rule{}, false
	}
	neg := strings.HasPrefix(raw, "!")
	if neg {
		raw = strings.TrimPrefix(raw, "!")
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	dirOnly := strings.HasSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, "/")
	anchored := strings.HasPrefix(raw, "/")
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return rule{}, false
	}
	prefix := ""
	if base != "" {
		prefix = regexp.QuoteMeta(strings.Trim(base, "/")) + "/"
	}
	var expression string
	if anchored || strings.Contains(raw, "/") {
		expression = "^" + prefix + globRegex(raw) + "(?:/.*)?$"
	} else {
		expression = "^(?:" + prefix + ")?(?:.*/)?" + globRegex(raw) + "(?:/.*)?$"
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return rule{}, false
	}
	return rule{negate: neg, dirOnly: dirOnly, regex: re}, true
}
func globRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				// **/ means zero or more complete directory components, while a
				// bare ** may span any characters, including path separators.
				// This makes assets/**/*.png match both assets/logo.png and
				// assets/icons/logo.png, as users expect from Git-style globs.
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		} else if ch == '?' {
			b.WriteString("[^/]")
		} else {
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return b.String()
}
func matchRules(rules []rule, rel string, isDir bool) bool {
	ignored := false
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	for _, r := range rules {
		if r.dirOnly && !isDir && !strings.Contains(rel, "/") {
			continue
		}
		if r.regex.MatchString(rel) {
			ignored = !r.negate
		}
	}
	return ignored
}
func isGitMetadataPath(p string) bool {
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(p)), "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

func IsProtectedEnv(p string) bool {
	base := strings.ToLower(path.Base(filepath.ToSlash(p)))
	if base == ".env" {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		for _, x := range []string{"example", "sample", "template", "dist", "defaults"} {
			if base == ".env."+x {
				return false
			}
		}
		return true
	}
	return false
}

func writeZIP(output string, entries []Entry, level int) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	if level >= 0 && level <= 9 {
		zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, level) })
	}
	for _, e := range entries {
		hdr, err := zip.FileInfoHeader(e.Info)
		if err != nil {
			return err
		}
		hdr.Name = e.Relative
		if e.Info.IsDir() {
			hdr.Name += "/"
		}
		if e.Info.Mode()&os.ModeSymlink != 0 {
			hdr.Method = zip.Store
		} else if !e.Info.IsDir() {
			hdr.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if e.LinkTarget != "" {
			if _, err = io.WriteString(w, e.LinkTarget); err != nil {
				return err
			}
		} else if !e.Info.IsDir() {
			in, err := os.Open(e.Absolute)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(w, in)
			closeErr := in.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	if err = zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func writeTAR(output string, entries []Entry, gz bool, level int) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	var out io.Writer = f
	var gzW *gzip.Writer
	if gz {
		if level < 0 || level > 9 {
			level = gzip.DefaultCompression
		}
		gzW, err = gzip.NewWriterLevel(f, level)
		if err != nil {
			return err
		}
		out = gzW
	}
	tw := tar.NewWriter(out)
	for _, e := range entries {
		hdr, err := tar.FileInfoHeader(e.Info, e.LinkTarget)
		if err != nil {
			return err
		}
		hdr.Name = e.Relative
		if e.Info.IsDir() {
			hdr.Name += "/"
		}
		if err = tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !e.Info.IsDir() && e.LinkTarget == "" {
			in, err := os.Open(e.Absolute)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, in)
			closeErr := in.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if gzW != nil {
		if err = gzW.Close(); err != nil {
			return err
		}
	}
	return f.Close()
}

// Matcher exposes the same ignore semantics to remote GitZip scans.
type Matcher struct{ rules []rule }

func NewMatcher(extra []string) Matcher {
	m := Matcher{}
	for _, p := range append([]string{".git/", ".git"}, extra...) {
		if r, ok := compileRule(p, ""); ok {
			m.rules = append(m.rules, r)
		}
	}
	return m
}
func (m Matcher) AddContent(content, base string) Matcher {
	next := Matcher{rules: append([]rule(nil), m.rules...)}
	for _, line := range strings.Split(content, "\n") {
		if r, ok := compileRule(strings.TrimSuffix(line, "\r"), base); ok {
			next.rules = append(next.rules, r)
		}
	}
	return next
}
func (m Matcher) AddPattern(pattern, base string) Matcher {
	next := Matcher{rules: append([]rule(nil), m.rules...)}
	if r, ok := compileRule(pattern, base); ok {
		next.rules = append(next.rules, r)
	}
	return next
}
func (m Matcher) Ignored(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	if isGitMetadataPath(rel) {
		return true
	}
	return matchRules(m.rules, rel, isDir)
}
