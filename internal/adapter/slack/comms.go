// Package slack implements CommsAdapter using the Slack API.
package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/aaronl1011/spec-cli/internal/adapter"
)

// Client implements adapter.CommsAdapter using the Slack API.
type Client struct {
	api            *slackapi.Client
	defaultChannel string
	standupChannel string
}

// NewClient creates a Slack CommsAdapter.
// token is a Slack Bot Token (xoxb-...). defaultChannel is the channel for general
// notifications (e.g., "#platform"). standupChannel is for standup posts.
func NewClient(token, defaultChannel, standupChannel string) *Client {
	api := slackapi.New(token)
	if standupChannel == "" {
		standupChannel = defaultChannel
	}
	return &Client{
		api:            api,
		defaultChannel: normaliseChannel(defaultChannel),
		standupChannel: normaliseChannel(standupChannel),
	}
}

// Notify sends a structured notification to a Slack channel.
func (c *Client) Notify(ctx context.Context, msg adapter.Notification) error {
	channel := c.defaultChannel
	if msg.Channel != "" {
		channel = normaliseChannel(msg.Channel)
	}

	blocks := buildNotificationBlocks(msg)

	_, _, err := c.api.PostMessageContext(
		ctx,
		channel,
		slackapi.MsgOptionBlocks(blocks...),
		slackapi.MsgOptionText(msg.Message, false), // fallback for notifications
	)
	if err != nil {
		return fmt.Errorf("posting notification to %s: %w", channel, err)
	}
	return nil
}

// PostStandup posts a formatted standup to the standup channel.
func (c *Client) PostStandup(ctx context.Context, standup adapter.StandupReport) error {
	blocks := buildStandupBlocks(standup)

	_, _, err := c.api.PostMessageContext(
		ctx,
		c.standupChannel,
		slackapi.MsgOptionBlocks(blocks...),
		slackapi.MsgOptionText(fmt.Sprintf("Standup - %s - %s", standup.UserName, standup.Date), false),
	)
	if err != nil {
		return fmt.Errorf("posting standup to %s: %w", c.standupChannel, err)
	}
	return nil
}

// FetchMentions searches for recent messages mentioning spec IDs in configured channels.
// Requires the Slack search:read scope (available on paid plans).
func (c *Client) FetchMentions(ctx context.Context, since time.Time) ([]adapter.Mention, error) {
	query := fmt.Sprintf("SPEC- after:%s", since.Format("2006-01-02"))

	params := slackapi.SearchParameters{
		Sort:      "timestamp",
		SortDirection: "desc",
		Count:     20,
		Page:      1,
	}

	msgs, err := c.api.SearchMessagesContext(ctx, query, params)
	if err != nil {
		// Search may not be available (free plan). Degrade gracefully.
		return nil, nil
	}

	var mentions []adapter.Mention
	for _, match := range msgs.Matches {
		specID := extractSpecID(match.Text)
		if specID == "" {
			continue
		}
		mentions = append(mentions, adapter.Mention{
			SpecID:    specID,
			Channel:   match.Channel.Name,
			Author:    match.Username,
			Preview:   truncate(match.Text, 120),
			Timestamp: parseSlackTimestamp(match.Timestamp),
		})
	}
	return mentions, nil
}

// buildNotificationBlocks creates Slack Block Kit blocks for a notification.
func buildNotificationBlocks(msg adapter.Notification) []slackapi.Block {
	var blocks []slackapi.Block

	// Header with spec ID and title
	header := fmt.Sprintf("*[%s]* %s", msg.SpecID, msg.Title)
	blocks = append(blocks, slackapi.NewSectionBlock(
		slackapi.NewTextBlockObject("mrkdwn", header, false, false),
		nil, nil,
	))

	// Message body
	if msg.Message != "" {
		blocks = append(blocks, slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject("mrkdwn", msg.Message, false, false),
			nil, nil,
		))
	}

	// Mention
	if msg.Mention != "" {
		mention := fmt.Sprintf("cc %s", msg.Mention)
		blocks = append(blocks, slackapi.NewContextBlock(
			"",
			slackapi.NewTextBlockObject("mrkdwn", mention, false, false),
		))
	}

	return blocks
}

// buildStandupBlocks creates Slack Block Kit blocks for a standup report.
func buildStandupBlocks(standup adapter.StandupReport) []slackapi.Block {
	var blocks []slackapi.Block

	// Header
	header := fmt.Sprintf("*Standup — %s — %s*", standup.UserName, standup.Date)
	blocks = append(blocks, slackapi.NewHeaderBlock(
		slackapi.NewTextBlockObject("plain_text", header, false, false),
	))

	blocks = append(blocks, slackapi.NewDividerBlock())

	// Yesterday
	if len(standup.Yesterday) > 0 {
		text := "*Yesterday:*\n" + bulletList(standup.Yesterday)
		blocks = append(blocks, slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject("mrkdwn", text, false, false),
			nil, nil,
		))
	}

	// Today
	if len(standup.Today) > 0 {
		text := "*Today:*\n" + bulletList(standup.Today)
		blocks = append(blocks, slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject("mrkdwn", text, false, false),
			nil, nil,
		))
	}

	// Blockers
	if len(standup.Blockers) > 0 {
		text := "*Blockers:*\n" + bulletList(standup.Blockers)
		blocks = append(blocks, slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject("mrkdwn", text, false, false),
			nil, nil,
		))
	}

	return blocks
}

func bulletList(items []string) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString("• ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}

func normaliseChannel(ch string) string {
	return strings.TrimPrefix(ch, "#")
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
	end := idx + 5 // past "SPEC-"
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == idx+5 {
		return "" // no digits after SPEC-
	}
	return text[idx:end]
}

func parseSlackTimestamp(ts string) time.Time {
	// Slack timestamps are Unix epoch with microseconds: "1234567890.123456"
	parts := strings.Split(ts, ".")
	if len(parts) == 0 {
		return time.Time{}
	}
	var sec int64
	_, _ = fmt.Sscanf(parts[0], "%d", &sec)
	return time.Unix(sec, 0)
}
