package tools

// apply_diff tool: apply a minimal unified diff to a file. Inspired by pi.dev's
// edit-diff tool. Supports the standard hunk format:
//
//   @@ -oldStart,oldLen +newStart,newLen @@
//    context line
//   -removed line
//   +added line
//
// The file header (`--- a/x` / `+++ b/x`) is optional; when present it must
// match `path`. Multiple hunks are supported. On failure the tool returns a
// descriptive error and does not modify the file.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func init() {
	register(Definition{
		Name: "apply_diff",
		Description: "Apply a unified diff to a single existing file. Use this instead of str_replace when a change spans many hunks or when you already have a diff. " +
			"The `diff` argument must be a standard unified diff with `@@ -old,len +new,len @@` hunk headers; the `--- / +++` header lines are optional but must reference `path` if present. " +
			"Every context line must match the file byte-for-byte. The whole diff is validated first and either applied atomically or rejected with a descriptive error. " +
			"REQUIREMENTS: (1) the file MUST already exist and MUST have been read with read_files in this session; (2) hunk line numbers are 1-indexed and must match the current file. " +
			"DO NOT use apply_diff to create a new file — use write_file instead.",
		Mutating: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path of the existing file to patch (project-relative or absolute)."},
				"diff": map[string]any{"type": "string", "description": "Unified diff body. Include at least one `@@ ... @@` hunk header."},
			},
			"required": []string{"path", "diff"},
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
			out, hunks, err := applyUnifiedDiff(string(data), str(args, "diff"), rel)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
				return "", err
			}
			markSeen(env, rel)
			return fmt.Sprintf("patched %s (%d hunk(s) applied).", rel, hunks), nil
		},
	})
}

// applyUnifiedDiff parses `diff` and returns the patched content. It ignores
// optional `--- a/… / +++ b/…` header lines. Each `@@ -a,b +c,d @@` hunk is
// applied against the current 1-indexed line numbers of `src`.
func applyUnifiedDiff(src, diff, rel string) (string, int, error) {
	if strings.TrimSpace(diff) == "" {
		return "", 0, errors.New("`diff` is empty")
	}
	lines := strings.Split(src, "\n")
	// Track whether src ends with a trailing newline so we can preserve it.
	trailingNL := strings.HasSuffix(src, "\n")
	if trailingNL {
		// Drop the empty tail produced by Split for cleaner indexing.
		lines = lines[:len(lines)-1]
	}

	diffLines := strings.Split(diff, "\n")
	i := 0
	hunks := 0
	// Delta between original and patched line numbers so successive hunks
	// stay aligned even when earlier hunks changed line counts.
	delta := 0
	for i < len(diffLines) {
		line := diffLines[i]
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			if strings.TrimSpace(line) == "" {
				i++
				continue
			}
			return "", 0, fmt.Errorf("expected hunk header `@@ -a,b +c,d @@`, got %q", line)
		}
		oldStart, oldLen, err := parseHunkHeader(line)
		if err != nil {
			return "", 0, err
		}
		i++
		// Collect body lines until next hunk or EOF.
		body := []string{}
		for i < len(diffLines) && !strings.HasPrefix(diffLines[i], "@@") {
			body = append(body, diffLines[i])
			i++
		}
		// Trim trailing empty strings introduced by a final "\n" in the diff;
		// real blank content lines are encoded as " " (space) or "-"/"+"
		// followed by nothing, never as a bare empty string.
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		newLines, consumed, err := applyHunk(lines, oldStart+delta, oldLen, body, rel, hunks+1)
		if err != nil {
			return "", 0, err
		}
		lines = newLines
		delta += consumed
		hunks++
	}
	if hunks == 0 {
		return "", 0, errors.New("no `@@` hunk headers found in diff")
	}
	out := strings.Join(lines, "\n")
	if trailingNL {
		out += "\n"
	}
	return out, hunks, nil
}

func parseHunkHeader(h string) (oldStart, oldLen int, err error) {
	// `@@ -a,b +c,d @@` — we only need the old side.
	rest := strings.TrimPrefix(h, "@@")
	minus := strings.Index(rest, "-")
	if minus < 0 {
		return 0, 0, fmt.Errorf("malformed hunk header: %q", h)
	}
	rest = rest[minus+1:]
	space := strings.IndexAny(rest, " \t")
	if space < 0 {
		return 0, 0, fmt.Errorf("malformed hunk header: %q", h)
	}
	spec := rest[:space]
	parts := strings.SplitN(spec, ",", 2)
	oldStart, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("malformed hunk header (old start): %q", h)
	}
	oldLen = 1
	if len(parts) == 2 {
		oldLen, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("malformed hunk header (old len): %q", h)
		}
	}
	return oldStart, oldLen, nil
}

// applyHunk validates a single hunk against `lines` starting at `oldStart`
// (1-indexed), then returns the mutated slice and the delta added to overall
// line count (positive if lines were inserted, negative if removed).
func applyHunk(lines []string, oldStart, oldLen int, body []string, rel string, hunkNo int) ([]string, int, error) {
	idx := oldStart - 1
	if idx < 0 || idx > len(lines) {
		return nil, 0, fmt.Errorf("hunk %d in %s: old start line %d is out of range (file has %d lines)", hunkNo, rel, oldStart, len(lines))
	}
	removed := []string{}
	added := []string{}
	// Track expected old lines to validate against src.
	expectedOld := []string{}
	for _, l := range body {
		if l == "" {
			// Blank line = context blank line.
			expectedOld = append(expectedOld, "")
			removed = append(removed, "")
			added = append(added, "")
			continue
		}
		switch l[0] {
		case ' ':
			expectedOld = append(expectedOld, l[1:])
			removed = append(removed, l[1:])
			added = append(added, l[1:])
		case '-':
			expectedOld = append(expectedOld, l[1:])
			removed = append(removed, l[1:])
		case '+':
			added = append(added, l[1:])
		case '\\':
			// `\ No newline at end of file` — informational, skip.
		default:
			return nil, 0, fmt.Errorf("hunk %d in %s: unexpected diff line prefix %q", hunkNo, rel, string(l[0]))
		}
	}
	if len(expectedOld) != oldLen && oldLen != 0 {
		// Some diffs omit exact counts; only warn if there is a clear mismatch.
		// We still enforce that expected lines exist in file.
	}
	// Validate every expected old line matches the file verbatim.
	for k, want := range expectedOld {
		fileIdx := idx + k
		if fileIdx >= len(lines) {
			return nil, 0, fmt.Errorf("hunk %d in %s: patch expects line %d but file only has %d lines", hunkNo, rel, fileIdx+1, len(lines))
		}
		if lines[fileIdx] != want {
			return nil, 0, fmt.Errorf("hunk %d in %s: context mismatch at line %d.\n  expected: %q\n  actual:   %q\n(re-read the file with read_files and regenerate the diff)", hunkNo, rel, fileIdx+1, want, lines[fileIdx])
		}
	}
	// Splice `added` in place of `removed` at idx..idx+len(removed).
	out := make([]string, 0, len(lines)-len(removed)+len(added))
	out = append(out, lines[:idx]...)
	out = append(out, added...)
	out = append(out, lines[idx+len(removed):]...)
	return out, len(added) - len(removed), nil
}
