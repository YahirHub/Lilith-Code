package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
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

// createFileParameters intentionally uses structs instead of map[string]any.
// encoding/json preserves struct field order, so `path` is serialized before
// `content` in the tool schema. Streaming models tend to follow that order;
// receiving the path first lets Lilith preflight an existing target before the
// model spends hundreds of tokens generating a body that will be rejected.
type createFileParameters struct {
	Type       string               `json:"type"`
	Properties createFileProperties `json:"properties"`
	Required   []string             `json:"required"`
}

type createFileProperties struct {
	Path    map[string]any `json:"path"`
	Content map[string]any `json:"content"`
}

func newCreateFileParameters() createFileParameters {
	return createFileParameters{
		Type: "object",
		Properties: createFileProperties{
			Path:    map[string]any{"type": "string", "description": "Target file path (project-relative or absolute). Emit this argument before content."},
			Content: map[string]any{"type": "string", "description": "Full, final content of the NEW file. No placeholders, no `...`, no `// rest of code`."},
		},
		Required: []string{"path", "content"},
	}
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	"vendor": true, ".next": true, "target": true, "bin": true, ".li": true,
}

// PreflightCreateFile checks a create-only target without writing anything.
// It is also used by the TUI while tool arguments are still streaming so an
// existing path can be rejected before the model spends tokens generating the
// rest of a large file body.
func PreflightCreateFile(root, rel string) (result string, exists bool, err error) {
	full, err := resolve(root, rel)
	if err != nil {
		return "", false, err
	}
	info, statErr := os.Stat(full)
	if errors.Is(statErr, os.ErrNotExist) {
		return "", false, nil
	}
	if statErr != nil {
		return "", false, statErr
	}
	if info.IsDir() {
		return fmt.Sprintf("FILE_EXISTS: %s already exists and is a directory. Choose a new file path; do not retry create_file.", rel), true, nil
	}
	return fmt.Sprintf("FILE_EXISTS: %s already exists (%d bytes). Use str_replace for targeted edits or apply_diff for a unified patch. Do not retry create_file.", rel, info.Size()), true, nil
}

// InterceptLegacyWrite handles model-hallucinated write/write_file calls without
// ever mutating disk. Those names are deliberately not exposed in Lilith's
// schemas, but some coding models still emit them because other agents use them
// for overwrite semantics. Returning a compact actionable tool result lets the
// same turn recover with create_file or str_replace/apply_diff instead of
// failing as an unknown tool or rewriting an existing file.
func InterceptLegacyWrite(root, toolName, rel string) (string, error) {
	if toolName != "write" && toolName != "write_file" {
		return "", fmt.Errorf("unsupported legacy write tool: %s", toolName)
	}
	path := strings.TrimSpace(rel)
	if path == "" {
		return "WRITE_BLOCKED: missing path. Use create_file for a new file or str_replace/apply_diff for an existing file.", nil
	}
	result, exists, err := PreflightCreateFile(root, path)
	if err != nil {
		return "", err
	}
	if exists {
		return result + " The requested write/write_file call was blocked before writing any bytes.", nil
	}
	return fmt.Sprintf("USE_CREATE_FILE: %s does not exist. Use create_file to create it; write/write_file are not executable tools in Lilith.", path), nil
}

