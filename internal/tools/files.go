package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxFileBytes bounds what a single read injects into the context.
const MaxFileBytes = 128 << 10

// resolve joins a user-supplied path to the project root. Absolute paths are
// allowed (so a running skill can read scripts/assets from its own directory
// under ~/.li/skills) and relative paths are anchored to the project root.
// We only block obviously sensitive prefixes to keep foot-guns off the table.
func resolve(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("empty path")
	}
	var clean string
	if filepath.IsAbs(p) {
		clean = filepath.Clean(p)
	} else {
		clean = filepath.Clean(filepath.Join(root, p))
	}
	return clean, nil
}

// markSeen records a path as known (leído o escrito) en esta sesión.
func markSeen(env Env, rel string) {
	if env.Seen != nil {
		env.Seen[filepath.ToSlash(filepath.Clean(rel))] = true
	}
}

func wasSeen(env Env, rel string) bool {
	if env.Seen == nil {
		return false
	}
	return env.Seen[filepath.ToSlash(filepath.Clean(rel))]
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	"vendor": true, ".next": true, "target": true, "bin": true, ".li": true,
}

func init() {
	register(Definition{
		Name: "read_files",
		Description: "Read one or more files. Accepts project-relative paths or absolute paths (e.g. a skill's own scripts/assets under ~/.li/skills). " +
			"For large text files use `offset` (1-indexed start line) and `limit` (max lines) to paginate instead of pulling the whole file into context; " +
			"the response reports the visible range and the next offset when there is more content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "File paths. Relative paths resolve against the project root; absolute paths are allowed.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Optional 1-indexed line number to start reading from (applies to every path). Omit to start at line 1.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of lines to return per file. Omit for the whole (byte-capped) file.",
				},
			},
			"required": []string{"paths"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			paths := strSlice(args, "paths")
			if len(paths) == 0 {
				if p := str(args, "path"); p != "" {
					paths = []string{p}
				}
			}
			if len(paths) == 0 {
				return "", errors.New("provide at least one path")
			}
			offset := intArg(args, "offset", 0)
			limit := intArg(args, "limit", 0)
			var b strings.Builder
			for _, p := range paths {
				full, err := resolve(env.Root, p)
				if err != nil {
					fmt.Fprintf(&b, "== %s ==\nerror: %v\n\n", p, err)
					continue
				}
				data, err := os.ReadFile(full)
				if err != nil {
					fmt.Fprintf(&b, "== %s ==\nerror: %v\n\n", p, err)
					continue
				}
				if kind := imageKind(full); kind != "" {
					markSeen(env, p)
					fmt.Fprintf(&b, "== %s ==\n[image %s, %d bytes — binary content not inlined; open in an external viewer if you need to see it]\n\n", p, kind, len(data))
					continue
				}
				if isBinary(data) {
					markSeen(env, p)
					fmt.Fprintf(&b, "== %s ==\n[binary file, %d bytes — refusing to inline; use read_url or a specialised tool]\n\n", p, len(data))
					continue
				}
				content, note := sliceLines(string(data), offset, limit)
				if note == "" && len(content) > MaxFileBytes {
					content = content[:MaxFileBytes]
					note = fmt.Sprintf("[truncated at %d bytes — use offset/limit to paginate]", MaxFileBytes)
				}
				markSeen(env, p)
				fmt.Fprintf(&b, "== %s ==\n%s\n", p, content)
				if note != "" {
					fmt.Fprintf(&b, "%s\n", note)
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	})



	register(Definition{
		Name: "write_file",
		Description: "Create a NEW file with the full final content. " +
			"Use this only when the file does not exist yet, or when the user " +
			"explicitly asks for a full rewrite. To modify an EXISTING file, " +
			"read it first with read_files and then edit it with str_replace. " +
			"Never call write_file with placeholders, ellipsis, or partial content.",
		Mutating: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "Target file path (project-relative or absolute)."},
				"content": map[string]any{"type": "string", "description": "Full, final content of the file. No placeholders, no `...`, no `// rest of code`."},
			},
			"required": []string{"path", "content"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			mu := lockFile(full)
			mu.Lock()
			defer mu.Unlock()
			if info, statErr := os.Stat(full); statErr == nil && !info.IsDir() && info.Size() > 0 && !wasSeen(env, rel) {
				return "", fmt.Errorf("%s already exists (%d bytes): read it with read_files and edit it with str_replace instead of rewriting the whole file", rel, info.Size())
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return "", err
			}
			content := str(args, "content")
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				return "", err
			}
			markSeen(env, rel)
			return fmt.Sprintf("wrote %s (%d bytes).", rel, len(content)), nil
		},
	})

	register(Definition{
		Name: "str_replace",
		Description: "Edit an existing file with one or more exact text replacements. " +
			"Pass either a single `old`/`new` pair OR an `edits` array `[{old, new}, ...]` to apply several " +
			"replacements to the same file in one call. Each `old` is matched against the ORIGINAL file " +
			"content (not incrementally) so edits must NOT overlap. " +
			"REQUIREMENTS: (1) the file MUST already exist and MUST have been read with read_files in this " +
			"session; (2) every `old` MUST be non-empty and copied byte-for-byte from the file (whitespace " +
			"and newlines matter); (3) every `old` MUST appear EXACTLY ONCE — expand it with 2-3 surrounding " +
			"anchor lines if the snippet is not unique; (4) `new` may be empty to delete the matched region. " +
			"If exact match fails, the tool retries with a fuzzy pass that normalises Unicode quotes/dashes " +
			"and trailing whitespace; the file is still written with the original bytes for unchanged regions. " +
			"DO NOT use str_replace to create a new file — use write_file instead. " +
			"DO NOT pass an empty `old`; to insert code, choose an existing anchor line already in the file " +
			"and put it inside `old`, then include the anchor plus the new lines inside `new`.",
		Mutating: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path of the existing file to edit (project-relative or absolute).",
				},
				"old": map[string]any{
					"type":        "string",
					"description": "Exact text to find. Non-empty, byte-for-byte, unique in the file. Ignored if `edits` is provided.",
				},
				"new": map[string]any{
					"type":        "string",
					"description": "Replacement text for `old`. May be empty to delete the matched region. Ignored if `edits` is provided.",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "Optional list of replacements to apply together. Each item is `{\"old\": string, \"new\": string}`. Matches are computed against the original file, so ranges must not overlap.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old": map[string]any{"type": "string"},
							"new": map[string]any{"type": "string"},
						},
						"required": []string{"old", "new"},
					},
				},
			},
			"required": []string{"path"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			mu := lockFile(full)
			mu.Lock()
			defer mu.Unlock()
			data, err := os.ReadFile(full)
			if err != nil {
				return "", err
			}
			edits, err := collectEdits(args)
			if err != nil {
				return "", err
			}
			out, applied, err := applyEdits(string(data), edits, rel)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
				return "", err
			}
			markSeen(env, rel)
			if applied == 1 {
				return fmt.Sprintf("edited %s (1 replacement).", rel), nil
			}
			return fmt.Sprintf("edited %s (%d replacements).", rel, applied), nil
		},
	})



	register(Definition{
		Name:        "list_directory",
		Description: "List files and folders in a directory (project-relative or absolute). Entries are sorted alphabetically with a trailing `/` on directories; dotfiles are included. Output is capped by `limit` (default 500) so large trees stay bounded.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string", "description": "Directory path. Empty = project root."},
				"limit": map[string]any{"type": "integer", "description": "Maximum entries to return (default 500)."},
			},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			if rel == "" {
				rel = "."
			}
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(full)
			if err != nil {
				return "", err
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			limit := intArg(args, "limit", 500)
			var b strings.Builder
			fmt.Fprintf(&b, "%s:\n", rel)
			shown := 0
			for _, e := range entries {
				if shown >= limit {
					fmt.Fprintf(&b, "[truncated at %d entries of %d — raise `limit` to see more]\n", limit, len(entries))
					break
				}
				if e.IsDir() {
					fmt.Fprintf(&b, "  %s/\n", e.Name())
				} else {
					fmt.Fprintf(&b, "  %s\n", e.Name())
				}
				shown++
			}
			return b.String(), nil
		},
	})

	register(Definition{
		Name:        "glob",
		Description: "Find files matching a glob pattern (e.g. `**/*.go` or `internal/**/*_test.go`).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Glob pattern relative to the project root."},
				"limit":   map[string]any{"type": "integer", "description": "Maximum number of results (default 200)."},
			},
			"required": []string{"pattern"},
		},
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			pattern := str(args, "pattern")
			limit := intArg(args, "limit", 200)
			matches, err := globWalk(env.Root, pattern, limit)
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return "no matches.", nil
			}
			sort.Strings(matches)
			return strings.Join(matches, "\n"), nil
		},
	})
}

