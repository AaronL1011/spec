package pi

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Containment is a precondition for advertising the completion plane, asserted
// here at the argv level.

func TestGenerateArgs_CarriesContainmentFlags(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{Prompt: "draft it"}, "")

	// --no-tools is the core guarantee; the discovery flags are equally
	// load-bearing because without them a completion's instructions depend on
	// whatever directory the process started in.
	required := []string{
		"--no-tools",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-context-files",
		"--no-session",
	}
	for _, flag := range required {
		if !hasFlag(args, flag) {
			t.Errorf("missing %s\nargv: %v", flag, args)
		}
	}
	if !hasFlag(args, "-p") {
		t.Errorf("missing -p (non-interactive)\nargv: %v", args)
	}
	if !hasFlagValue(args, "--mode", "json") {
		t.Errorf("missing --mode json\nargv: %v", args)
	}
	if strings.Contains(strings.Join(args, " "), "--mcp-config") {
		t.Errorf("a contained completion must not be given MCP servers\nargv: %v", args)
	}
}

// AGENTS.md / CLAUDE.md discovery would let an unrelated repo's house rules
// steer a spec draft, so this flag is containment rather than tidiness.
func TestGenerateArgs_DisablesContextFileDiscovery(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{Prompt: "x"}, "")
	if !hasFlag(args, "--no-context-files") {
		t.Error("--no-context-files is required: project instructions must not leak into a draft")
	}
}

func TestGenerateArgs_PassesModelVerbatim(t *testing.T) {
	// pi accepts "provider/id" and defaults to google, so the user's spelling is
	// the correct one; spec must not translate it.
	args := generateArgs(adapter.GenerateRequest{Prompt: "x"}, "anthropic/claude-sonnet-4-5")
	if !hasFlagValue(args, "--model", "anthropic/claude-sonnet-4-5") {
		t.Errorf("model should pass through verbatim\nargv: %v", args)
	}
}

// pi's --mode json is an output envelope, not schema-enforced generation, so a
// JSON request degrades to schema-in-prompt rather than claiming enforcement.
func TestGenerateArgs_JSONFallsBackToSchemaInPrompt(t *testing.T) {
	args := generateArgs(adapter.GenerateRequest{
		Prompt: "x",
		Format: adapter.FormatJSON,
		Schema: []byte(`{"type":"object"}`),
	}, "")
	if hasFlag(args, "--json-schema") {
		t.Error("pi has no native schema flag; it must not be passed one")
	}
	prompt := args[len(args)-1]
	if !strings.Contains(prompt, `{"type":"object"}`) {
		t.Errorf("schema should be embedded in the prompt as a fallback:\n%s", prompt)
	}
}

func TestCapabilities_AdvertisesContainedCompletions(t *testing.T) {
	caps := NewAgent("").Capabilities()
	if !caps.Generate {
		t.Error("Generate should be advertised once containment is enforced")
	}
	if caps.StructuredOutput {
		t.Error("StructuredOutput must be false: --mode json is an envelope, not schema enforcement")
	}
	if !caps.MCP || !caps.Headless || !caps.Skills || !caps.SystemPrompt {
		t.Errorf("session-plane capabilities regressed: %+v", caps)
	}
}

func TestParseGenerateStream(t *testing.T) {
	tests := []struct {
		name     string
		stream   string
		wantText string
		wantRaw  bool
	}{
		{
			name: "message_end with string content",
			stream: `{"type":"session","model":"claude-sonnet-4-5"}
{"type":"message_end","content":"Drafted body."}`,
			wantText: "Drafted body.",
		},
		{
			name:     "content blocks are concatenated",
			stream:   `{"type":"message_end","content":[{"type":"text","text":"Part one. "},{"type":"text","text":"Part two."}]}`,
			wantText: "Part one. Part two.",
		},
		{
			name:     "agent_end spelling is accepted",
			stream:   `{"type":"agent_end","message":{"content":"From agent_end."}}`,
			wantText: "From agent_end.",
		},
		{
			// Interleaved log output must be skipped, not fatal.
			name: "non-JSON lines are ignored",
			stream: `starting up...
{"type":"message_end","content":"Body."}
done`,
			wantText: "Body.",
		},
		{
			name:    "unparseable stream degrades with raw tail",
			stream:  "not json at all\nstill not json",
			wantRaw: true,
		},
		{
			name:    "empty stream",
			stream:  "",
			wantRaw: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := parseGenerateStream(tt.stream)
			if res.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", res.Text, tt.wantText)
			}
			if tt.wantRaw && res.Raw == "" {
				t.Error("expected a bounded Raw tail for debugging")
			}
		})
	}
}

func TestParseGenerateStream_CapturesModelAndUsage(t *testing.T) {
	stream := `{"type":"session","model":"claude-sonnet-4-5"}
{"type":"message_end","content":"Body.","usage":{"input":100,"output":25}}`
	res := parseGenerateStream(stream)
	if res.Model != "claude-sonnet-4-5" {
		t.Errorf("Model = %q", res.Model)
	}
	if res.Tokens.Input != 100 || res.Tokens.Output != 25 {
		t.Errorf("Tokens = %+v, want 100 in / 25 out", res.Tokens)
	}
}

// pi re-emits a cumulative usage snapshot on every event that touches a
// message, so summing them multiplies the real cost by the number of events.
// This is the shape a live `pi -p --mode json` run produces: four snapshots of
// one message, three of them carrying the same final figures.
func TestParseGenerateStream_CumulativeUsageIsNotSummed(t *testing.T) {
	stream := `{"type":"message_start","message":{"usage":{"input":0,"output":0,"totalTokens":0}}}
{"type":"message_update","message":{"usage":{"input":0,"output":0,"totalTokens":0}}}
{"type":"message_update","message":{"usage":{"input":2,"output":4,"cacheRead":644,"totalTokens":650}}}
{"type":"message_end","message":{"content":[{"type":"text","text":"OK"}],"usage":{"input":2,"output":4,"cacheRead":644,"totalTokens":650}}}
{"type":"turn_end","message":{"usage":{"input":2,"output":4,"cacheRead":644,"totalTokens":650}}}`

	res := parseGenerateStream(stream)
	if res.Text != "OK" {
		t.Fatalf("Text = %q, want %q", res.Text, "OK")
	}
	// 2/4/6, not 6/12/1950: the snapshot is a running total, not a delta, and
	// cacheRead is harness overhead rather than work done for this request.
	want := adapter.TokenUsage{Input: 2, Output: 4, Total: 6}
	if res.Tokens != want {
		t.Errorf("Tokens = %+v, want %+v", res.Tokens, want)
	}
}

// An early snapshot is emitted before the provider reports usage. It must not
// overwrite real figures with zeroes when it arrives last.
func TestParseGenerateStream_ZeroUsageDoesNotClobber(t *testing.T) {
	stream := `{"type":"message_end","content":"Body.","usage":{"input":100,"output":25}}
{"type":"turn_end","message":{"usage":{"input":0,"output":0,"totalTokens":0}}}`
	if got := parseGenerateStream(stream).Tokens.Output; got != 25 {
		t.Errorf("Tokens.Output = %d, want 25 preserved", got)
	}
}

// A later message replaces an earlier one rather than appending, so a multi-turn
// stream yields the final answer instead of a concatenation of drafts.
func TestParseGenerateStream_LastMessageWins(t *testing.T) {
	stream := `{"type":"message_end","content":"first"}
{"type":"message_end","content":"second"}`
	if got := parseGenerateStream(stream).Text; got != "second" {
		t.Errorf("Text = %q, want the final message", got)
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
