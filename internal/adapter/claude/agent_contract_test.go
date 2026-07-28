package claude

import (
	"context"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// TestInvoke_MissingBinary_ReturnsActionableError asserts the failure-isolation
// contract: an absent CLI yields an actionable error, never a panic.
func TestInvoke_MissingBinary_ReturnsActionableError(t *testing.T) {
	agent := NewAgent("definitely-not-a-real-binary-xyz")
	_, err := agent.Invoke(context.Background(), adapter.InvokeRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error when the CLI binary is missing")
	}
	if got := err.Error(); !contains(got, "not found in PATH") || !contains(got, "anthropic.com") {
		t.Errorf("error = %q, want it to name the missing binary and an install hint", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
