package adapter

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNotSupported reports that a provider does not implement the requested
// plane. Callers test for it with errors.Is and degrade gracefully — a
// completion-only provider has no session plane, and a session-only harness
// that cannot prove tool containment has no completion plane. Never match on
// the message text.
var ErrNotSupported = errors.New("not supported by this agent provider")

// AgentAdapter manages coding agent integration across two planes of the same
// capability.
//
// The session plane (Invoke) spawns the agent as an interactive or headless
// subprocess with MCP config, skills, and a system prompt: this is the build
// and interactive-authoring contract. The completion plane (Generate) is
// one-shot, contained generation for drafting and summarising — no tools, no
// repo access, no interactivity.
//
// A provider need not implement both. Consumers negotiate via Capabilities
// before use, and an unimplemented plane returns ErrNotSupported.
type AgentAdapter interface {
	// Invoke spawns the agent as a subprocess. It blocks until the agent exits.
	Invoke(ctx context.Context, req InvokeRequest) (*InvokeResult, error)
	// Generate performs a single contained completion. Implementations must not
	// grant the model tools, repo access, or interactivity.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)
	// Capabilities describes what the agent harness supports.
	Capabilities() Capabilities
}

// InvokeRequest carries everything an agent needs for a build session.
type InvokeRequest struct {
	SpecID        string   // active spec id (e.g. SPEC-042)
	WorkDir       string   // working directory the agent runs in
	ContextFile   string   // consolidated markdown fallback for non-MCP agents
	MCPConfigPath string   // engine-generated; runs `spec mcp-server --spec <id>`
	SystemPrompt  string   // assembled build instructions
	SkillPaths    []string // reproducibility skill paths (may be empty)
	Prompt        string   // kickoff prompt (may be empty)
	Headless      bool     // -p mode for `spec fix --auto` / CI
}

// InvokeResult reports what the agent did during the session. spec-cli
// reconciles real per-node progress from the durable node ledger after the
// agent exits; this result carries the session-level signal that the ledger
// cannot capture — why the run ended, how it failed, and what it cost — so that
// autonomous (`--auto`) runs are debuggable from the activity log.
//
// All fields are best-effort. A headless harness whose output cannot be parsed
// yields a zero-value result with Raw populated; callers must treat empty
// fields as "unknown", never as an error.
type InvokeResult struct {
	// ExitReason is why the session ended: "completed", "error",
	// "interrupted", or "" when it could not be determined.
	ExitReason string `json:"exitReason,omitempty"`
	// ErrorClass categorises a failure (e.g. "auto_retry_exhausted",
	// "compaction_failed", "nonzero_exit"). Empty on success.
	ErrorClass string `json:"errorClass,omitempty"`
	// ErrorMessage is the harness-reported failure detail, when present.
	ErrorMessage string `json:"errorMessage,omitempty"`
	// Tokens aggregates token usage across the session, when the harness
	// reports it.
	Tokens TokenUsage `json:"tokens,omitempty"`
	// Raw holds a bounded tail of the harness output, retained for debugging
	// when structured parsing yields nothing.
	Raw string `json:"raw,omitempty"`
}

// TokenUsage aggregates the token counts a headless harness reports over a
// session. Zero values mean the harness did not report that figure.
type TokenUsage struct {
	Input  int `json:"input,omitempty"`
	Output int `json:"output,omitempty"`
	Total  int `json:"total,omitempty"`
}

// GenerateRequest is a single contained completion request. The caller (the
// task registry in internal/llm) owns all prompt assembly; the adapter only
// translates this into its provider's wire format or CLI invocation.
type GenerateRequest struct {
	// Task is the stable task id (e.g. "draft-section"), used for telemetry
	// and to scope per-task budgets.
	Task string
	// System is the task-specific system prompt.
	System string
	// Prompt is the assembled user prompt.
	Prompt string
	// Context carries labelled blocks the adapter appends verbatim. Assembly
	// and trimming happen before the adapter sees the request.
	Context []ContextPart
	// MaxTokens caps the response. Honoured by completion-API providers only;
	// headless harnesses expose no token cap and ignore it. 0 = provider
	// default.
	MaxTokens int
	// Format selects the output shape. FormatJSON is only honoured when
	// Capabilities.StructuredOutput is true; otherwise the caller is expected
	// to have embedded the schema in the prompt and to parse defensively.
	Format OutputFormat
	// Schema is the JSON schema for Format == FormatJSON. Ignored otherwise.
	Schema json.RawMessage
}

// ContextPart is a labelled block of context with a trimming weight. Higher
// weights survive budget trimming; see internal/llm for the assembly rules.
type ContextPart struct {
	Label   string
	Content string
	Weight  int
}

// OutputFormat selects the shape of a Generate response.
type OutputFormat int

const (
	// FormatMarkdown is prose or markdown output. The default.
	FormatMarkdown OutputFormat = iota
	// FormatJSON requests structured output against GenerateRequest.Schema.
	FormatJSON
)

// GenerateResult carries a completion and what it cost. All fields except Text
// are best-effort: a provider that does not report usage leaves Tokens zero,
// and a provider that does not name its model leaves Model empty. Callers must
// treat empty fields as "unknown", never as an error.
type GenerateResult struct {
	// Text is the generated content.
	Text string
	// Tokens reports usage when the provider supplies it; zero = unreported.
	Tokens TokenUsage
	// Model names what served the request, when known.
	Model string
	// Raw holds a bounded tail of provider output, populated only when
	// structured parsing yielded no text. Lets output-format drift degrade
	// into a debuggable empty result instead of an error.
	Raw string
}

// Capabilities describes the features an agent harness supports. Consumers read
// this once and gate their affordances on it: an unsupported action must be
// explained, never offered and then failed.
type Capabilities struct {
	// MCP reports whether the harness can be given an MCP server config.
	MCP bool
	// Headless reports whether the harness has a non-interactive mode.
	Headless bool
	// Skills reports whether the harness accepts skill paths.
	Skills bool
	// SystemPrompt reports whether the harness accepts a system prompt.
	SystemPrompt bool
	// Generate reports whether the completion plane is available. For a
	// session-capable harness this is true only when the adapter can prove
	// tool containment (hard tool-disable flags asserted by contract test);
	// a harness that cannot be contained does not advertise completions.
	Generate bool
	// StructuredOutput reports whether the provider enforces a JSON schema
	// natively. When false, FormatJSON degrades to schema-in-prompt and the
	// caller parses defensively.
	StructuredOutput bool
}
