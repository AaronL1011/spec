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
		"--mcp-config /tmp/mcp.json",
		"--skill /skills/build",
		"--skill /skills/fix",
		"--append-system-prompt do the thing",
		"--session-id spec-SPEC-042",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestArgsOmitsEmptyFlags(t *testing.T) {
	got := NewAgent("").args(adapter.InvokeRequest{})
	if len(got) != 0 {
		t.Errorf("expected no flags for empty request, got %v", got)
	}
}

func TestScanForStepComplete(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   bool
	}{
		{
			name:   "success event",
			stream: `{"type":"tool_execution_start","toolName":"spec_step_complete"}` + "\n" + `{"type":"tool_execution_end","toolName":"spec_step_complete","isError":false}`,
			want:   true,
		},
		{
			name:   "error event ignored",
			stream: `{"type":"tool_execution_end","toolName":"spec_step_complete","isError":true}`,
			want:   false,
		},
		{
			name:   "other tool ignored",
			stream: `{"type":"tool_execution_end","toolName":"spec_decide","isError":false}`,
			want:   false,
		},
		{
			name:   "malformed lines skipped",
			stream: "not json\n{partial" + "\n" + `{"type":"tool_execution_end","toolName":"spec_step_complete","isError":false}`,
			want:   true,
		},
		{
			name:   "no events",
			stream: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanForStepComplete(strings.NewReader(tt.stream)); got != tt.want {
				t.Errorf("scanForStepComplete = %v, want %v", got, tt.want)
			}
		})
	}
}
