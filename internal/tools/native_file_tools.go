package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

type writeFileParameters struct {
	Type       string              `json:"type"`
	Properties writeFileProperties `json:"properties"`
	Required   []string            `json:"required"`
}

type writeFileProperties struct {
	Path           map[string]any `json:"path"`
	Overwrite      map[string]any `json:"overwrite"`
	ExpectedSHA256 map[string]any `json:"expected_sha256"`
	Content        map[string]any `json:"content"`
}

func newWriteFileParameters() writeFileParameters {
	return writeFileParameters{
		Type: "object",
		Properties: writeFileProperties{
			Path: map[string]any{
				"type":        "string",
				"description": "Target path. Emit this argument first so Lilith can inspect the destination before a large content body is streamed.",
			},
			Overwrite: map[string]any{
				"type":        "boolean",
				"description": "Required as true to replace an existing file. Omit or false when the target must be new.",
			},
			ExpectedSHA256: map[string]any{
				"type":        "string",
				"description": "Optional SHA-256 of the existing file. When supplied, the write is rejected if the file changed since it was read.",
			},
			Content: map[string]any{
				"type":        "string",
				"description": "Complete final content. The write is native and atomic; do not wrap it in shell commands, heredocs or base64.",
			},
		},
		Required: []string{"path", "content"},
	}
}

type appendFileParameters struct {
	Type       string               `json:"type"`
	Properties appendFileProperties `json:"properties"`
	Required   []string             `json:"required"`
}

type appendFileProperties struct {
	Path            map[string]any `json:"path"`
	CreateIfMissing map[string]any `json:"create_if_missing"`
	ExpectedSHA256  map[string]any `json:"expected_sha256"`
	Content         map[string]any `json:"content"`
}

func newAppendFileParameters() appendFileParameters {
	return appendFileParameters{
		Type: "object",
		Properties: appendFileProperties{
			Path: map[string]any{
				"type":        "string",
				"description": "Target path. Relative paths resolve against the project root.",
			},
			CreateIfMissing: map[string]any{
				"type":        "boolean",
				"description": "Create the file when absent. Defaults to true. Set false when absence should be treated as an error.",
			},
			ExpectedSHA256: map[string]any{
				"type":        "string",
				"description": "Optional SHA-256 of the current file. Rejects the append if another process changed it.",
			},
			Content: map[string]any{
				"type":        "string",
				"description": "Text to append. For long reports, append one complete section per call instead of using a shell heredoc.",
			},
		},
		Required: []string{"path", "content"},
	}
}

// PreflightWriteFile lets the streaming TUI stop a full-file payload before
// the model emits its body when the destination exists but overwrite was not
// explicitly authorized.
func PreflightWriteFile(root, rel string, overwrite bool) (result string, blocked bool, err error) {
	full, err := resolve(root, rel)
	if err != nil {
		return "", false, err
	}
	if overwrite {
		return "", false, nil
	}
	inspection, err := inspectFile(full, true)
	if err != nil {
		return "", false, err
	}
	if !inspection.Exists {
		return "", false, nil
	}
	return fmt.Sprintf(
		"OVERWRITE_REQUIRED: %s already exists (%d bytes, sha256 %s). Retry write_file with overwrite=true only if replacing the whole file is intentional; otherwise use str_replace/apply_diff.",
		rel, inspection.Size, inspection.SHA256,
	), true, nil
}

