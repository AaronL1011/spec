package git

import (
	"context"
	"strings"
	"time"
)

// LogEntry is a single commit in a repository's history, oldest-first when
// returned from Log. Patch carries the commit's unified diff text when
// LogOptions.WithPatch is set; callers parse it, git does not interpret it.
type LogEntry struct {
	SHA     string
	Author  string
	When    time.Time // author date — when the change was made, stable across rebases
	Message string    // subject line only
	Patch   string    // raw unified diff (empty unless WithPatch)
}

// LogOptions scopes a Log query.
type LogOptions struct {
	// Path limits history to commits touching this path (relative to repo root).
	Path string
	// WithPatch includes each commit's unified diff in LogEntry.Patch.
	WithPatch bool
}

// Log field/record separators: ASCII control characters that cannot appear in
// commit subjects or author names, making the -z-style parse unambiguous.
const (
	logRecordSep = "\x1e"
	logFieldSep  = "\x1f"
	logFieldsPer = 4
)

// logTimeout overrides the default git timeout: walking full history with
// patches on a large specs repo can legitimately exceed the 30s default.
const logTimeout = 2 * time.Minute

// Log returns the repository's commit history oldest-first in topological
// order (parents before children), so consumers can replay events forward.
func Log(ctx context.Context, dir string, opts LogOptions) ([]LogEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, logTimeout)
		defer cancel()
	}

	args := []string{
		"log", "--topo-order", "--reverse",
		"--format=" + logRecordSep + "%H" + logFieldSep + "%an" + logFieldSep + "%aI" + logFieldSep + "%s",
	}
	if opts.WithPatch {
		args = append(args, "-p")
	}
	if opts.Path != "" {
		args = append(args, "--", opts.Path)
	}

	out, err := Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// parseLog splits raw `git log` output on the record separator and decodes
// each record into a LogEntry. Malformed records are skipped rather than
// failing the whole read — analytics degrade, they don't crash.
func parseLog(out string) []LogEntry {
	var entries []LogEntry
	for _, record := range strings.Split(out, logRecordSep) {
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.SplitN(record, logFieldSep, logFieldsPer)
		if len(fields) != logFieldsPer {
			continue
		}
		when, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		message, patch := splitSubjectPatch(fields[3])
		entries = append(entries, LogEntry{
			SHA:     fields[0],
			Author:  fields[1],
			When:    when,
			Message: message,
			Patch:   patch,
		})
	}
	return entries
}

// splitSubjectPatch separates the subject line from the patch text that `git
// log -p` appends after the format line.
func splitSubjectPatch(tail string) (subject, patch string) {
	if idx := strings.Index(tail, "\n"); idx >= 0 {
		return strings.TrimRight(tail[:idx], "\r"), strings.TrimSpace(tail[idx+1:])
	}
	return strings.TrimSpace(tail), ""
}
