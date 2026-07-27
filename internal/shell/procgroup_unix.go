//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup makes the child shell the leader of a new process
// group so we can signal every descendant with a single kill(-pgid).
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the entire group. We use SIGKILL rather
// than SIGTERM because Ctrl+C in the TUI is already a "stop everything now"
// signal — a polite SIGTERM would leave stubborn children (docker run, tsc
// --watch, pnpm dev) hanging around, which is exactly what we want to fix.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fallback: kill just the direct child. Better than nothing.
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}