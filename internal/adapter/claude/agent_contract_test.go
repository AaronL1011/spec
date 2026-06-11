package claude

import (
	"context"
	"os"
	"path/filepath"
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

// TestInstallMCPConfig_WritesAndRestores asserts the .mcp.json install writes
// the engine config and the returned restore removes a file that did not exist
// before.
func TestInstallMCPConfig_WritesAndRestores(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(src, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	restore, err := installMCPConfig(src, work)
	if err != nil {
		t.Fatalf("installMCPConfig: %v", err)
	}
	dest := filepath.Join(work, ".mcp.json")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf(".mcp.json not written: %v", err)
	}

	restore()
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("restore should remove the created .mcp.json, stat err = %v", err)
	}
}

// TestInstallMCPConfig_RestoresPriorFile asserts restore puts back a
// pre-existing .mcp.json byte-for-byte.
func TestInstallMCPConfig_RestoresPriorFile(t *testing.T) {
	work := t.TempDir()
	dest := filepath.Join(work, ".mcp.json")
	prior := []byte(`{"mcpServers":{"old":true}}`)
	if err := os.WriteFile(dest, prior, 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(src, []byte(`{"mcpServers":{"new":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	restore, err := installMCPConfig(src, work)
	if err != nil {
		t.Fatalf("installMCPConfig: %v", err)
	}
	// During the session the new config is installed.
	cur, _ := os.ReadFile(dest)
	if string(cur) != `{"mcpServers":{"new":true}}` {
		t.Errorf("installed config = %s, want the new config", cur)
	}

	restore()
	after, _ := os.ReadFile(dest)
	if string(after) != string(prior) {
		t.Errorf("restored config = %s, want the prior config %s", after, prior)
	}
}

// TestInstallMCPConfig_MissingSource_Errors asserts a missing source config is
// surfaced as an error, not a panic.
func TestInstallMCPConfig_MissingSource_Errors(t *testing.T) {
	_, err := installMCPConfig(filepath.Join(t.TempDir(), "nope.json"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a missing source config")
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
