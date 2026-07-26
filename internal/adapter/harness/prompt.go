package harness

import (
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
)

// RenderPrompt flattens a request's prompt and labelled context into the single
// string a CLI harness accepts as its positional prompt argument.
//
// Assembly and trimming happened upstream in internal/llm, which owns the budget
// — this only renders what it was given, so the text a harness receives matches
// the golden-tested request rather than being rebuilt per adapter.
//
// When the caller asked for JSON but the harness has no native schema support,
// the schema is appended as an instruction. That is the schema-in-prompt fallback
// the capability flag promises: a best-effort ask rather than enforcement, with
// the caller parsing defensively.
func RenderPrompt(req adapter.GenerateRequest) string {
	var sb strings.Builder
	sb.WriteString(req.Prompt)

	for _, part := range req.Context {
		if strings.TrimSpace(part.Content) == "" {
			continue
		}
		sb.WriteString("\n\n")
		if part.Label != "" {
			sb.WriteString("## ")
			sb.WriteString(part.Label)
			sb.WriteString("\n\n")
		}
		sb.WriteString(part.Content)
	}

	return sb.String()
}

// RenderPromptWithSchema is RenderPrompt plus an embedded schema instruction,
// for harnesses that cannot enforce one natively.
func RenderPromptWithSchema(req adapter.GenerateRequest) string {
	prompt := RenderPrompt(req)
	if req.Format != adapter.FormatJSON || len(req.Schema) == 0 {
		return prompt
	}
	return prompt + "\n\nRespond with JSON only, matching this schema exactly. " +
		"Do not wrap it in a code fence or add commentary:\n\n" + string(req.Schema)
}
