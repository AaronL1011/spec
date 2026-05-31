package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// StatusKind is the canonical set of status signals surfaced by the TUI. It
// formalises the previously scattered surfaces (spinner, pending notice,
// banner, toasts) into one taxonomy — no new application states are added
// (SPEC-016 §7.1). Ordering is significant for muscle-memory: idle is the
// resting state and every transient outcome decays back to it.
type StatusKind int

const (
	// StatusIdle is the resting state. The slot stays present (never collapses)
	// showing a dim glyph and a muted label so the layout never reflows.
	StatusIdle StatusKind = iota
	// StatusPending marks in-progress work. Its glyph is the only animated one.
	StatusPending
	// StatusSuccess marks a completed outcome. Decays back to idle.
	StatusSuccess
	// StatusError marks a failed outcome. Decays back to idle.
	StatusError
)

// idleClearLabel is the resting label when there is no pending work to report.
const idleClearLabel = "No pending work"

// Status is the single status component. It owns the status-bar slot formerly
// held by the "pending" notification and is the sole producer of status
// visuals. It maps the current StatusKind to an icon (animated for
// StatusPending) and a label, redrawn in place into a fixed footprint.
//
// Transient kinds (success, error) carry an expiry; once elapsed the component
// decays back to idle on the next View(). Expiry is evaluated lazily against
// the wall clock — like the retired Toast — so no extra timer is introduced
// and the pending animation reuses the existing refresh cadence (§7.2).
type Status struct {
	kind      StatusKind
	label     string
	expiresAt time.Time // zero for non-expiring kinds (idle, pending)
	frame     int       // pending-spinner animation frame
	// restingCount is the number of pending specs surfaced in the idle state.
	// The idle state is not empty boilerplate: it reports the standing "how
	// much work is waiting" signal (the former "N pending" badge, now folded in
	// here so the bar carries one status element instead of two). Transient
	// in-flight states override this and decay back to it.
	restingCount int
	styles       StatusStyles
}

// StatusStyles holds the styles for each status kind. All are derived from the
// existing "pending" element palette tokens by the caller for visual
// continuity (SPEC-016 §5.4 / AC-6); this component never defines colours.
type StatusStyles struct {
	Idle    lipgloss.Style
	Pending lipgloss.Style
	Success lipgloss.Style
	Error   lipgloss.Style
}

// NewStatus creates a status component in the idle state.
func NewStatus(styles StatusStyles) Status {
	return Status{kind: StatusIdle, styles: styles}
}

// SetRestingCount records how many specs are pending. It updates the standing
// idle signal only; it never disturbs a live pending/success/error state,
// which will reveal the new count when it decays back to idle.
func (s *Status) SetRestingCount(n int) {
	if n < 0 {
		n = 0
	}
	s.restingCount = n
}

// SetPending puts the element into the animated pending state with a
// present-tense label (e.g. "Syncing…"). Pending does not expire; it is
// cleared by a later SetSuccess/SetError or Idle.
func (s *Status) SetPending(label string) {
	s.kind = StatusPending
	s.label = label
	s.expiresAt = time.Time{}
}

// SetSuccess shows a positive outcome that decays back to idle after duration.
func (s *Status) SetSuccess(label string, duration time.Duration) {
	s.set(StatusSuccess, label, duration)
}

// SetError shows a failure that decays back to idle after duration. Errors
// must remain as legible as the old banner, so callers should pass a longer
// duration than for success (§7.2 behavioural-regression guard).
func (s *Status) SetError(label string, duration time.Duration) {
	s.set(StatusError, label, duration)
}

// SetIdle returns the element to its resting state immediately. The resting
// label is derived from the pending-spec count, not stored, so the slot always
// reflects the latest count.
func (s *Status) SetIdle() {
	s.kind = StatusIdle
	s.label = ""
	s.expiresAt = time.Time{}
}

func (s *Status) set(kind StatusKind, label string, duration time.Duration) {
	s.kind = kind
	s.label = label
	s.expiresAt = time.Now().Add(duration)
}

// NextFrame advances the pending-spinner animation. It is a no-op unless the
// element is pending, so non-pending states consume no animation frames
// (§7.1). Driven by the existing TUI spinner tick.
func (s *Status) NextFrame() {
	if s.current() != StatusPending {
		return
	}
	s.frame = (s.frame + 1) % len(glyph.SpinnerFrames)
}

// Animating reports whether the element currently needs animation ticks, so
// the app can render frames only while a task is pending.
func (s Status) Animating() bool { return s.current() == StatusPending }

// Kind returns the current (decay-resolved) status kind.
func (s Status) Kind() StatusKind { return s.current() }

// Label returns the current effective label.
func (s Status) Label() string { return s.effectiveLabel(s.current()) }

// ShowingOutcome reports whether a live success or error outcome is currently
// displayed (i.e. set and not yet decayed to idle).
func (s Status) ShowingOutcome() bool {
	k := s.current()
	return k == StatusSuccess || k == StatusError
}

// current resolves the effective kind, decaying expired transient states back
// to idle without mutating (View and Animating are value receivers).
func (s Status) current() StatusKind {
	if (s.kind == StatusSuccess || s.kind == StatusError) &&
		!s.expiresAt.IsZero() && !time.Now().Before(s.expiresAt) {
		return StatusIdle
	}
	return s.kind
}

// icon returns the glyph for a kind. Meaning is carried by shape so the status
// stays distinguishable when colour is stripped (accessibility).
func (s Status) icon(kind StatusKind) string {
	switch kind {
	case StatusPending:
		return glyph.SpinnerFrames[s.frame%len(glyph.SpinnerFrames)]
	case StatusSuccess:
		return glyph.Done
	case StatusError:
		return glyph.Rejected
	default:
		return glyph.Active
	}
}

func (s Status) style(kind StatusKind) lipgloss.Style {
	switch kind {
	case StatusPending:
		return s.styles.Pending
	case StatusSuccess:
		return s.styles.Success
	case StatusError:
		return s.styles.Error
	default:
		return s.styles.Idle
	}
}

// idleLabel renders the resting signal from the pending-spec count. This is
// the folded-in "N pending" badge: the idle state reports standing work rather
// than generic boilerplate, so the single element carries both signals.
func (s Status) idleLabel() string {
	if s.restingCount > 0 {
		return fmt.Sprintf("%d pending", s.restingCount)
	}
	return idleClearLabel
}

// effectiveLabel resolves the displayed label for a kind. Idle reports the
// pending-count resting signal; transient states show their caller label,
// falling back to the resting signal if a caller passed none.
func (s Status) effectiveLabel(kind StatusKind) string {
	if kind == StatusIdle || s.label == "" {
		return s.idleLabel()
	}
	return s.label
}

// View renders the status element as "<icon> <label>" through the kind's
// style. The element is always non-empty (idle reports pending work or "No
// pending work"), so the slot never collapses and surrounding panes never
// reflow (AC-3, AC-4). The caller owns the surrounding fixed-width slot; this
// returns the styled inner content.
func (s Status) View() string {
	kind := s.current()
	return s.style(kind).Render(s.icon(kind) + " " + s.effectiveLabel(kind))
}
