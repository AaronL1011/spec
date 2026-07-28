//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

// On POSIX systems the harness runs in its own process group and cancellation
// signals the whole group.
//
// This matters because exec.CommandContext kills only the direct child, and both
// shipping harnesses spawn descendants (node, language servers, model runners).
// Without the group, Esc-cancels-generation would orphan work that keeps running
// and burning tokens after the user thought they stopped it.

// setProcessGroup puts the command in a new process group.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killGroup signals the command's entire process group.
//
// SIGKILL rather than SIGTERM: this path runs when the user has already
// cancelled, so a harness that ignores or slow-walks a graceful signal would
// defeat the affordance. Falls back to the direct child if the group is gone.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative PID addresses the group led by that PID.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
