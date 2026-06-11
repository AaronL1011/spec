package claude

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// stubClaudeOnPath writes a no-op executable named "claude" into a temp dir and
// prepends it to PATH, so Invoke can spawn a real (trivial) subprocess without
// the actual Claude CLI installed.
func stubClaudeOnPath(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stub not portable to Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestInvoke_SpawnsAndSucceeds drives the full Invoke path with a stub binary,
// including system-prompt + prompt argument assembly.
func TestInvoke_SpawnsAndSucceeds(t *testing.T) {
	stubClaudeOnPath(t, "exit 0")
	agent := NewAgent("")

	res, err := agent.Invoke(context.Background(), adapter.InvokeRequest{
		SystemPrompt: "be terse",
		Prompt:       "do the thing",
		WorkDir:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res == nil {
		t.Error("Invoke result = nil, want a non-nil result")
	}
}

// TestInvoke_InstallsMCPConfig drives the branch that installs .mcp.json before
// spawning, then asserts it is restored (removed) afterwards.
func TestInvoke_InstallsMCPConfig(t *testing.T) {
	stubClaudeOnPath(t, "exit 0")
	agent := NewAgent("")

	work := t.TempDir()
	mcpSrc := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcpSrc, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := agent.Invoke(context.Background(), adapter.InvokeRequest{
		Prompt:        "go",
		WorkDir:       work,
		MCPConfigPath: mcpSrc,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// The installed .mcp.json is restored (removed) when Invoke returns.
	if _, err := os.Stat(filepath.Join(work, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf(".mcp.json should be cleaned up after Invoke, stat err = %v", err)
	}
}

// TestInvoke_SigintExit_IsClean asserts a 130 (Ctrl-C) exit is treated as a
// clean user quit, not an error.
func TestInvoke_SigintExit_IsClean(t *testing.T) {
	stubClaudeOnPath(t, "exit 130")
	agent := NewAgent("")

	res, err := agent.Invoke(context.Background(), adapter.InvokeRequest{Prompt: "x", WorkDir: t.TempDir()})
	if err != nil {
		t.Errorf("Invoke with exit 130 = %v, want nil (clean Ctrl-C)", err)
	}
	if res == nil {
		t.Error("Invoke result = nil, want non-nil on clean exit")
	}
}

// TestInvoke_NonZeroExit_ReturnsError asserts a genuine non-zero exit surfaces
// as an error.
func TestInvoke_NonZeroExit_ReturnsError(t *testing.T) {
	stubClaudeOnPath(t, "exit 1")
	agent := NewAgent("")

	_, err := agent.Invoke(context.Background(), adapter.InvokeRequest{Prompt: "x", WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error on non-zero claude exit")
	}
}
