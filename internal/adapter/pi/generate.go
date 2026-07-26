package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/harness"
)

// containmentFlags are the hard disable flags that make a headless pi run a
// completion rather than an agent session.
//
// Each one closes a specific hole:
//   - --no-tools removes every built-in and extension tool, so there is no
//     read, write, edit, or bash;
//   - --no-extensions stops extension discovery, which can register further
//     tools and CLI flags;
//   - --no-skills and --no-prompt-templates stop skill and template discovery,
//     which would otherwise inject instructions from the filesystem;
//   - --no-context-files stops AGENTS.md / CLAUDE.md discovery, so an unrelated
//     repo's house rules cannot steer a spec draft;
//   - --no-session keeps a one-shot draft out of session history.
//
// The discovery flags matter as much as --no-tools: without them a completion is
// contained but not reproducible, because its instructions would depend on
// whatever directory the process happened to start in.
var containmentFlags = []string{
	"--no-tools",
	"--no-extensions",
	"--no-skills",
	"--no-prompt-templates",
	"--no-context-files",
	"--no-session",
}

// generateArgs builds the full argv for a contained completion.
func generateArgs(req adapter.GenerateRequest, model string) []string {
	args := []string{"-p", "--mode", "json"}
	args = append(args, containmentFlags...)

	if model != "" {
		// Passed through verbatim: pi accepts "provider/id" and defaults to the
		// google provider, so the user's spelling is the correct one and spec
		// does not translate model names.
		args = append(args, "--model", model)
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}

	// pi's --mode json is an output envelope, not schema-enforced generation, so
	// a JSON request degrades to schema-in-prompt and the caller parses
	// defensively. Capabilities.StructuredOutput is false to say so.
	args = append(args, harness.RenderPromptWithSchema(req))
	return args
}

// Generate runs one contained completion through pi's headless JSON mode, riding
// pi's own auth so no separate API key is needed.
func (a *Agent) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	out, err := harness.Run(ctx, a.Command, generateArgs(req, a.Model), a.Timeout)
	if err != nil {
		return nil, err
	}
	return parseGenerateStream(out), nil
}

// generateEvent is a permissive view over the pi event stream, decoding only the
// fields this adapter acts on so drift in unrelated fields never breaks parsing.
type generateEvent struct {
	Type    string `json:"type"`
	Model   string `json:"model"`
	Content any    `json:"content"`
	Text    string `json:"text"`
	Message *struct {
		Model   string      `json:"model"`
		Content any         `json:"content"`
		Text    string      `json:"text"`
		Usage   *usageBlock `json:"usage"`
	} `json:"message"`
	Usage *usageBlock `json:"usage"`
}

// parseGenerateStream extracts assistant text from pi's JSON event stream.
//
// Only structured events are read, and an unparseable stream degrades to an
// empty result with a bounded tail rather than an error — output-format drift
// across harness versions should cost a draft, not break the feature.
func parseGenerateStream(out string) *adapter.GenerateResult {
	res := &adapter.GenerateResult{}

	var (
		text  strings.Builder
		tail  strings.Builder
		usage adapter.TokenUsage
	)

	scanner := bufio.NewScanner(strings.NewReader(out))
	// Message snapshots can be large; raise the line cap well above the default.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		appendTail(&tail, line)

		var ev generateEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Interleaved non-JSON log output: skip rather than fail.
			continue
		}

		if ev.Model != "" {
			res.Model = ev.Model
		}
		if ev.Message != nil && ev.Message.Model != "" {
			res.Model = ev.Message.Model
		}

		// The terminal assistant message carries the completion. Both event
		// spellings are accepted because the envelope has varied across
		// versions and neither is expensive to support.
		switch ev.Type {
		case "message_end", "agent_end", "assistant_message":
			if s := eventText(ev); s != "" {
				text.Reset()
				text.WriteString(s)
			}
		}

		if ev.Usage != nil {
			addUsage(&usage, ev.Usage)
		}
		if ev.Message != nil && ev.Message.Usage != nil {
			addUsage(&usage, ev.Message.Usage)
		}
	}

	res.Text = strings.TrimSpace(text.String())
	res.Tokens = usage
	if res.Text == "" {
		res.Raw = harness.Truncate(strings.TrimSpace(tail.String()), harness.MaxRawTail)
	}
	return res
}

// eventText pulls assistant text out of an event, tolerating the shapes pi uses:
// a plain string, a list of content blocks, or a bare text field.
func eventText(ev generateEvent) string {
	if s := contentText(ev.Content); s != "" {
		return s
	}
	if ev.Text != "" {
		return ev.Text
	}
	if ev.Message != nil {
		if s := contentText(ev.Message.Content); s != "" {
			return s
		}
		if ev.Message.Text != "" {
			return ev.Message.Text
		}
	}
	return ""
}

// contentText renders a content field that may be a string or a block list.
func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Only text blocks contribute; tool blocks cannot appear here since
			// tools are disabled, but ignoring them is free insurance.
			if t, _ := block["type"].(string); t != "" && t != "text" {
				continue
			}
			if s, ok := block["text"].(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String()
	default:
		return ""
	}
}

// Capabilities reports pi's supported features.
//
// Generate is true because the containment flags above are asserted by contract
// test. StructuredOutput is false: --mode json is an output envelope rather than
// schema-enforced generation, so a JSON request falls back to schema-in-prompt.
func (a *Agent) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		MCP:          true,
		Headless:     true,
		Skills:       true,
		SystemPrompt: true,
		Generate:     true,
	}
}
