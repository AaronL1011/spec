package pi

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

func TestDefaultCommand(t *testing.T) {
	if got := NewAgent("").Command; got != "pi" {
		t.Errorf("default command = %q, want pi", got)
	}
	if got := NewAgent("pi-dev").Command; got != "pi-dev" {
		t.Errorf("custom command = %q, want pi-dev", got)
	}
}

func TestCapabilities(t *testing.T) {
	caps := NewAgent("").Capabilities()
	if !caps.MCP || !caps.Headless || !caps.Skills || !caps.SystemPrompt {
		t.Errorf("pi should support all capabilities, got %+v", caps)
	}
}

// MCP config is installed as .mcp.json by Invoke, not passed on the command
// line: pi has no --mcp-config flag, so args must never carry one.
func TestArgsBuildsFlags(t *testing.T) {
	a := NewAgent("")
	got := a.args(adapter.InvokeRequest{
		SpecID:        "SPEC-042",
		MCPConfigPath: "/tmp/mcp.json",
		SystemPrompt:  "do the thing",
		SkillPaths:    []string{"/skills/build", "/skills/fix"},
	})
	joined := strings.Join(got, " ")

	for _, want := range []string{
		"--skill /skills/build",
		"--skill /skills/fix",
		"--append-system-prompt do the thing",
		"--session-id spec-SPEC-042",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "--mcp-config") {
		t.Errorf("args must not carry --mcp-config; pi discovers MCP via .mcp.json: %q", joined)
	}
}

func TestArgsOmitsEmptyFlags(t *testing.T) {
	got := NewAgent("").args(adapter.InvokeRequest{})
	if len(got) != 0 {
		t.Errorf("expected no flags for empty request, got %v", got)
	}
}
