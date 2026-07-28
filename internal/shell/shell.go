// Package shell runs shell commands portably: bash where it exists and the
// managed busybox shell on Windows, always with a timeout and bounded output.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lilith/li/internal/toolchain"
)

// DefaultTimeout applies when Request.Timeout is zero.
const DefaultTimeout = 30 * time.Second

// MaxOutputBytes caps each stream so a runaway command cannot exhaust memory.
const MaxOutputBytes = 256 << 10

// ErrNoShell means no POSIX shell is available on this machine.
var ErrNoShell = errors.New("no hay shell POSIX disponible: ejecuta `go run ./cmd/build install` para instalar busybox")

// Request describes one command execution.
type Request struct {
	Command string
	// Dir is the working directory; empty means the current process directory.
	Dir string
	// Timeout zero means DefaultTimeout; negative means no timeout.
	Timeout time.Duration
}

// Result is the structured outcome of a command.
type Result struct {
	Command    string        `json:"command"`
	Shell      string        `json:"shell"`
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

// Run executes the command with the resolved shell and returns its result.
// A non-zero exit code is not a Go error: it is reported in Result.ExitCode.
func Run(ctx context.Context, req Request) (Result, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return Result{}, errors.New("comando vacío")
	}
	shellPath, prefix, ok := toolchain.ShellCommand()
	if !ok {
		return Result{}, ErrNoShell
	}

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

	timeout := req.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	args := append(append([]string{}, prefix...), command)
	cmd := exec.CommandContext(runCtx, shellPath, args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	// Run the shell in its own process group / job so a cancel (Ctrl+C from
	// the TUI, or a timeout) tears down every descendant it spawned. Without
	// this, exec.CommandContext only kills the immediate shell PID and leaves
	// long-running children (npm, tsc, docker, …) orphaned in the background.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	// Ctrl+C is a hard stop. After killing the process tree, do not let stale
	// inherited pipes keep the command goroutine around for seconds.
	cmd.WaitDelay = 150 * time.Millisecond

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	res := Result{
		Command:    command,
		Shell:      shellPath,
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

func clip(b []byte) (string, bool) {
	if len(b) <= MaxOutputBytes {
		return string(b), false
	}
	return string(b[:MaxOutputBytes]) + "\n… salida truncada …", true
}