func init() {
	register(Definition{
		Name: "read_files",
		Description: "Read one or more files. Accepts project-relative paths or absolute paths (e.g. a skill's own scripts/assets under ~/.li/skills). " +
			"For large text files use `offset` (1-indexed start line) and `limit` (max lines) to paginate instead of pulling the whole file into context; " +
			"the response reports the visible range and the next offset when there is more content.",
		PromptSnippet: "Read file contents with optional offset/limit pagination",
		PromptGuidelines: []string{
			"Use read_files to inspect source before reasoning about exact code; paginate large files instead of loading them all at once.",
		},
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
					fmt.Fprintf(&b, "== %s ==\n[image %s, %d bytes — binary content not inlined; open in an external viewer if you need to see it]\n\n", p, kind, len(data))
					continue
				}
				if isBinary(data) {
					fmt.Fprintf(&b, "== %s ==\n[binary file, %d bytes — refusing to inline; use read_url or a specialised tool]\n\n", p, len(data))
					continue
				}
				content, note := sliceLines(string(data), offset, limit)
				if note == "" && len(content) > MaxFileBytes {
					content = content[:MaxFileBytes]
					note = fmt.Sprintf("[truncated at %d bytes — use offset/limit to paginate]", MaxFileBytes)
				}
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
		Name: "create_file",
		Description: "Create a NEW file with the full final content. " +
			"This tool never overwrites an existing file. If the target already exists, the call is skipped and returns FILE_EXISTS; " +
			"switch to str_replace for targeted changes or apply_diff for a unified patch. " +
			"Never call create_file with placeholders, ellipsis, or partial content.",
		PromptSnippet: "Create new files only; existing targets are skipped",
		PromptGuidelines: []string{
			"Use create_file only when the target path is intended to be new. Never use it to modify, replace, rewrite, fix, refactor or regenerate an existing file. If it returns FILE_EXISTS, do not retry it; use str_replace or apply_diff.",
			"When calling create_file, emit the path argument before content so Lilith can preflight the target before a large body is generated.",
		},
		Mutating:   true,
		Parameters: newCreateFileParameters(),
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			mu := lockFile(full)
			mu.Lock()
			defer mu.Unlock()
			if result, exists, err := PreflightCreateFile(env.Root, rel); err != nil {
				return "", err
			} else if exists {
				return result, nil
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return "", err
			}
			content := str(args, "content")
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %s (%d bytes).", rel, len(content)), nil
		},
	})

	register(Definition{
		Name: "str_replace",
		Description: "Edit an existing file with one or more targeted text replacements. " +
			"Pass either a single `old`/`new` pair OR an `edits` array `[{old, new}, ...]`; `edits` may also arrive as a JSON string and is normalized automatically. " +
			"Every edit is validated against the CURRENT file at execution time, so a separate read_files call is recommended for understanding the code but is not a runtime prerequisite. " +
			"Each non-empty `old` must identify exactly one non-overlapping region of the original file; add nearby context when needed. " +
			"Exact matching is tried first, then a pi.dev-compatible fuzzy pass normalizes line endings, Unicode NFKC, smart quotes/dashes/spaces and trailing whitespace while preserving unchanged bytes, UTF-8 BOM and the file's CRLF/LF style. " +
			"`new` may be empty to delete. Pairs where `old == new` are harmless no-ops. Do not use str_replace to create files.",
		PromptSnippet: "Make precise replacements in existing files, including multiple disjoint edits in one call",
		PromptGuidelines: []string{
			"Use str_replace for precise existing-file changes. Each old snippet must be non-empty and unique in the current file.",
			"When changing multiple separate regions in one file, prefer one str_replace call with edits[]; all entries match the original file and must not overlap.",
			"Keep old snippets as small as possible while still unique. If a match fails or is ambiguous, read the affected region and retry with current text.",
		},
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
			if applied == 0 {
				return fmt.Sprintf("no changes needed in %s (all replacements were already identical).", rel), nil
			}
			if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
				return "", err
			}
			if applied == 1 {
				return fmt.Sprintf("edited %s (1 replacement).", rel), nil
			}
			return fmt.Sprintf("edited %s (%d replacements).", rel, applied), nil
		},
	})

	register(Definition{
		Name:          "list_directory",
		Description:   "List files and folders in a directory (project-relative or absolute). Entries are sorted alphabetically with a trailing `/` on directories; dotfiles are included. Output is capped by `limit` (default 500) so large trees stay bounded.",
		PromptSnippet: "List directory contents",
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
		Name:          "glob",
		Description:   "Find files matching a glob pattern (e.g. `**/*.go` or `internal/**/*_test.go`).",
		PromptSnippet: "Find files by glob pattern",
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

func stringField(args map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		v, ok := args[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		return s, true
	}
	return "", false
}

// collectEdits accepts Lilith's native {old,new} shape plus the oldText/newText
// aliases used by pi.dev. Some models serialize edits[] as a JSON string; pi
// tolerates that, so Lilith normalizes it before validating as well.
func collectEdits(args map[string]any) ([]editPair, error) {
	var rawList []any
	if raw, ok := args["edits"]; ok && raw != nil {
		switch value := raw.(type) {
		case []any:
			rawList = value
		case []map[string]any:
			rawList = make([]any, len(value))
			for i := range value {
				rawList[i] = value[i]
			}
		case string:
			if strings.TrimSpace(value) != "" {
				if err := json.Unmarshal([]byte(value), &rawList); err != nil {
					return nil, fmt.Errorf("`edits` is a string but is not a valid JSON array: %w", err)
				}
			}
		default:
			return nil, errors.New("`edits` must be an array of {old, new} objects")
		}
	}

	out := make([]editPair, 0, len(rawList))
	for i, item := range rawList {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edits[%d] must be an object with `old` and `new` string fields", i)
		}
		old, oldOK := stringField(m, "old", "oldText")
		newText, newOK := stringField(m, "new", "newText")
		if !oldOK || !newOK {
			return nil, fmt.Errorf("edits[%d] must contain string `old` and `new` fields", i)
		}
		if old == "" {
			return nil, fmt.Errorf("edits[%d].old is empty. Use a real existing anchor; to insert, repeat that anchor inside `new`", i)
		}
		out = append(out, editPair{old: old, new: newText})
	}

	if len(out) == 0 {
		old, oldOK := stringField(args, "old", "oldText")
		newText, newOK := stringField(args, "new", "newText")
		if !oldOK || old == "" {
			return nil, errors.New("`old` must not be empty. Use exact current text as the target; to insert, reuse an existing anchor inside `new`. To create a file, use create_file")
		}
		if !newOK {
			newText = ""
		}
		out = append(out, editPair{old: old, new: newText})
	}
	return out, nil
}

type textReplacement struct {
	editIndex            int
	matchIndex, matchLen int
	newText              string
}

type lineSpan struct{ start, end int }

func detectLineEnding(content string) string {
	lf := strings.IndexByte(content, '\n')
	if lf < 0 {
		return "\n"
	}
	if lf > 0 && content[lf-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

func stripUTF8BOM(content string) (string, string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", strings.TrimPrefix(content, "\uFEFF")
	}
	return "", content
}

func splitLinesWithEndings(content string) []string {
	if content == "" {
		return nil
	}
	var lines []string
	for len(content) > 0 {
		i := strings.IndexByte(content, '\n')
		if i < 0 {
			lines = append(lines, content)
			break
		}
		lines = append(lines, content[:i+1])
		content = content[i+1:]
	}
	return lines
}

func lineSpans(content string) []lineSpan {
	lines := splitLinesWithEndings(content)
	out := make([]lineSpan, 0, len(lines))
	offset := 0
	for _, line := range lines {
		out = append(out, lineSpan{start: offset, end: offset + len(line)})
		offset += len(line)
	}
	return out
}

func replacementLineRange(lines []lineSpan, repl textReplacement) (int, int, error) {
	start, end := repl.matchIndex, repl.matchIndex+repl.matchLen
	startLine := -1
	for i, line := range lines {
		if start >= line.start && start < line.end {
			startLine = i
			break
		}
	}
	if startLine < 0 {
		return 0, 0, errors.New("replacement range is outside the base content")
	}
	endLine := startLine
	for endLine < len(lines) && lines[endLine].end < end {
		endLine++
	}
	if endLine >= len(lines) {
		return 0, 0, errors.New("replacement range is outside the base content")
	}
	return startLine, endLine + 1, nil
}

func applyTextReplacements(content string, replacements []textReplacement, offset int) string {
	out := content
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		idx := r.matchIndex - offset
		out = out[:idx] + r.newText + out[idx+r.matchLen:]
	}
	return out
}

// applyReplacementsPreservingUnchangedLines mirrors pi.dev's edit-diff logic.
// When fuzzy matching requires a normalized view, only lines touched by edits
// are rebuilt from that view; untouched lines retain their original bytes.
func applyReplacementsPreservingUnchangedLines(original, base string, replacements []textReplacement) (string, error) {
	originalLines := splitLinesWithEndings(original)
	baseLines := lineSpans(base)
	if len(originalLines) != len(baseLines) {
		return "", errors.New("cannot preserve unchanged lines because normalized content changed the line count")
	}
	type group struct {
		startLine, endLine int
		replacements       []textReplacement
	}
	sorted := append([]textReplacement(nil), replacements...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].matchIndex < sorted[j].matchIndex })
	var groups []group
	for _, repl := range sorted {
		startLine, endLine, err := replacementLineRange(baseLines, repl)
		if err != nil {
			return "", err
		}
		if len(groups) > 0 && startLine < groups[len(groups)-1].endLine {
			g := &groups[len(groups)-1]
			if endLine > g.endLine {
				g.endLine = endLine
			}
			g.replacements = append(g.replacements, repl)
			continue
		}
		groups = append(groups, group{startLine: startLine, endLine: endLine, replacements: []textReplacement{repl}})
	}

	var b strings.Builder
	originalLineIndex := 0
	for _, g := range groups {
		for _, line := range originalLines[originalLineIndex:g.startLine] {
			b.WriteString(line)
		}
		groupStart := baseLines[g.startLine].start
		groupEnd := baseLines[g.endLine-1].end
		b.WriteString(applyTextReplacements(base[groupStart:groupEnd], g.replacements, groupStart))
		originalLineIndex = g.endLine
	}
	for _, line := range originalLines[originalLineIndex:] {
		b.WriteString(line)
	}
	return b.String(), nil
}

