package agentcheck

import (
	"fmt"
	"os/exec"
	"strings"
)

// KnownHarnesses are the coding agents spec can detect on PATH, with the binary
// each provider expects.
var KnownHarnesses = []struct {
	Provider string
	Command  string
}{
	{Provider: "claude-code", Command: "claude"},
	{Provider: "pi", Command: "pi"},
}

// Detected returns the providers whose binary is on PATH, so setup can offer
// what is installed first and make the common case a single keystroke.
func Detected() []string {
	var found []string
	for _, h := range KnownHarnesses {
		if _, err := exec.LookPath(h.Command); err == nil {
			found = append(found, h.Provider)
		}
	}
	return found
}

// DetectedHint suggests installed harnesses in an error message.
func DetectedHint() string {
	found := Detected()
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf(" (detected on PATH: %s)", strings.Join(found, ", "))
}
