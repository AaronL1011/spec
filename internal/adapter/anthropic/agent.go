// Package anthropic implements the agent completion plane using the Anthropic
// Messages API. It is completion-only: there is no session plane, so Invoke
// returns adapter.ErrNotSupported and `spec build` degrades with an explanation.
//
// This provider exists so drafting works without a coding harness installed —
// an API key is enough. Users who already run Claude Code should prefer the
// claude-code provider, which serves completions through the harness's own auth
// and needs no separate key.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// DefaultModel is the default Anthropic model for completions.
const DefaultModel = "claude-sonnet-4-20250514"

// DefaultBaseURL is the Anthropic API base URL.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultMaxTokens caps a completion when the task does not specify a budget.
const DefaultMaxTokens = 4096

// DefaultTimeout bounds a single completion. See openaicompat.DefaultTimeout for
// why this is far longer than the 10s house budget for ordinary API calls.
const DefaultTimeout = 120 * time.Second

// maxRawTail bounds the provider output retained for debugging when parsing
// yields no text, matching the InvokeResult convention.
const maxRawTail = 500

// Client implements adapter.AgentAdapter's completion plane using the Anthropic
// Messages API.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewClient creates a completion-only Anthropic agent adapter.
// model defaults to DefaultModel if empty.
func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
}

// Invoke reports that this provider has no session plane. Callers test with
// errors.Is(err, adapter.ErrNotSupported) and degrade.
func (c *Client) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	return nil, fmt.Errorf("anthropic agent does not support sessions: %w", adapter.ErrNotSupported)
}

// Capabilities reports a completion-only provider. StructuredOutput is false:
// the Messages API enforces a schema only through tool definitions, which is a
// different request shape than this adapter sends, so callers fall back to
// schema-in-prompt rather than being promised native enforcement.
func (c *Client) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Generate: true}
}

// Generate performs one Messages API completion. Containment is inherent: this
// is a plain HTTP request, so the model has no tools, no shell, and no repo
// access.
func (c *Client) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	body := messagesRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  []message{{Role: "user", Content: assemblePrompt(req)}},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling completion request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating completion request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.apiKey)
	httpReq.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading completion response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (HTTP %d): %s", resp.StatusCode, truncate(respBody, maxRawTail))
	}

	var result messagesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Output-format drift degrades to an empty result with a bounded tail
		// rather than an error, matching the InvokeResult convention.
		return &adapter.GenerateResult{Raw: truncate(respBody, maxRawTail)}, nil
	}

	text := result.Text()
	out := &adapter.GenerateResult{
		Text:  text,
		Model: result.Model,
		Tokens: adapter.TokenUsage{
			Input:  result.Usage.InputTokens,
			Output: result.Usage.OutputTokens,
			Total:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}
	if text == "" {
		out.Raw = truncate(respBody, maxRawTail)
	}
	return out, nil
}

// assemblePrompt appends labelled context blocks after the prompt. Trimming and
// ordering are the caller's job (internal/llm owns the budget); the adapter only
// renders what it is given.
func assemblePrompt(req adapter.GenerateRequest) string {
	if len(req.Context) == 0 {
		return req.Prompt
	}
	var sb strings.Builder
	sb.WriteString(req.Prompt)
	for _, part := range req.Context {
		if part.Content == "" {
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

// --- API types ---

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

// Text extracts the concatenated text from all text content blocks.
func (r *messagesResponse) Text() string {
	var result string
	for _, block := range r.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}
	return result
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func truncate(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
