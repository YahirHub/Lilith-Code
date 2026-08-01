package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lilith/li/internal/shell"
	"github.com/lilith/li/internal/toolchain"
)

// bashOutputMaxLines / bashOutputMaxBytes bound the inline snippet we return
// to the model. Anything longer is tail-truncated and the full stream is saved
// to a temp file so the model can inspect it with read_files if needed. This
// mirrors pi.dev's bash tool behaviour.
const (
	bashOutputMaxLines = 200
	bashOutputMaxBytes = 32 << 10
)

func init() {
	register(Definition{
		Name: "run_terminal_command",
		Description: fmt.Sprintf(
			"Run a shell command in the project directory (bash, or busybox sh on Windows) and return stdout, "+
				"stderr and the exit code. `timeout_seconds` is optional: when omitted the command runs until it finishes or "+
				"the user cancels it. Set a positive value only when a hard deadline is actually required. Output is tail-truncated to the last %d lines / %dKB per "+
				"stream; when truncated the full stream is saved to a temp file whose path is reported so you can inspect "+
				"it with read_files.",
			bashOutputMaxLines, bashOutputMaxBytes/1024,
		),
		PromptSnippet: "Execute shell commands in the project directory",
		PromptGuidelines: []string{
			"Use run_terminal_command for builds, tests, git and shell inspection; prefer non-interactive commands. Omit timeout_seconds for long builds, installs and test suites unless a hard deadline is explicitly needed.",
			"When discarding shell output use /dev/null. Never redirect to a literal file named null; Lilith normalizes common accidental null redirections defensively.",
		},
		Mutating: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string", "description": "Full command line to run."},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "description": "Optional hard deadline in seconds. When omitted, the command has no execution timeout and runs until completion or user cancellation."},
			},
			"required": []string{"command"},
		},
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			timeout := terminalCommandTimeout(args)
			res, err := shell.Run(ctx, shell.Request{
				Command: str(args, "command"),
				Dir:     env.Root,
				Timeout: timeout,
			})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "exit_code: %d\n", res.ExitCode)
			if res.TimedOut {
				b.WriteString("timeout: yes\n")
			}
			if res.Canceled {
				b.WriteString("canceled: yes\n")
			}
			if s := strings.TrimSpace(res.Stdout); s != "" {
				body, note := tailTruncate(s, "stdout")
				fmt.Fprintf(&b, "stdout:\n%s\n", body)
				if note != "" {
					fmt.Fprintf(&b, "%s\n", note)
				}
			}
			if s := strings.TrimSpace(res.Stderr); s != "" {
				body, note := tailTruncate(s, "stderr")
				fmt.Fprintf(&b, "stderr:\n%s\n", body)
				if note != "" {
					fmt.Fprintf(&b, "%s\n", note)
				}
			}
			return b.String(), nil
		},
	})

	register(Definition{
		Name: "code_search",
		Description: "Search file contents for a pattern (uses ripgrep). Returns matches as `path:line:text`. " +
			"Options: `glob` filters files (e.g. `*.go`), `path` scopes the search directory (default: project root), " +
			"`literal` treats `pattern` as a fixed string instead of a regex, `ignore_case` enables case-insensitive matching, " +
			"`context` shows N lines before/after each match, and `limit` caps total matches (default 100). " +
			"Long output is truncated; long lines are individually truncated to 500 chars.",
		PromptSnippet: "Search file contents with ripgrep",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Regex (default) or literal string when `literal` is true."},
				"glob":        map[string]any{"type": "string", "description": "Optional file filter, e.g. `*.go` or `**/*_test.go`."},
				"path":        map[string]any{"type": "string", "description": "Directory or file to search (default: project root)."},
				"literal":     map[string]any{"type": "boolean", "description": "Treat `pattern` as a fixed string instead of a regex (default false)."},
				"ignore_case": map[string]any{"type": "boolean", "description": "Case-insensitive match (default false)."},
				"context":     map[string]any{"type": "integer", "description": "Lines of context to show before/after each match (default 0)."},
				"limit":       map[string]any{"type": "integer", "description": "Maximum matches (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			pattern := str(args, "pattern")
			if strings.TrimSpace(pattern) == "" {
				return "", fmt.Errorf("empty pattern")
			}
			rg := toolchain.Lookup("rg")
			if rg == "" {
				return "", fmt.Errorf("ripgrep not installed: run `go run ./cmd/build install`")
			}
			limit := intArg(args, "limit", 100)
			cmdArgs := []string{"--line-number", "--no-heading", "--color", "never", "--max-count", "50", "--max-columns", "500"}
			if boolArg(args, "literal") {
				cmdArgs = append(cmdArgs, "--fixed-strings")
			}
			if boolArg(args, "ignore_case") {
				cmdArgs = append(cmdArgs, "--ignore-case")
			}
			if c := intArg(args, "context", 0); c > 0 {
				cmdArgs = append(cmdArgs, fmt.Sprintf("--context=%d", c))
			}
			if g := str(args, "glob"); g != "" {
				cmdArgs = append(cmdArgs, "--glob", g)
			}
			target := str(args, "path")
			if target == "" {
				target = "."
			}
			cmdArgs = append(cmdArgs, pattern, target)
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(runCtx, rg, cmdArgs...)
			cmd.Dir = env.Root
			out, err := cmd.CombinedOutput()
			text := string(out)
			// Cap total matches (by line) to `limit`.
			if limit > 0 {
				if lines := strings.Split(text, "\n"); len(lines) > limit {
					text = strings.Join(lines[:limit], "\n") + fmt.Sprintf("\n[truncated at %d matches — raise `limit` to see more]", limit)
				}
			}
			if len(text) > MaxFileBytes {
				text = text[:MaxFileBytes] + "\n… results truncated …"
			}
			if strings.TrimSpace(text) == "" {
				_ = err
				return "no matches.", nil
			}
			return text, nil
		},
	})
}

func terminalCommandTimeout(args map[string]any) time.Duration {
	seconds := intArg(args, "timeout_seconds", 0)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// boolArg extracts an optional boolean tool argument.
func boolArg(args map[string]any, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// tailTruncate keeps the last bashOutputMaxLines lines and bashOutputMaxBytes
// bytes of the stream. When it truncates it writes the full output to a temp
// file under $TMPDIR/lilith-exec-logs/ and returns a note pointing to it so
// the model can read it with read_files if it needs the earlier part.
func tailTruncate(s, stream string) (string, string) {
	lines := strings.Split(s, "\n")
	totalLines := len(lines)
	truncated := false
	if totalLines > bashOutputMaxLines {
		lines = lines[totalLines-bashOutputMaxLines:]
		truncated = true
	}
	body := strings.Join(lines, "\n")
	if len(body) > bashOutputMaxBytes {
		body = body[len(body)-bashOutputMaxBytes:]
		truncated = true
	}
	if !truncated {
		return body, ""
	}
	path, err := writeFullLog(s, stream)
	if err != nil {
		return body, fmt.Sprintf("[truncated tail; full %s unavailable: %v]", stream, err)
	}
	return body, fmt.Sprintf("[showing tail; full %s (%d lines, %d bytes) at %s]", stream, totalLines, len(s), path)
}

func writeFullLog(content, stream string) (string, error) {
	dir := filepath.Join(os.TempDir(), "lilith-exec-logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s-%s.log", time.Now().Format("20060102-150405"), stream, hex.EncodeToString(buf[:]))
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return full, nil
}
