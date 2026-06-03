package adapter

import "context"

// AIAdapter manages LLM integration for content drafting.
type AIAdapter interface {
	// Complete sends a prompt and returns the completion.
	Complete(ctx context.Context, prompt string, system string) (string, error)
}
