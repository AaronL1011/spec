package claude

import "testing"

func TestCapabilities(t *testing.T) {
	agent := NewAgent("")
	caps := agent.Capabilities()
	if !caps.MCP {
		t.Error("Claude Code should support MCP")
	}
	if !caps.SystemPrompt {
		t.Error("Claude Code should support an appended system prompt")
	}
}

func TestDefaultCommand(t *testing.T) {
	agent := NewAgent("")
	if agent.Command != "claude" {
		t.Errorf("expected default command 'claude', got %q", agent.Command)
	}
}

func TestCustomCommand(t *testing.T) {
	agent := NewAgent("claude-dev")
	if agent.Command != "claude-dev" {
		t.Errorf("expected custom command 'claude-dev', got %q", agent.Command)
	}
}
