package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lilith/li/internal/shell"
	"github.com/lilith/li/internal/textsearch"
	"github.com/lilith/li/internal/toolchain"
)

// bashOutputMaxLines / bashOutputMaxBytes bound the inline snippet we return
// to the model. Anything longer is tail-truncated and the full stream is saved
// to a temp file so the model can inspect it with read_files if needed. This
// mirrors pi.dev's bash tool behaviour.
const (
	bashOutputMaxLines      = 200
	bashOutputMaxBytes      = 32 << 10
	repositorySearchTimeout = 30 * time.Second
)

func init() {
	register(Definition{
		Name: "run_terminal_command",
		Description: fmt.Sprintf(
			"Run a command in the project directory with automatic shell selection and return stdout, "+
				"stderr and the exit code. Native host shells keep priority: Windows uses PowerShell/CMD syntax or an installed POSIX shell, while Linux/macOS/Termux prefer Bash/sh. When a POSIX shell is unavailable, Lilith can use an embedded pure-Go interpreter for a bounded Bash/POSIX-compatible subset (`shell=portable`) with a curated Go toolbox (`rg`, `grep`, `find`, `ls`, `cat`, `head`, `tail`, `wc`, `mkdir`, `touch`, `cp`, `mv`, `rm`, `chmod`, `sha256sum`). It does not emulate full Bash and does not emulate a full Linux userland: unsupported syntax fails explicitly, and commands such as git, gh, go, npm, docker or make still require their executable in PATH. Heredocs and oversized inline file-writing commands are rejected before execution; use write_file/append_file for generated content. `timeout_seconds` is optional: when omitted, builds/tests/installations run until completion or cancellation, while repository searches receive a 30-second safety deadline. Recursive grep without an explicit target is rejected before execution so the model can use code_search or provide a concrete path. Output is tail-truncated to the last %d lines / %dKB per "+
				"stream; when truncated the full stream is saved to a temp file whose path is reported so you can inspect it with read_files.",
			bashOutputMaxLines, bashOutputMaxBytes/1024,
		),
		PromptSnippet: "Execute shell commands in the project directory",
		PromptGuidelines: []string{
			"Use code_search for repository text/source lookups; it has a pure-Go fallback and does not require ripgrep. Use run_terminal_command for builds, tests, git and shell inspection.",
			"Omit timeout_seconds for long builds, installs and test suites unless a hard deadline is explicitly needed. Repository search commands receive a 30-second safety deadline when omitted.",
			"Keep commands native to the detected host. shell=auto keeps PowerShell/CMD/Bash/sh ahead of the embedded portable interpreter. Use shell=portable only for its documented Bash/POSIX-compatible subset when no POSIX shell exists or when deterministic pure-Go execution is intentional.",
			"The portable shell is bounded: heredocs, job control, process substitution, arrays and unsupported Bash extensions fail explicitly. It provides a curated file/search toolbox, not Git or a full Linux environment. Never assume git, gh, go, npm, docker, make or another external executable exists; inspect availability or use a dedicated Lilith tool.",
			"Do not generate long files with heredocs, printf, PowerShell here-strings or base64 in the terminal. Use write_file for complete content or append_file for bounded sections; unsafe inline writes are blocked before execution.",
			"When discarding output, use the selected shell's null device (/dev/null for Bash/sh, $null for PowerShell, NUL for CMD). Lilith normalizes common cross-shell mistakes defensively.",
		},
		Mutating: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string", "description": "Full command line to run using the selected shell syntax."},
				"shell":           map[string]any{"type": "string", "enum": []string{"auto", "bash", "sh", "powershell", "cmd", "portable"}, "description": "Interpreter to use. auto prefers a compatible native shell and falls back to Lilith's embedded pure-Go interpreter for a bounded POSIX/Bash-compatible subset. portable selects that interpreter explicitly."},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "description": "Optional hard deadline in seconds. When omitted, long-running builds/tests/installations are unlimited, while repository search commands use a 30-second safety deadline."},
			},
			"required": []string{"command"},
		},
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			timeout := terminalCommandTimeout(args)
			res, err := shell.Run(ctx, shell.Request{
				Command: str(args, "command"),
				Dir:     env.Root,
				Timeout: timeout,
				Shell:   str(args, "shell"),
			})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "shell: %s (%s)\n", res.ShellKind, res.Shell)
			fmt.Fprintf(&b, "exit_code: %d\n", res.ExitCode)
			if res.TimedOut {
				b.WriteString("timeout: yes\n")
				if shell.IsRepositorySearchCommand(str(args, "command")) {
					b.WriteString("hint: narrow the path/glob or use code_search for repository content\n")
				}
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
		Description: "Search file contents for a pattern. Lilith uses native ripgrep when available and transparently falls back to a bounded pure-Go search engine when it is absent. Returns matches as `path:line:text`. " +
			"Options: `glob` filters files (e.g. `*.go`), `path` scopes the search directory (default: project root), " +
			"`literal` treats `pattern` as a fixed string instead of a regex, `ignore_case` enables case-insensitive matching, " +
			"`context` shows N lines before/after each match, and `limit` caps total matches (default 100). " +
			"The Go fallback respects repository ignore files, skips hidden/VCS/dependency/build paths plus binary and oversized files by default, and still searches an explicitly requested ignored file. Long output is truncated; long lines are individually truncated to 500 chars.",
		PromptSnippet: "Search repository contents with ripgrep or Lilith's native Go fallback",
		PromptGuidelines: []string{
			"Prefer code_search over terminal grep/rg for repository content. It works even when ripgrep is not installed.",
			"Scope broad searches with path or glob and raise limit only when the additional matches are necessary.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "RE2-compatible regex (default) or literal string when `literal` is true."},
				"glob":        map[string]any{"type": "string", "description": "Optional file filter, e.g. `*.go` or `**/*_test.go`."},
				"path":        map[string]any{"type": "string", "description": "Directory or file to search (default: project root)."},
				"literal":     map[string]any{"type": "boolean", "description": "Treat `pattern` as a fixed string instead of a regex (default false)."},
				"ignore_case": map[string]any{"type": "boolean", "description": "Case-insensitive match (default false)."},
				"context":     map[string]any{"type": "integer", "minimum": 0, "description": "Lines of context to show before/after each match (default 0)."},
				"limit":       map[string]any{"type": "integer", "minimum": 1, "description": "Maximum matches (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Run: func(ctx context.Context, args map[string]any, env Env) (string, error) {
			pattern := str(args, "pattern")
			if strings.TrimSpace(pattern) == "" {
				return "", fmt.Errorf("empty pattern")
			}
			runCtx, cancel := context.WithTimeout(ctx, repositorySearchTimeout)
			defer cancel()
			if rg := toolchain.Lookup("rg"); rg != "" {
				return runRipgrepSearch(runCtx, rg, args, env)
			}
			return runNativeCodeSearch(runCtx, args, env)
		},
	})
}

