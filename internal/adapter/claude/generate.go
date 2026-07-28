package claude

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/harness"
)

// containmentFlags are the hard tool-disable flags that make a headless Claude
// Code run a completion rather than an agent session.
//
// Every entry is load-bearing:
//   - `--tools ""` disables the entire built-in tool set, so there is no Edit,
//     no Write, and no Bash;
//   - `--strict-mcp-config` with no --mcp-config means MCP servers configured
//     anywhere else on the machine are ignored, so the model cannot reach spec's
//     own authoring port or any other server;
//   - `--no-session-persistence` keeps a one-shot draft out of the user's
//     resumable session history.
//
// `--bare` looks like a shortcut to the same place and is deliberately NOT used:
// it forces Anthropic auth to ANTHROPIC_API_KEY and never reads OAuth or the
// keychain, which would silently break single-auth for every subscription user —
// the exact benefit that serving completions through the harness is meant to
// deliver.
var containmentFlags = []string{
	"--tools", "",
	"--strict-mcp-config",
	"--no-session-persistence",
}

// generateArgs builds the full argv for a contained completion.
func generateArgs(req adapter.GenerateRequest, model string) []string {
	args := []string{"-p", "--output-format", "json"}
	args = append(args, containmentFlags...)

	if model != "" {
		args = append(args, "--model", model)
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	// Claude Code enforces a JSON schema natively, so a structured request is
	// honoured rather than degraded to schema-in-prompt.
	if req.Format == adapter.FormatJSON && len(req.Schema) > 0 {
		args = append(args, "--json-schema", string(req.Schema))
	}

	args = append(args, harness.RenderPrompt(req))
	return args
}

// Generate runs one contained completion through Claude Code's headless JSON
// mode, riding the harness's own auth so no separate API key is needed.
func (a *Agent) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	out, err := harness.Run(ctx, a.Command, generateArgs(req, a.Model), a.Timeout)
	if err != nil {
		return nil, err
	}
	return parseGenerateOutput(out), nil
}

// resultEnvelope is the subset of `claude -p --output-format json` output this
// adapter reads.
type resultEnvelope struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Model   string `json:"model"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// parseGenerateOutput reads the structured envelope and never errors on
// unexpected shapes.
//
// Output-format drift across harness versions is a question of when, not if, so
// an unparseable response degrades to an empty result with a bounded tail for
// debugging — the same convention InvokeResult already uses. Failing here would
// turn a cosmetic upstream change into a broken feature.
func parseGenerateOutput(out string) *adapter.GenerateResult {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return &adapter.GenerateResult{}
	}

	var env resultEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return &adapter.GenerateResult{Raw: harness.Truncate(trimmed, harness.MaxRawTail)}
	}

	res := &adapter.GenerateResult{
		Text:  strings.TrimSpace(env.Result),
		Model: env.Model,
		Tokens: adapter.TokenUsage{
			Input:  env.Usage.InputTokens,
			Output: env.Usage.OutputTokens,
			Total:  env.Usage.InputTokens + env.Usage.OutputTokens,
		},
	}
	if res.Text == "" || env.IsError {
		res.Text = ""
		res.Raw = harness.Truncate(trimmed, harness.MaxRawTail)
	}
	return res
}

// Capabilities reports Claude Code's supported features.
//
// Generate is true because the containment flags above are asserted by contract
// test: a harness whose tool-disable cannot be proven must not advertise the
// completion plane, or callers would hand it a "contained" request it might
// execute with full tool access.
//
// StructuredOutput is true because --json-schema enforces a schema natively.
func (a *Agent) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		MCP:              true,
		SystemPrompt:     true,
		Generate:         true,
		StructuredOutput: true,
	}
}