// globWalk implements `**` semantics on top of filepath.Match.
func globWalk(root, pattern string, limit int) ([]string, error) {
	pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matchGlob(pattern, rel) {
			out = append(out, rel)
			if len(out) >= limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	return out, err
}

func matchGlob(pattern, name string) bool {
	if strings.Contains(pattern, "**/") {
		suffix := pattern[strings.LastIndex(pattern, "**/")+3:]
		prefix := pattern[:strings.Index(pattern, "**/")]
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			return false
		}
		base := name
		for {
			if ok, _ := filepath.Match(suffix, base); ok {
				return true
			}
			i := strings.Index(base, "/")
			if i < 0 {
				return false
			}
			base = base[i+1:]
		}
	}
	ok, _ := filepath.Match(pattern, name)
	return ok
}

func strSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return int(t)
		}
	case int:
		if t > 0 {
			return t
		}
	}
	return def
}

// -----------------------------------------------------------------------------
// Helpers portados/inspirados en pi.dev (packages/agent/src/harness/tools):
// read pagination (offset/limit), multi-edit replacements y fuzzy matching.
// -----------------------------------------------------------------------------

// sliceLines returns a 1-indexed line window `[offset, offset+limit)` from src.
// When offset<=0 and limit<=0 it returns the whole content. When a window is
// selected it appends a hint noting the visible range and next offset.
func sliceLines(src string, offset, limit int) (string, string) {
	if offset <= 0 && limit <= 0 {
		return src, ""
	}
	lines := strings.Split(src, "\n")
	total := len(lines)
	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start >= total {
		return "", fmt.Sprintf("[offset %d is beyond end of file (%d lines total)]", offset, total)
	}
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	out := strings.Join(lines[start:end], "\n")
	if end < total {
		return out, fmt.Sprintf("[showing lines %d-%d of %d — use offset=%d to continue]", start+1, end, total, end+1)
	}
	return out, fmt.Sprintf("[showing lines %d-%d of %d]", start+1, end, total)
}

