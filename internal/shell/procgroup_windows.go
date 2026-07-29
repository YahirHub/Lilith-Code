//go:build windows

package shell

import (
	"os/exec"
	"strconv"
	"syscall"
)

// On Windows we place the shell in a new process group. The actual hard-stop
// primitive is taskkill /T /F because commands such as npm/npx/Electron spawn
// several descendants that outlive the immediate shell if only its PID dies.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x00000200 // CREATE_NEW_PROCESS_GROUP
}

// killProcessGroup mirrors pi.dev's Windows strategy: launch taskkill for the
// entire tree and return immediately instead of waiting for taskkill itself.
// Waiting here used to make context cancellation depend on how quickly Windows
// could reap Electron/Node descendants, so interactive cancellation could feel delayed and the
// tool goroutine could remain alive until the GUI was closed manually.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	killer := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killer.Start(); err != nil {
		// taskkill could not even be launched; kill the direct process as a
		// fallback. This may not catch every descendant, but guarantees the
		// waiting shell itself is interrupted.
		return cmd.Process.Kill()
	}
	// Reap taskkill asynchronously. The cancellation path must never block the
	// Bubble Tea update loop or os/exec's context watcher.
	go func() { _ = killer.Wait() }()
	return nil
}
