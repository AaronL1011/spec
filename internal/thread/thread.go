// Package thread provides inline Q&A threads for spec review.
//
// A thread is a lightweight, section-anchored conversation: a question, a
// list of replies, and an open/resolved flag. Threads are persisted as a
// sidecar YAML file next to the spec so they ride the existing git-backed
// specs-repo sync without touching the spec markdown or its frontmatter.
//
// The engine performs no terminal I/O and shells out to nothing. Callers
// (the CLI, the TUI, and the MCP handler) drive it through the Store
// interface and render the results themselves.
package thread

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Status values for a thread.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
)

// Thread is a single section-anchored conversation.
type Thread struct {
	// ID is a short, stable, content-independent identifier (e.g. "T-7f3a").
	// It never changes, so replies and resolves never collide on renumbering.
	ID string `yaml:"id"`

	// Section is the markdown section slug the thread is anchored to.
	// This is the only anchor in v1 — a thread is never orphaned by line shifts.
	Section string `yaml:"section"`

	// Status is open or resolved.
	Status string `yaml:"status"`

	// Author is the handle/name of whoever asked the question.
	Author string `yaml:"author"`

	// Created is when the question was asked (UTC).
	Created time.Time `yaml:"created"`

	// Question is the opening message.
	Question string `yaml:"question"`

	// Replies are appended in chronological order.
	Replies []Reply `yaml:"replies,omitempty"`

	// ResolvedBy and ResolvedAt are set when the thread is resolved.
	ResolvedBy string     `yaml:"resolved_by,omitempty"`
	ResolvedAt *time.Time `yaml:"resolved_at,omitempty"`

	// Mentions are the handles addressed by the question, in first-seen order.
	// Derived from the body (and any explicit --to handles) at write time;
	// never hand-edited.
	Mentions []string `yaml:"mentions,omitempty"`

	// Quote is the verbatim text span the thread refers to within Section.
	// Optional. When the text no longer appears in the section, the thread
	// degrades to a section-level anchor — it is never orphaned.
	Quote string `yaml:"quote,omitempty"`

	// QuotePrefix is a short run of text immediately before Quote, used to
	// disambiguate when Quote appears more than once in the section.
	QuotePrefix string `yaml:"quote_prefix,omitempty"`
}

// Reply is a single message appended to a thread.
type Reply struct {
	Author   string    `yaml:"author"`
	At       time.Time `yaml:"at"`
	Body     string    `yaml:"body"`
	Mentions []string  `yaml:"mentions,omitempty"`
}

// IsOpen reports whether the thread is still awaiting resolution.
func (t Thread) IsOpen() bool { return t.Status != StatusResolved }

// LastActivity returns the thread's most recent activity timestamp: the last
// reply's time when replies exist, else the creation time. Read-state
// tracking compares against this so a new reply re-marks a thread unread.
func (t Thread) LastActivity() time.Time {
	latest := t.Created
	if n := len(t.Replies); n > 0 && t.Replies[n-1].At.After(latest) {
		latest = t.Replies[n-1].At
	}
	if t.ResolvedAt != nil && t.ResolvedAt.After(latest) {
		latest = *t.ResolvedAt
	}
	return latest
}

// Participants returns the deduplicated set of handles involved in a thread:
// the author, every replier, and every mentioned handle. Order is stable
// (author first, then first-seen).
func (t Thread) Participants() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(handle string) {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			return
		}
		key := strings.ToLower(strings.TrimPrefix(handle, "@"))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, handle)
	}

	add(t.Author)
	for _, m := range t.Mentions {
		add(m)
	}
	for _, r := range t.Replies {
		add(r.Author)
		for _, m := range r.Mentions {
			add(m)
		}
	}
	return out
}

// newID returns a short, stable thread identifier such as "T-7f3a".
// Randomness avoids collisions when two people create threads offline.
func newID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a timestamp-derived suffix; collisions are still
		// astronomically unlikely and the merge layer dedupes by ID.
		return fmt.Sprintf("T-%06x", time.Now().UnixNano()&0xffffff)
	}
	return "T-" + hex.EncodeToString(b[:])
}

// validateQuestion trims and rejects empty questions so we never write an
// empty sidecar entry.
func validateQuestion(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", fmt.Errorf("question is empty — nothing to ask")
	}
	return q, nil
}

// validateReply trims and rejects empty reply bodies.
func validateReply(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("reply is empty — nothing to add")
	}
	return body, nil
}
