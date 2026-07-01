package adapter

import (
	"context"
	"errors"
	"time"
)

// ErrRecipientUnknown is returned by NotifyUser when an adapter cannot
// resolve a handle to a user it can message directly. Callers fall back to a
// channel broadcast — this is never a fatal condition.
var ErrRecipientUnknown = errors.New("comms: recipient handle not resolvable")

// CommsAdapter sends notifications and retrieves mentions from a comms platform.
type CommsAdapter interface {
	// Notify sends a structured message to the configured channel.
	Notify(ctx context.Context, msg Notification) error
	// NotifyUser sends a notification directly to a specific user by handle,
	// bypassing the channel broadcast. Adapters that cannot resolve a handle
	// to a user, or that have no per-user delivery mechanism at all, return
	// ErrRecipientUnknown so the caller can fall back to Notify. Like every
	// comms call it is best-effort and never fatal.
	NotifyUser(ctx context.Context, handle string, msg Notification) error
	// PostStandup posts a formatted standup to the standup channel.
	PostStandup(ctx context.Context, standup StandupReport) error
	// FetchMentions returns recent mentions of spec IDs in configured channels.
	FetchMentions(ctx context.Context, since time.Time) ([]Mention, error)
}
