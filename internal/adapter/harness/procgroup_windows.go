//go:build windows

package harness

import "os/exec"

// Windows has no POSIX process groups, and the release builds windows/amd64 and
// windows/arm64, so the guarantee is stated honestly rather than silently
// failing to compile or pretending to hold.
//
// Descendant termination here is best-effort: killing the direct child is what
// the standard library offers without pulling in job-object plumbing, which
// would be a disproportionate amount of platform-specific code for a plane whose
// containment already rests on tool-disable flags and an empty working
// directory. The strong no-orphans guarantee applies to the POSIX platforms
// where `d`-key drafting is actually used.

// setProcessGroup is a no-op on Windows.
func setProcessGroup(cmd *exec.Cmd) {}

// killGroup kills the direct child. Descendants may survive; see the package
// note above for why that is accepted rather than worked around.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
