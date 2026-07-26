// Package openaicompat implements the agent completion plane against any
// OpenAI-compatible /v1/chat/completions endpoint.
//
// A local completions endpoint is a protocol, not a vendor: Ollama, llama.cpp's
// llama-server, LM Studio, vLLM, LocalAI, and every hosted gateway expose the
// OpenAI chat-completions shape. One adapter therefore serves the whole family,
// and vendor names are presets over it (see internal/adapter/resolve) rather
// than separate code paths. Adding a server nobody anticipated needs no change
// here — just a base_url.
//
// This provider is completion-only: it has no session plane, so Invoke returns
// adapter.ErrNotSupported and `spec build` degrades with an explanation.
package openaicompat

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

// DefaultTimeout bounds a single completion. It is deliberately far longer than
// the 10s house budget for API calls: a local model routinely needs 30-60s to
// draft a spec section, and a budget that kills those requests would make the
// feature look broken rather than slow. Callers surface elapsed time so a long
// wait stays legible.
const DefaultTimeout = 120 * time.Second

// maxRawTail bounds the provider output retained for debugging when parsing
// yields no text, matching the InvokeResult convention.
const maxRawTail = 500

// Client implements adapter.AgentAdapter's completion plane against an
// OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	model   string
	token   string
	http    *http.Client
}

// Options configures a Client. BaseURL is required; everything else is
// optional. Model is passed through verbatim when set, so the server's own
// default applies when it is empty. Token is sent as a bearer header only when
// set, so air-gapped local servers need no credential while hosted gateways
// remain reachable through the same adapter.
type Options struct {
	BaseURL string
	Model   string
	Token   string
	Timeout time.Duration
}

// New creates a completion-only agent adapter for an OpenAI-compatible
// endpoint. It returns an error naming the missing field when BaseURL is empty,
// because a completions client with no endpoint has no useful default.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.BaseURL) == "" {
		return nil, fmt.Errorf("openai-compatible agent: 'base_url' is required — set agent.generate.base_url in ~/.spec/config.yaml (e.g. http://localhost:11434/v1), or use a vendor preset such as 'ollama'")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
		model:   opts.Model,
		token:   opts.Token,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// BaseURL returns the resolved endpoint. Exported so preset resolution can be
// asserted by test without reaching into adapter internals.
func (c *Client) BaseURL() string { return c.baseURL }

// Invoke reports that this provider has no session plane. Callers test with
// errors.Is(err, adapter.ErrNotSupported) and degrade.
func (c *Client) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	return nil, fmt.Errorf("openai-compatible agent does not support sessions: %w", adapter.ErrNotSupported)
}

// Capabilities reports a completion-only provider. StructuredOutput is false:
// response_format support varies by server, and claiming it for every
// OpenAI-compatible endpoint would promise something a bare llama-server does
// not honour. Callers fall back to schema-in-prompt.
func (c *Client) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Generate: true}
}

// Generate performs one chat completion. Containment is inherent: this is a
// plain HTTP request, so the model has no tools, no shell, and no repo access.
func (c *Client) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	messages := make([]chatMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, chatMessage{Role: "user", Content: assemblePrompt(req)})

	body := chatRequest{
		Model:     c.model,
		Messages:  messages,
		Stream:    false,
		MaxTokens: req.MaxTokens,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling completion request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating completion request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading completion response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("completions endpoint %s returned HTTP %d: %s", url, resp.StatusCode, truncate(respBody, maxRawTail))
	}

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Output-format drift degrades to an empty result with a bounded tail
		// rather than an error, matching the InvokeResult convention.
		return &adapter.GenerateResult{Raw: truncate(respBody, maxRawTail)}, nil
	}

	text := result.text()
	out := &adapter.GenerateResult{
		Text:  text,
		Model: result.Model,
		Tokens: adapter.TokenUsage{
			Input:  result.Usage.PromptTokens,
			Output: result.Usage.CompletionTokens,
			Total:  result.Usage.TotalTokens,
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

// --- wire types ---

type chatRequest struct {
	Model    string        `json:"model,omitempty"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	// MaxTokens uses the modern spelling; servers that only accept the legacy
	// field name ignore it, which degrades to the server default rather than
	// failing the request.
	MaxTokens int `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (r *chatResponse) text() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

func truncate(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
