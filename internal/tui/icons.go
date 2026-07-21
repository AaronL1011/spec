package tui

import "github.com/aaronl1011/spec/internal/tui/glyph"

// Icon and glyph names for the tui package. The glyph literals themselves live
// in the dependency-free internal/tui/glyph package (the single source of
// truth), so the tui package and its components subpackage share one icon
// vocabulary without an import cycle. No glyph literals appear outside
// internal/tui/glyph.
const (
	IconFocus      = glyph.Focus
	IconSpark      = glyph.Spark
	IconActive     = glyph.Active
	IconStale      = glyph.Stale
	IconBlocked    = glyph.Blocked
	IconReview     = glyph.Review
	IconDiscussion = glyph.Discussion
	IconIncoming   = glyph.Incoming
	IconDone       = glyph.Done
	IconRejected   = glyph.Rejected
	IconChanges    = glyph.Changes
	IconPending    = glyph.Pending
	IconFilled     = glyph.Filled
	IconOpen       = glyph.Open

	IconBullet = glyph.Bullet
	IconCursor = glyph.Cursor
	IconCaret  = glyph.Caret
	IconGutter = glyph.Gutter
	IconClock  = glyph.Clock
	IconUrgent = glyph.Urgent // urgent triage item marker

	IconToastOK   = glyph.ToastOK
	IconToastErr  = glyph.ToastErr
	IconToastInfo = glyph.ToastInfo

	GlyphVSep    = glyph.VSep
	GlyphHRule   = glyph.HRule
	GlyphSection = glyph.Section
)

// PriorityIconFor returns the mono-width glyph for a triage priority. Meaning
// is carried by shape (filled dot vs hollow) plus the caller-applied colour.
func PriorityIconFor(priority string) string {
	switch priority {
	case "critical", "high", "medium", "low":
		return IconActive
	default:
		return IconOpen
	}
}

// StageIcons is the ordered set of mono-width glyphs used to mark pipeline
// stages by position. It replaces the previous emoji array; the pipeline view
// and stage-selection prompts both index into it. Colour distinguishes stage
// state at the call site. Indices beyond the slice fall back via StageIconAt.
var StageIcons = []string{
	IconIncoming, // 0  intake / triage
	IconPending,  // 1  draft
	IconReview,   // 2  first review
	IconChanges,  // 3  design
	IconActive,   // 4  engineering / build prep
	IconFilled,   // 5  build
	IconReview,   // 6  pr review
	IconDone,     // 7  qa-validation
	IconActive,   // 8  release
	IconBullet,   // 9  metrics
	IconDone,     // 10 done
	IconFilled,   // 11 packaged
	IconBlocked,  // 12 locked
	IconBullet,   // 13 discussion
	IconReview,   // 14 backlog
	IconOpen,     // 15 fallback / unknown
}

// StageIconAt returns the stage glyph for the given zero-based stage position,
// falling back to IconOpen when the position is out of range.
func StageIconAt(pos int) string {
	if pos < 0 || pos >= len(StageIcons) {
		return IconOpen
	}
	return StageIcons[pos]
}

// CIIconFor returns the glyph for a CI status.
func CIIconFor(status string) string {
	switch status {
	case "passing":
		return IconDone
	case "failing":
		return IconRejected
	case "pending":
		return IconChanges
	default:
		return IconPending
	}
}
