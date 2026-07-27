//go:build windows

package shell

import (
	"os/exec"
	"strconv"

	"syscall"
)

// On Windows we ask the kernel to put the shell in a new process group so
// GenerateConsoleCtrlEvent can address it — but the reliable "nuke every
// descendant" primitive is `taskkill /T /F`, which walks the process tree.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x00000200 // CREATE_NEW_PROCESS_GROUP
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// /T = kill child tree, /F = force. Non-zero exit is fine; we still fall
	// back to killing the direct process.
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	return cmd.Process.Kill()
}