func runRipgrepSearch(ctx context.Context, rg string, args map[string]any, env Env) (string, error) {
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
	cmdArgs = append(cmdArgs, str(args, "pattern"), target)
	cmd := exec.CommandContext(ctx, rg, cmdArgs...)
	cmd.Dir = env.Root
	out, err := cmd.CombinedOutput()
	text := string(out)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			if message := strings.TrimSpace(text); message != "" {
				return "", fmt.Errorf("ripgrep: %s", message)
			}
			return "", err
		}
	}
	if limit > 0 {
		if lines := strings.Split(text, "\n"); len(lines) > limit {
			text = strings.Join(lines[:limit], "\n") + fmt.Sprintf("\n[truncated at %d matches — raise `limit` to see more]", limit)
		}
	}
	if len(text) > MaxFileBytes {
		text = text[:MaxFileBytes] + "\n… results truncated …"
	}
	if strings.TrimSpace(text) == "" {
		return "no matches.", nil
	}
	return text, nil
}

func runNativeCodeSearch(ctx context.Context, args map[string]any, env Env) (string, error) {
	res, err := textsearch.Search(ctx, textsearch.Options{
		Root:       env.Root,
		Path:       str(args, "path"),
		Pattern:    str(args, "pattern"),
		Glob:       str(args, "glob"),
		Literal:    boolArg(args, "literal"),
		IgnoreCase: boolArg(args, "ignore_case"),
		Context:    intArg(args, "context", 0),
		Limit:      intArg(args, "limit", 100),
		MaxLine:    500,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(res.Text) == "" {
		return "no matches.", nil
	}
	text := res.Text
	if res.Truncated {
		text += fmt.Sprintf("\n[truncated at %d matches — raise `limit` to see more]", intArg(args, "limit", 100))
	}
	if len(text) > MaxFileBytes {
		text = text[:MaxFileBytes] + "\n… results truncated …"
	}
	return text, nil
}

func terminalCommandTimeout(args map[string]any) time.Duration {
	seconds := intArg(args, "timeout_seconds", 0)
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if shell.IsRepositorySearchCommand(str(args, "command")) {
		return repositorySearchTimeout
	}
	return 0
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
