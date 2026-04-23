// Package teams implements CommsAdapter using Microsoft Teams webhooks.
//
// Outbound notifications use the Incoming Webhook connector (simple HTTP POST
// with Adaptive Card payload). No OAuth or Graph API is required for sending.
//
// FetchMentions optionally uses the Microsoft Graph API (requires an app
// registration with ChannelMessage.Read.All). If Graph credentials are not
// configured, FetchMentions degrades gracefully and returns nil.
package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aaronl1011/spec-cli/internal/adapter"
)

// Client implements adapter.CommsAdapter using Microsoft Teams.
type Client struct {
	webhookURL     string
	standupWebhook string
	graphToken     string // optional — for FetchMentions via Graph API
	teamID         string // optional — Graph API team/channel identifiers
	channelID      string
	http           *http.Client
}

// NewClient creates a Teams CommsAdapter.
// webhookURL is the Incoming Webhook URL for the default notification channel.
// standupWebhook is optional; defaults to webhookURL if empty.
// graphToken, teamID, channelID are optional for FetchMentions support.
func NewClient(webhookURL, standupWebhook, graphToken, teamID, channelID string) *Client {
	if standupWebhook == "" {
		standupWebhook = webhookURL
	}
	return &Client{
		webhookURL:     webhookURL,
		standupWebhook: standupWebhook,
		graphToken:     graphToken,
		teamID:         teamID,
		channelID:      channelID,
		http:           &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends a structured notification to Teams via the Incoming Webhook.
func (c *Client) Notify(ctx context.Context, msg adapter.Notification) error {
	webhook := c.webhookURL
	if msg.Channel != "" && msg.Channel != "default" {
		// Custom per-channel webhooks would need a map; for now use default.
		webhook = c.webhookURL
	}

	card := buildNotificationCard(msg)
	return c.postCard(ctx, webhook, card)
}

// PostStandup posts a formatted standup to the standup webhook.
func (c *Client) PostStandup(ctx context.Context, standup adapter.StandupReport) error {
	card := buildStandupCard(standup)
	return c.postCard(ctx, c.standupWebhook, card)
}

// FetchMentions retrieves messages mentioning spec IDs from a Teams channel.
// Requires Graph API credentials (graphToken, teamID, channelID). If not
// configured, returns nil, nil — graceful degradation per spec.
func (c *Client) FetchMentions(ctx context.Context, since time.Time) ([]adapter.Mention, error) {
	if c.graphToken == "" || c.teamID == "" || c.channelID == "" {
		return nil, nil
	}

	url := fmt.Sprintf(
		"https://graph.microsoft.com/v1.0/teams/%s/channels/%s/messages?$top=50&$orderby=createdDateTime desc",
		c.teamID, c.channelID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Graph request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.graphToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil // degrade gracefully on network error
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // degrade gracefully on auth/permission error
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}

	var result graphMessagesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil
	}

	var mentions []adapter.Mention
	for _, msg := range result.Value {
		created, _ := time.Parse(time.RFC3339, msg.CreatedDateTime)
		if created.Before(since) {
			continue
		}
		content := msg.Body.Content
		specID := extractSpecID(content)
		if specID == "" {
			continue
		}
		mentions = append(mentions, adapter.Mention{
			SpecID:    specID,
			Channel:   c.channelID,
			Author:    msg.From.User.DisplayName,
			Preview:   truncate(stripHTML(content), 120),
			Timestamp: created,
		})
	}
	return mentions, nil
}

// postCard sends an Adaptive Card to a Teams webhook.
func (c *Client) postCard(ctx context.Context, webhookURL string, card adaptiveCard) error {
	payload := webhookPayload{
		Type:        "message",
		Attachments: []attachment{{
			ContentType: "application/vnd.microsoft.card.adaptive",
			ContentURL:  "",
			Content:     card,
		}},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling Teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating Teams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("posting to Teams webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("teams webhook error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

// --- Adaptive Card builders ---

func buildNotificationCard(msg adapter.Notification) adaptiveCard {
	var body []cardElement

	// Header
	body = append(body, cardElement{
		Type:   "TextBlock",
		Text:   fmt.Sprintf("**[%s]** %s", msg.SpecID, msg.Title),
		Size:   "Medium",
		Weight: "Bolder",
		Wrap:   true,
	})

	// Message
	if msg.Message != "" {
		body = append(body, cardElement{
			Type: "TextBlock",
			Text: msg.Message,
			Wrap: true,
		})
	}

	// Mention
	if msg.Mention != "" {
		body = append(body, cardElement{
			Type:     "TextBlock",
			Text:     fmt.Sprintf("cc %s", msg.Mention),
			IsSubtle: true,
			Size:     "Small",
		})
	}

	return adaptiveCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body:    body,
	}
}

func buildStandupCard(standup adapter.StandupReport) adaptiveCard {
	var body []cardElement

	// Header
	body = append(body, cardElement{
		Type:   "TextBlock",
		Text:   fmt.Sprintf("**Standup — %s — %s**", standup.UserName, standup.Date),
		Size:   "Medium",
		Weight: "Bolder",
	})

	// Separator
	body = append(body, cardElement{Type: "TextBlock", Text: "---"})

	if len(standup.Yesterday) > 0 {
		body = append(body, cardElement{
			Type: "TextBlock",
			Text: "**Yesterday:**\n" + bulletList(standup.Yesterday),
			Wrap: true,
		})
	}

	if len(standup.Today) > 0 {
		body = append(body, cardElement{
			Type: "TextBlock",
			Text: "**Today:**\n" + bulletList(standup.Today),
			Wrap: true,
		})
	}

	if len(standup.Blockers) > 0 {
		body = append(body, cardElement{
			Type:  "TextBlock",
			Text:  "**Blockers:**\n" + bulletList(standup.Blockers),
			Wrap:  true,
			Color: "Attention",
		})
	}

	return adaptiveCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body:    body,
	}
}

// --- Payload types ---

type webhookPayload struct {
	Type        string       `json:"type"`
	Attachments []attachment `json:"attachments"`
}

type attachment struct {
	ContentType string       `json:"contentType"`
	ContentURL  string       `json:"contentUrl"`
	Content     adaptiveCard `json:"content"`
}

type adaptiveCard struct {
	Schema  string        `json:"$schema"`
	Type    string        `json:"type"`
	Version string        `json:"version"`
	Body    []cardElement `json:"body"`
}

type cardElement struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Size     string `json:"size,omitempty"`
	Weight   string `json:"weight,omitempty"`
	Wrap     bool   `json:"wrap,omitempty"`
	IsSubtle bool   `json:"isSubtle,omitempty"`
	Color    string `json:"color,omitempty"`
}

// --- Graph API types ---

type graphMessagesResponse struct {
	Value []graphMessage `json:"value"`
}

type graphMessage struct {
	CreatedDateTime string `json:"createdDateTime"`
	Body            struct {
		Content string `json:"content"`
	} `json:"body"`
	From struct {
		User struct {
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"from"`
}

// --- Helpers ---

func bulletList(items []string) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// extractSpecID finds the first SPEC-NNN pattern in a string.
func extractSpecID(text string) string {
	idx := strings.Index(text, "SPEC-")
	if idx < 0 {
		return ""
	}
	end := idx + 5
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == idx+5 {
		return ""
	}
	return text[idx:end]
}

// stripHTML removes HTML tags from a string (simple regex-free approach).
func stripHTML(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}
