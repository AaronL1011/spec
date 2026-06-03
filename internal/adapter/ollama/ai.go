// Package ollama implements AIAdapter using the Ollama local API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultModel is the default Ollama model.
const DefaultModel = "llama3.1"

// DefaultBaseURL is the default Ollama API base URL.
const DefaultBaseURL = "http://localhost:11434"

// Client implements adapter.AIAdapter using the Ollama REST API.
type Client struct {
	model   string
	baseURL string
	http    *http.Client
}

// NewClient creates an Ollama AIAdapter.
// model defaults to DefaultModel if empty. baseURL defaults to DefaultBaseURL.
func NewClient(model, baseURL string) *Client {
	if model == "" {
		model = DefaultModel
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		model:   model,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second, // local models can be slow on first load
		},
	}
}

// Complete sends a prompt to the Ollama generate API and returns the completion text.
func (c *Client) Complete(ctx context.Context, prompt string, system string) (string, error) {
	body := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}
	if system != "" {
		body.Messages = append([]chatMessage{{Role: "system", Content: system}}, body.Messages...)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling Ollama API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama API error (HTTP %d): %s", resp.StatusCode, truncate(respBody, 500))
	}

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return result.Message.Content, nil
}

// --- API types ---

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
}

func truncate(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
