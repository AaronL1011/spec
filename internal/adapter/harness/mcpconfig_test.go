package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// .mcp.json install is the mechanism both harness adapters (claude, pi) share
// for giving a session the spec MCP server. A bug that restores the wrong file
// or leaves a stale one would break both, so these live with the shared helper
// rather than in either adapter's package.

// TestInstallMCPConfig_WritesAndRestores asserts the install writes the engine
// config and the returned restore removes a file that did not exist before.
func TestInstallMCPConfig_WritesAndRestores(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(src, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	restore, err := InstallMCPConfig(src, work)
	if err != nil {
		t.Fatalf("InstallMCPConfig: %v", err)
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

	restore, err := InstallMCPConfig(src, work)
	if err != nil {
		t.Fatalf("InstallMCPConfig: %v", err)
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
	_, err := InstallMCPConfig(filepath.Join(t.TempDir(), "nope.json"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a missing source config")
	}
}