func init() {
	register(Definition{
		Name: "write_file",
		Description: "Write complete file content directly through Lilith without a shell. The operation uses a temporary file, verifies the exact byte count and SHA-256, then atomically replaces the destination. " +
			"New files are allowed by default. Replacing an existing file requires `overwrite: true`; use optional `expected_sha256` to reject a destination whose observed content changed since it was read. " +
			"Use this for generated reports or intentional full-file regeneration, not for small source edits where str_replace/apply_diff is safer.",
		PromptSnippet: "Atomically create or intentionally replace a complete file without shell redirection",
		PromptGuidelines: []string{
			"Use write_file for complete generated documents and intentional full-file regeneration. Existing targets require overwrite=true; prefer expected_sha256 when replacing content you previously inspected.",
			"Never create long files with shell heredocs, printf, PowerShell here-strings or base64. Pass the content directly to write_file, or build it section-by-section with append_file.",
		},
		Mutating:   true,
		Parameters: newWriteFileParameters(),
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			content := []byte(str(args, "content"))
			if len(content) > MaxNativeWriteBytes {
				return "", fmt.Errorf("content is %d bytes; write_file accepts at most %d bytes per call. Use append_file in smaller complete sections", len(content), MaxNativeWriteBytes)
			}
			mu := lockFile(full)
			mu.Lock()
			defer mu.Unlock()

			needHash := !boolArg(args, "overwrite") || strings.TrimSpace(str(args, "expected_sha256")) != ""
			inspection, err := inspectFile(full, needHash)
			if err != nil {
				return "", err
			}
			exists := inspection.Exists
			if exists && !boolArg(args, "overwrite") {
				return fmt.Sprintf(
					"OVERWRITE_REQUIRED: %s already exists (%d bytes, sha256 %s). Retry write_file with overwrite=true only if replacing the whole file is intentional; otherwise use str_replace/apply_diff.",
					rel, inspection.Size, inspection.SHA256,
				), nil
			}
			if exists {
				if err := validateExpectedSHAValue(str(args, "expected_sha256"), inspection.SHA256, rel); err != nil {
					return "", err
				}
			} else if strings.TrimSpace(str(args, "expected_sha256")) != "" {
				return "", fmt.Errorf("FILE_CHANGED: %s does not exist, but expected_sha256 was supplied. Re-read the path before writing", rel)
			}
			var report fileWriteReport
			if exists {
				report, err = atomicWriteFile(ctx, full, content, 0o644)
			} else {
				report, err = atomicCreateFile(ctx, full, content, 0o644)
			}
			if err != nil {
				if !exists && errors.Is(err, os.ErrExist) {
					if boolArg(args, "overwrite") {
						return "", fmt.Errorf("FILE_CHANGED: %s appeared while write_file was preparing the new file. Re-read the path before deciding whether to overwrite it", rel)
					}
					if result, blocked, preflightErr := PreflightWriteFile(env.Root, rel, false); preflightErr == nil && blocked {
						return result, nil
					}
				}
				return "", err
			}
			action := "created"
			if exists {
				action = "replaced"
			}
			return formatWriteReport(action, rel, len(content), report.Bytes, report), nil
		},
	})

	register(Definition{
		Name: "append_file",
		Description: "Append text to a file directly without shell redirection. Lilith reads the current file under a per-path lock, combines it with the supplied content, writes a temporary file and atomically replaces the destination. " +
			"The final byte count and SHA-256 are verified. This is intended for long reports or logs assembled in bounded sections. `create_if_missing` defaults to true.",
		PromptSnippet: "Atomically append a bounded section to a file without a shell heredoc",
		PromptGuidelines: []string{
			"Use append_file to build long reports one complete section at a time. Keep each call bounded, pass the previous result SHA-256 as expected_sha256 when chaining sections, and validate the final document with read_files or a line-count command.",
			"Do not use shell heredocs or output redirection for large generated content; append_file cannot leave a partially appended destination.",
		},
		Mutating:   true,
		Parameters: newAppendFileParameters(),
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			rel := str(args, "path")
			full, err := resolve(env.Root, rel)
			if err != nil {
				return "", err
			}
			addition := []byte(str(args, "content"))
			if len(addition) > MaxNativeWriteBytes {
				return "", fmt.Errorf("content is %d bytes; append_file accepts at most %d bytes per call. Split the document into smaller complete sections", len(addition), MaxNativeWriteBytes)
			}
			mu := lockFile(full)
			mu.Lock()
			defer mu.Unlock()

			inspection, inspectErr := inspectFile(full, false)
			if inspectErr != nil {
				return "", inspectErr
			}
			if inspection.Exists && inspection.Size > MaxNativeFileBytes {
				return "", fmt.Errorf("existing file is %d bytes; maximum final file size is %d bytes", inspection.Size, MaxNativeFileBytes)
			}
			current, readErr := os.ReadFile(full)
			exists := readErr == nil
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return "", readErr
			}
			createIfMissing := true
			if value, ok := args["create_if_missing"].(bool); ok {
				createIfMissing = value
			}
			if !exists && !createIfMissing {
				return "", fmt.Errorf("%s does not exist and create_if_missing=false", rel)
			}
			if exists {
				if err := validateExpectedSHA(str(args, "expected_sha256"), current, rel); err != nil {
					return "", err
				}
			} else if strings.TrimSpace(str(args, "expected_sha256")) != "" {
				return "", fmt.Errorf("FILE_CHANGED: %s does not exist, but expected_sha256 was supplied. Re-read the path before appending", rel)
			}
			if len(current)+len(addition) > MaxNativeFileBytes {
				return "", fmt.Errorf("append would produce %d bytes; maximum final file size is %d bytes", len(current)+len(addition), MaxNativeFileBytes)
			}
			combined := make([]byte, 0, len(current)+len(addition))
			combined = append(combined, current...)
			combined = append(combined, addition...)
			var report fileWriteReport
			if exists {
				report, err = atomicWriteFile(ctx, full, combined, 0o644)
			} else {
				report, err = atomicCreateFile(ctx, full, combined, 0o644)
			}
			if err != nil {
				if !exists && errors.Is(err, os.ErrExist) {
					return "", fmt.Errorf("FILE_CHANGED: %s appeared while append_file was preparing a new file. Re-read it before appending", rel)
				}
				return "", err
			}
			return fmt.Sprintf(
				"appended %s\nbytes_appended: %d\ntotal_bytes: %d\nlines: %d\nsha256: %s\natomic: yes",
				rel, len(addition), report.Bytes, report.Lines, report.SHA256,
			), nil
		},
	})
}