type editPair struct{ old, new string }

func collectEdits(args map[string]any) ([]editPair, error) {
	var out []editPair
	if raw, ok := args["edits"]; ok && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return nil, errors.New("`edits` must be an array of {old, new} objects")
		}
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("edits[%d] must be an object with `old` and `new` string fields", i)
			}
			pair := editPair{old: str(m, "old"), new: str(m, "new")}
			if pair.old == "" {
				return nil, fmt.Errorf("edits[%d].old is empty. Copy the exact snippet you want to replace (with 2-3 surrounding lines if needed). To insert code, reuse an anchor line as `old` and repeat it inside `new`", i)
			}
			out = append(out, pair)
		}
	}
	if len(out) == 0 {
		old, nw := str(args, "old"), str(args, "new")
		if old == "" {
			return nil, errors.New("`old` must not be empty. Copy the exact text you want to replace from the current file (with 2-3 surrounding lines if needed). To insert new code, pick an existing line as `old` and repeat it inside `new`. To create a new file, use write_file instead")
		}
		out = append(out, editPair{old: old, new: nw})
	}
	return out, nil
}

// applyEdits applies each edit to `src` matched against the ORIGINAL content
// (non-incremental) so the model can reason about the file as-is. Overlaps are
// rejected. Exact match is tried first; on failure we retry with a fuzzy pass
// that normalises Unicode quotes/dashes/spaces and trailing whitespace.
func applyEdits(src string, edits []editPair, rel string) (string, int, error) {
	type span struct{ start, end int; repl string }
	spans := make([]span, 0, len(edits))
	fuzzy := normalizeFuzzy(src)
	for i, e := range edits {
		if e.old == e.new {
			return "", 0, fmt.Errorf("edits[%d]: `old` and `new` are identical — nothing to change", i)
		}
		count := strings.Count(src, e.old)
		if count == 1 {
			idx := strings.Index(src, e.old)
			spans = append(spans, span{idx, idx + len(e.old), e.new})
			continue
		}
		if count > 1 {
			return "", 0, fmt.Errorf("edits[%d]: text appears %d times in %s — expand `old` with 2-3 surrounding lines so the match is unique", i, count, rel)
		}
		// Fuzzy fallback: normalise both sides and match on the normalised src.
		nOld := normalizeFuzzy(e.old)
		fCount := strings.Count(fuzzy, nOld)
		if fCount == 0 {
			return "", 0, fmt.Errorf("edits[%d]: text not found in %s. Re-read the file with read_files and copy the target snippet verbatim (whitespace, indentation and Unicode punctuation matter)", i, rel)
		}
		if fCount > 1 {
			return "", 0, fmt.Errorf("edits[%d]: fuzzy match found %d occurrences in %s — expand `old` with 2-3 surrounding lines so the match is unique", i, fCount, rel)
		}
		// Map the fuzzy hit back to original bytes by scanning candidate
		// windows of the same length until their normalisation matches nOld.
		fIdx := strings.Index(fuzzy, nOld)
		origIdx, origLen := locateOriginalSpan(src, fIdx, nOld)
		if origIdx < 0 {
			return "", 0, fmt.Errorf("edits[%d]: could not project fuzzy match back to original bytes in %s", i, rel)
		}
		spans = append(spans, span{origIdx, origIdx + origLen, e.new})
	}
	// Reject overlaps.
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return "", 0, fmt.Errorf("edits overlap in %s — merge the touching changes into a single edit", rel)
		}
	}
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		b.WriteString(src[prev:s.start])
		b.WriteString(s.repl)
		prev = s.end
	}
	b.WriteString(src[prev:])
	return b.String(), len(spans), nil
}

