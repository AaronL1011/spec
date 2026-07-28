package claude

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Containment is a precondition for advertising the completion plane, so it is
// asserted at the argv level: if these flags are ever dropped, a "one-shot
// completion" could edit files and run commands.

func TestGenerateArgs_CarriesContainmentFlags(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{Prompt: "draft it"}, "")
	joined := strings.Join(args, " ")

	// --tools "" disables the built-in tool set. The empty value is significant,
	// so the pair is checked positionally rather than by substring.
	if !hasFlagValue(args, "--tools", "") {
		t.Errorf("missing `--tools \"\"`: built-in tools would remain enabled\nargv: %v", args)
	}
	for _, flag := range []string{"--strict-mcp-config", "--no-session-persistence"} {
		if !hasFlag(args, flag) {
			t.Errorf("missing %s\nargv: %v", flag, args)
		}
	}
	// Headless JSON mode: without -p the harness would try to be interactive.
	if !hasFlag(args, "-p") {
		t.Errorf("missing -p (non-interactive)\nargv: %v", args)
	}
	if !hasFlagValue(args, "--output-format", "json") {
		t.Errorf("missing --output-format json\nargv: %v", args)
	}
	// No MCP config may be passed: --strict-mcp-config with no servers is what
	// keeps the model away from spec's own authoring port.
	if strings.Contains(joined, "--mcp-config") {
		t.Errorf("a contained completion must not be given MCP servers\nargv: %v", args)
	}
}

// --bare would force ANTHROPIC_API_KEY and never read OAuth or the keychain,
// silently breaking single-auth for subscription users — the exact benefit that
// serving completions through the harness exists to provide.
func TestGenerateArgs_DoesNotUseBareMode(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{Prompt: "x"}, "")
	if hasFlag(args, "--bare") {
		t.Error("--bare forces API-key auth and bypasses OAuth; containment must come from narrow flags instead")
	}
}

func TestGenerateArgs_PassesModelAndSystemPrompt(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{
		Prompt: "draft it",
		System: "you are a technical writer",
	}, "claude-sonnet-4-5")

	if !hasFlagValue(args, "--model", "claude-sonnet-4-5") {
		t.Errorf("model not passed through\nargv: %v", args)
	}
	if !hasFlagValue(args, "--append-system-prompt", "you are a technical writer") {
		t.Errorf("system prompt not passed through\nargv: %v", args)
	}
	// The prompt is the final positional argument.
	if args[len(args)-1] != "draft it" {
		t.Errorf("prompt should be the last argument, got %q", args[len(args)-1])
	}
}

// Claude Code enforces a schema natively, so a JSON request is honoured rather
// than degraded to schema-in-prompt.
func TestGenerateArgs_UsesNativeJSONSchema(t *testing.T) {
	schema := `{"type":"object"}`
	args := generateArgs(adapter.GenerateRequest{
		Prompt: "x",
		Format: adapter.FormatJSON,
		Schema: []byte(schema),
	}, "")
	if !hasFlagValue(args, "--json-schema", schema) {
		t.Errorf("expected native --json-schema\nargv: %v", args)
	}
}

func TestGenerateArgs_RendersLabelledContext(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{
		Prompt:  "draft it",
		Context: []adapter.ContextPart{{Label: "Problem Statement", Content: "things break"}},
	}, "")
	prompt := args[len(args)-1]
	if !strings.Contains(prompt, "## Problem Statement") || !strings.Contains(prompt, "things break") {
		t.Errorf("labelled context missing from prompt:\n%s", prompt)
	}
}

// Capabilities.Generate must only be true where containment is proven; this
// pairs the claim with the argv assertions above.
func TestCapabilities_AdvertisesContainedCompletions(t *testing.T) {
	caps := NewAgent("").Capabilities()
	if !caps.Generate {
		t.Error("Generate should be advertised once containment is enforced")
	}
	if !caps.StructuredOutput {
		t.Error("StructuredOutput should be true: --json-schema is enforced natively")
	}
	if !caps.MCP || !caps.SystemPrompt {
		t.Errorf("session-plane capabilities regressed: %+v", caps)
	}
}

func TestParseGenerateOutput(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		wantText  string
		wantModel string
		wantRaw   bool
	}{
		{
			name:      "well-formed result",
			out:       `{"result":"Drafted body.","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":4}}`,
			wantText:  "Drafted body.",
			wantModel: "claude-sonnet-4-5",
		},
		{
			// Format drift must degrade to an empty result with a debuggable
			// tail, never an error: a cosmetic upstream change should cost a
			// draft, not break the feature.
			name:    "unparseable output degrades",
			out:     `this is not json`,
			wantRaw: true,
		},
		{
			name:    "error envelope yields no text",
			out:     `{"result":"","is_error":true}`,
			wantRaw: true,
		},
		{
			name: "empty output",
			out:  "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := parseGenerateOutput(tt.out)
			if res.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", res.Text, tt.wantText)
			}
			if tt.wantModel != "" && res.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", res.Model, tt.wantModel)
			}
			if tt.wantRaw && res.Raw == "" {
				t.Error("expected a bounded Raw tail for debugging")
			}
		})
	}
}

func TestParseGenerateOutput_SumsTokens(t *testing.T) {
	res := parseGenerateOutput(`{"result":"x","usage":{"input_tokens":10,"output_tokens":4}}`)
	if res.Tokens.Total != 14 {
		t.Errorf("Tokens.Total = %d, want 14", res.Tokens.Total)
	}
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func hasFlagValue(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}
