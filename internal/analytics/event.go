// Package analytics reconstructs team flow metrics (lead time, cycle time,
// stage dwell, bottlenecks) from the specs repo git history. It consumes
// plain git.LogEntry values and never shells out itself.
package analytics

import (
	"regexp"
	"time"

	"github.com/aaronl1011/spec/internal/git"
)

// EventKind classifies a spec lifecycle event.
type EventKind string

// Event kinds derived from the specs repo history.
const (
	KindScaffolded EventKind = "scaffolded"
	KindAdvanced   EventKind = "advanced"
	KindReverted   EventKind = "reverted"
	KindEjected    EventKind = "ejected"
	KindResumed    EventKind = "resumed"
)

// EventSource records which extraction tier attributed the event kind.
type EventSource string

// Extraction tiers: conventional commit messages (fast path) and frontmatter
// status diffs (truth path for manual edits and unconventional history).
const (
	SourceMessage     EventSource = "message"
	SourceFrontmatter EventSource = "frontmatter"
)

// StatusBlocked is the escape-hatch status stamped by `spec eject`.
// Mirrors pipeline.StatusBlocked without importing the pipeline engine.
const StatusBlocked = "blocked"

// Event is a single stage transition (or scaffold) for one spec.
type Event struct {
	SpecID    string
	Kind      EventKind
	FromStage string // empty for scaffolds
	ToStage   string
	At        time.Time
	Source    EventSource
}

// ExtractResult carries the extracted events plus honesty counters for the
// report footer ("analysed N/M transitions").
type ExtractResult struct {
	Events         []Event
	CommitsScanned int
	Transitions    int // status transitions successfully attributed
	Unattributable int // spec-file changes whose status could not be read
}

// Tier-1 commit message patterns, exactly as emitted by the workflow engine
// (internal/workflow/advance.go, revert.go, block.go) and cmd scaffolding.
var (
	reMsgAdvance  = regexp.MustCompile(`^feat: advance (SPEC-\d+) to \S+$`)
	reMsgRevert   = regexp.MustCompile(`^fix: revert (SPEC-\d+) to \S+`)
	reMsgEject    = regexp.MustCompile(`^fix: eject (SPEC-\d+)`)
	reMsgResume   = regexp.MustCompile(`^fix: resume (SPEC-\d+) to \S+$`)
	reMsgScaffold = regexp.MustCompile(`^feat: (?:scaffold (SPEC-\d+)|promote \S+ to (SPEC-\d+))`)
)

// ExtractEvents replays the commit log (oldest-first) into typed lifecycle
// events. Frontmatter status diffs are the source of truth; commit messages
// only classify the kind when stage names are unknown to the pipeline.
func ExtractEvents(entries []git.LogEntry, stageNames []string) ExtractResult {
	stageIdx := make(map[string]int, len(stageNames))
	for i, name := range stageNames {
		stageIdx[name] = i
	}

	res := ExtractResult{CommitsScanned: len(entries)}
	for _, entry := range entries {
		changes, unreadable := parsePatchStatusChanges(entry.Patch)
		res.Unattributable += unreadable
		msgSpec, msgKind := classifyMessage(entry.Message)
		for _, ch := range changes {
			ev := changeToEvent(ch, entry.When, stageIdx, msgSpec, msgKind)
			res.Events = append(res.Events, ev)
			res.Transitions++
		}
	}
	return res
}

// classifyMessage matches a commit subject against the tier-1 patterns,
// returning the spec ID it names and the event kind, or empty when the
// message is not a lifecycle commit.
func classifyMessage(subject string) (specID string, kind EventKind) {
	switch {
	case reMsgAdvance.MatchString(subject):
		return reMsgAdvance.FindStringSubmatch(subject)[1], KindAdvanced
	case reMsgRevert.MatchString(subject):
		return reMsgRevert.FindStringSubmatch(subject)[1], KindReverted
	case reMsgEject.MatchString(subject):
		return reMsgEject.FindStringSubmatch(subject)[1], KindEjected
	case reMsgResume.MatchString(subject):
		return reMsgResume.FindStringSubmatch(subject)[1], KindResumed
	case reMsgScaffold.MatchString(subject):
		m := reMsgScaffold.FindStringSubmatch(subject)
		if m[1] != "" {
			return m[1], KindScaffolded
		}
		return m[2], KindScaffolded
	}
	return "", ""
}

// changeToEvent converts a raw frontmatter status change into a typed event.
// Kind precedence: blocked-status semantics, then stage ordering from the
// configured pipeline, then the commit message, then a conservative default.
func changeToEvent(ch statusChange, at time.Time, stageIdx map[string]int, msgSpec string, msgKind EventKind) Event {
	ev := Event{
		SpecID:    ch.specID,
		FromStage: ch.from,
		ToStage:   ch.to,
		At:        at,
		Source:    SourceFrontmatter,
	}
	if msgSpec == ch.specID && msgKind != "" {
		ev.Source = SourceMessage
	}

	fromIdx, fromKnown := stageIdx[ch.from]
	toIdx, toKnown := stageIdx[ch.to]
	switch {
	case ch.added:
		ev.Kind = KindScaffolded
		ev.FromStage = ""
	case ch.to == StatusBlocked:
		ev.Kind = KindEjected
	case ch.from == StatusBlocked:
		ev.Kind = KindResumed
	case fromKnown && toKnown && toIdx < fromIdx:
		ev.Kind = KindReverted
	case fromKnown && toKnown:
		ev.Kind = KindAdvanced
	case ev.Source == SourceMessage:
		ev.Kind = msgKind
	default:
		ev.Kind = KindAdvanced
	}
	return ev
}
