// Package anthropic implements AIAdapter using the Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultModel is the default Anthropic model for completions.
const DefaultModel = "claude-sonnet-4-20250514"

// DefaultBaseURL is the Anthropic API base URL.
const DefaultBaseURL = "https://api.anthropic.com"

// Client implements adapter.AIAdapter using the Anthropic Messages API.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewClient creates an Anthropic AIAdapter.
// model defaults to DefaultModel if empty.
func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: DefaultBaseURL,
		http: &http.Client{
			Timeout: 60 * time.Second, // LLM completions can be slow
		},
	}
}

// Complete sends a prompt to the Anthropic Messages API and returns the completion text.
func (c *Client) Complete(ctx context.Context, prompt string, system string) (string, error) {
	body := messagesRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}
	if system != "" {
		body.System = system
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API error (HTTP %d): %s", resp.StatusCode, truncateBody(respBody, 500))
	}

	var result messagesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return result.Text(), nil
}

// Embed returns a vector embedding for the given text.
// Anthropic does not currently offer a public embeddings API.
// Returns nil, ErrEmbeddingsNotSupported.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, ErrEmbeddingsNotSupported
}

// ErrEmbeddingsNotSupported indicates the provider does not support embeddings.
var ErrEmbeddingsNotSupported = fmt.Errorf("anthropic does not support embeddings — use a separate embedding provider or Ollama")

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

func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
