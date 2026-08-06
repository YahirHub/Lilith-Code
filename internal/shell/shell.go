// Package shell runs commands with the host-appropriate interpreter. Windows
// prefers PowerShell for neutral commands, keeps CMD available, and selects
// Bash only for POSIX syntax; Linux, macOS and Termux prefer Bash/sh. A pure-Go
// interpreter remains available as the last POSIX-compatible fallback.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// MaxOutputBytes caps each stream so a runaway command cannot exhaust memory.
const MaxOutputBytes = 256 << 10

// ErrNoShell means no compatible command interpreter is available.
var ErrNoShell = errors.New("no compatible shell is available on this host")

// Request describes one command execution.
type Request struct {
	Command string
	// Dir is the working directory; empty means the current process directory.
	Dir string
	// Timeout applies only when positive. Zero or a negative value means no
	// deadline; the command still stops when the parent context is canceled.
	Timeout time.Duration
	// Shell selects auto, bash, sh, powershell, cmd or portable. Empty means auto.
	Shell string
}

// Result is the structured outcome of a command.
type Result struct {
	Command    string        `json:"command"`
	Shell      string        `json:"shell"`
	ShellKind  string        `json:"shellKind"`
	Dir        string        `json:"startingCwd"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	ExitCode   int           `json:"exitCode"`
	TimedOut   bool          `json:"timedOut"`
	Canceled   bool          `json:"canceled"`
	Duration   time.Duration `json:"-"`
	DurationMs int64         `json:"durationMs"`
	Truncated  bool          `json:"truncated"`
}

var nullRedirectPattern = regexp.MustCompile(`(?i)((?:&>>|&>|[0-9]*>>?)\s*)(?:'null'|"null"|null|/dev/null|nul|\$null)([ \t\r\n;|&)]|$)`)

const powershellUTF8Prelude = `[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); $OutputEncoding = [Console]::OutputEncoding; `

// normalizeNullRedirects prevents a common cross-platform model mistake:
// redirecting output to a literal file named "null" or using another shell's
// null device. The replacement follows the interpreter actually selected.
func normalizeNullRedirects(command, shellKind string) string {
	target := "/dev/null"
	switch shellKind {
	case ShellPowerShell:
		target = "$null"
	case ShellCmd:
		target = "NUL"
	}
	replacement := `${1}` + strings.ReplaceAll(target, "$", "$$") + `${2}`
	for {
		next := nullRedirectPattern.ReplaceAllString(command, replacement)
		if next == command {
			return command
		}
		command = next
	}
}

// Run executes the command with the resolved shell and returns its result.
// A non-zero exit code is not a Go error: it is reported in Result.ExitCode.
func Run(ctx context.Context, req Request) (Result, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return Result{}, errors.New("comando vacío")
	}
	if err := validateCommandSafety(command); err != nil {
		return Result{}, err
	}
	spec, err := resolveExecutionShell(req.Shell, command)
	if err != nil {
		return Result{}, err
	}
	command = normalizeNullRedirects(command, spec.Kind)

	dir := req.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Result{}, err
		}
		dir = wd
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("directorio de trabajo inválido: %s", dir)
	}

	runCtx, cancel := withOptionalTimeout(ctx, req.Timeout)
	defer cancel()

	if spec.Kind == ShellPortable {
		start := time.Now()
		stdout, stderr, exitCode, runErr := runPortable(runCtx, command, dir)
		elapsed := time.Since(start)
		res := Result{
			Command:    command,
			Shell:      spec.Path,
			ShellKind:  spec.Kind,
			Dir:        dir,
			ExitCode:   exitCode,
			Duration:   elapsed,
			DurationMs: elapsed.Milliseconds(),
		}
		res.Stdout, res.Truncated = clip(stdout)
		var truncErr bool
		res.Stderr, truncErr = clip(stderr)
		res.Truncated = res.Truncated || truncErr
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			res.TimedOut = true
			res.ExitCode = -1
			return res, nil
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			res.Canceled = true
			res.ExitCode = -1
			return res, nil
		}
		if runErr != nil {
			return res, runErr
		}
		return res, nil
	}

	executionCommand := commandForShell(command, spec.Kind)
	args := append(append([]string{}, spec.Prefix...), executionCommand)
	cmd := exec.CommandContext(runCtx, spec.Path, args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	// Run the shell in its own process group / job so a cancel (from
	// the TUI, or a timeout) tears down every descendant it spawned. Without
	// this, exec.CommandContext only kills the immediate shell PID and leaves
	// long-running children (npm, tsc, docker, …) orphaned in the background.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	// Interactive cancellation is a hard stop. After killing the process tree, do not let stale
	// inherited pipes keep the command goroutine around for seconds.
	cmd.WaitDelay = 100 * time.Millisecond

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)

	res := Result{
		Command:    command,
		Shell:      spec.Path,
		ShellKind:  spec.Kind,
		Dir:        dir,
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
	}
	res.Stdout, res.Truncated = clip(stdout.Bytes())
	var truncErr bool
	res.Stderr, truncErr = clip(stderr.Bytes())
	res.Truncated = res.Truncated || truncErr

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res, nil
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		res.Canceled = true
		res.ExitCode = -1
		return res, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

func commandForShell(command, shellKind string) string {
	if shellKind == ShellPowerShell {
		// Windows PowerShell 5.1 inherits the legacy console code page when its
		// standard streams are redirected. Force BOM-less UTF-8 before running
		// the user's command so Go can decode stdout/stderr without losing
		// accents or supplementary Unicode characters. Keep the user's command
		// last: appending cleanup statements would overwrite PowerShell's native
		// exit-code semantics.
		return powershellUTF8Prelude + command
	}
	return command
}

func withOptionalTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func clip(b []byte) (string, bool) {
	if len(b) <= MaxOutputBytes {
		return string(b), false
	}
	return string(b[:MaxOutputBytes]) + "\n… salida truncada …", true
}