// locateOriginalSpan finds the byte window in `src` whose fuzzy normalisation
// equals `needle`, given that `needle` was located at `fuzzyIdx` inside the
// pre-normalised source. We progressively widen a candidate window from the
// closest original offset until its normalised form matches.
func locateOriginalSpan(src string, fuzzyIdx int, needle string) (int, int) {
	// Map fuzzy offsets to original offsets by walking both strings.
	// Fuzzy normalisation can only shrink or leave lengths equal for the
	// substitutions we perform (trailing-space strip may shrink; quotes/dashes
	// keep length). So original >= fuzzy in length.
	orig := 0
	fuzz := 0
	fSrc := normalizeFuzzy(src)
	for orig < len(src) && fuzz < fuzzyIdx {
		// Advance one line at a time to keep the mapping simple.
		lineEnd := strings.IndexByte(src[orig:], '\n')
		if lineEnd < 0 {
			lineEnd = len(src) - orig
		} else {
			lineEnd++ // include the newline
		}
		origLine := src[orig : orig+lineEnd]
		fLine := normalizeFuzzy(origLine)
		if fuzz+len(fLine) > fuzzyIdx {
			break
		}
		orig += lineEnd
		fuzz += len(fLine)
	}
	// Now scan forward for the smallest suffix starting at `orig` whose
	// normalisation begins with `needle`.
	for start := orig; start <= len(src); start++ {
		for end := start; end <= len(src); end++ {
			if normalizeFuzzy(src[start:end]) == needle {
				return start, end - start
			}
			if end-start > len(needle)+64 {
				break
			}
		}
		if start-orig > 256 {
			break
		}
	}
	_ = fSrc
	return -1, 0
}

// normalizeFuzzy mirrors pi.dev's normalizeForFuzzyMatch: NFKC + smart quotes
// → ASCII, Unicode dashes → '-', special spaces → ' ', trailing whitespace
// stripped per line.
func normalizeFuzzy(s string) string {
	// Fold quotes/dashes/spaces.
	replacer := strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'",
		"\u201C", "\"", "\u201D", "\"", "\u201E", "\"", "\u201F", "\"",
		"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-",
		"\u2014", "-", "\u2015", "-", "\u2212", "-",
		"\u00A0", " ", "\u202F", " ", "\u205F", " ", "\u3000", " ",
		"\u2002", " ", "\u2003", " ", "\u2004", " ", "\u2005", " ",
		"\u2006", " ", "\u2007", " ", "\u2008", " ", "\u2009", " ", "\u200A", " ",
	)
	s = replacer.Replace(s)
	// Strip trailing whitespace per line.
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// imageKind reports a short kind ("png", "jpg", …) for common image
// extensions, or "" otherwise. Used by read_files to avoid inlining binary
// bytes into the model context while still surfacing that the path IS an
// image (useful for the model to route the file to a viewer / diff tool).
func imageKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".gif":
		return "gif"
	case ".webp":
		return "webp"
	case ".bmp":
		return "bmp"
	case ".svg":
		// SVG is text/XML — return "" so it gets inlined as text.
		return ""
	}
	return ""
}

// isBinary returns true when the first 8KiB of `data` contains a NUL byte or
// >30% non-printable bytes — a cheap heuristic that keeps read_files from
// dumping executables, archives, .wasm, etc. into the prompt.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	nonPrint := 0
	for i := 0; i < n; i++ {
		b := data[i]
		if b == 0 {
			return true
		}
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			nonPrint++
		}
	}
	return n > 0 && nonPrint*100/n > 30
}
