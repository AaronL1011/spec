package adapter

import "context"

// AgentAdapter manages coding agent integration. The build engine assembles
// all context (spec, MCP config, skills, system prompt) and hands the adapter
// a structured InvokeRequest; the adapter only translates that request into the
// CLI invocation for its harness.
type AgentAdapter interface {
	// Invoke spawns the agent as a subprocess. It blocks until the agent exits.
	Invoke(ctx context.Context, req InvokeRequest) (*InvokeResult, error)
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

// Capabilities describes the features an agent harness supports.
type Capabilities struct {
	MCP          bool
	Headless     bool
	Skills       bool
	SystemPrompt bool
}