func fuzzyFind(content, oldText string) (index, matchLen int, usedFuzzy, found bool) {
	if idx := strings.Index(content, oldText); idx >= 0 {
		return idx, len(oldText), false, true
	}
	fuzzyContent := normalizeFuzzy(content)
	fuzzyOld := normalizeFuzzy(oldText)
	if fuzzyOld == "" {
		return -1, 0, false, false
	}
	if idx := strings.Index(fuzzyContent, fuzzyOld); idx >= 0 {
		return idx, len(fuzzyOld), true, true
	}
	return -1, 0, false, false
}

func fuzzyOccurrenceCount(content, oldText string) int {
	oldText = normalizeFuzzy(oldText)
	if oldText == "" {
		return 0
	}
	return strings.Count(normalizeFuzzy(content), oldText)
}

// applyEdits follows pi.dev's important safety property: every replacement is
// checked against the file as it exists at execution time. A prior read_files
// call is useful for the model but is not trusted as a freshness guarantee.
func applyEdits(src string, edits []editPair, rel string) (string, int, error) {
	bom, content := stripUTF8BOM(src)
	ending := detectLineEnding(content)
	normalizedContent := normalizeToLF(content)

	type indexedEdit struct {
		index int
		old   string
		new   string
	}
	active := make([]indexedEdit, 0, len(edits))
	for i, edit := range edits {
		oldText := normalizeToLF(edit.old)
		newText := normalizeToLF(edit.new)
		if oldText == "" {
			return "", 0, fmt.Errorf("edits[%d].old must not be empty in %s", i, rel)
		}
		if oldText == newText {
			continue
		}
		active = append(active, indexedEdit{index: i, old: oldText, new: newText})
	}
	if len(active) == 0 {
		return src, 0, nil
	}

	usedFuzzy := false
	for _, edit := range active {
		_, _, fuzzy, found := fuzzyFind(normalizedContent, edit.old)
		if !found {
			return "", 0, fmt.Errorf("edits[%d]: target text was not found in the current %s. Read the affected region and retry with current text", edit.index, rel)
		}
		usedFuzzy = usedFuzzy || fuzzy
	}

	base := normalizedContent
	if usedFuzzy {
		base = normalizeFuzzy(normalizedContent)
	}

	replacements := make([]textReplacement, 0, len(active))
	for _, edit := range active {
		idx, matchLen, _, found := fuzzyFind(base, edit.old)
		if !found {
			return "", 0, fmt.Errorf("edits[%d]: target text was not found in the current %s. Read the affected region and retry with current text", edit.index, rel)
		}
		occurrences := fuzzyOccurrenceCount(base, edit.old)
		if occurrences > 1 {
			return "", 0, fmt.Errorf("edits[%d]: target matches %d locations in %s. Read the affected region and add only enough surrounding context to make it unique", edit.index, occurrences, rel)
		}
		replacements = append(replacements, textReplacement{
			editIndex:  edit.index,
			matchIndex: idx,
			matchLen:   matchLen,
			newText:    edit.new,
		})
	}

	sort.Slice(replacements, func(i, j int) bool { return replacements[i].matchIndex < replacements[j].matchIndex })
	for i := 1; i < len(replacements); i++ {
		prev, cur := replacements[i-1], replacements[i]
		if prev.matchIndex+prev.matchLen > cur.matchIndex {
			return "", 0, fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge touching changes into one edit", prev.editIndex, cur.editIndex, rel)
		}
	}

	var newContent string
	if usedFuzzy {
		var err error
		newContent, err = applyReplacementsPreservingUnchangedLines(normalizedContent, base, replacements)
		if err != nil {
			return "", 0, fmt.Errorf("apply fuzzy edits to %s: %w", rel, err)
		}
	} else {
		newContent = applyTextReplacements(base, replacements, 0)
	}

	final := bom + restoreLineEndings(newContent, ending)
	if final == src {
		return src, 0, nil
	}
	return final, len(active), nil
}

// normalizeFuzzy mirrors pi.dev's normalizeForFuzzyMatch: NFKC + smart quotes
// -> ASCII, Unicode dashes -> '-', special spaces -> ' ', trailing whitespace
// stripped per line.
func normalizeFuzzy(s string) string {
	s = norm.NFKC.String(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	s = strings.Join(lines, "\n")
	replacer := strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'",
		"\u201C", "\"", "\u201D", "\"", "\u201E", "\"", "\u201F", "\"",
		"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-",
		"\u2014", "-", "\u2015", "-", "\u2212", "-",
		"\u00A0", " ", "\u202F", " ", "\u205F", " ", "\u3000", " ",
		"\u2002", " ", "\u2003", " ", "\u2004", " ", "\u2005", " ",
		"\u2006", " ", "\u2007", " ", "\u2008", " ", "\u2009", " ", "\u200A", " ",
	)
	return replacer.Replace(s)
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
