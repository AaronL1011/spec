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
// reconciles real progress from the durable node ledger after the agent exits,
// so the result carries no per-step signal; it exists for future structured
// reporting and to keep the adapter contract uniform.
type InvokeResult struct{}

// Capabilities describes the features an agent harness supports.
type Capabilities struct {
	MCP          bool
	Headless     bool
	Skills       bool
	SystemPrompt bool
}
