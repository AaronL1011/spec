// Package glyph is the single source of truth for every glyph the TUI
// renders. It is a dependency-free leaf package so both the tui package and
// its components subpackage can share one icon vocabulary without an import
// cycle. No glyph literals may live outside this package.
//
// Every glyph here is exactly one terminal cell wide. Emoji are deliberately
// avoided: they render at inconsistent cell widths across terminals, breaking
// column-alignment maths. Semantic meaning is carried by glyph SHAPE and, at
// the call site, theme COLOUR — so glyphs stay orthogonal to the palette.
package glyph

// Status and semantic icons. Each is a distinct shape so status remains
// distinguishable even when colour is stripped (accessibility).
const (
	Focus      = "★" // focused spec marker
	Spark      = "✦" // brand spark (boot splash wordmark)
	Active     = "●" // active / DO
	Stale      = "◷" // stale / waiting
	Blocked    = "■" // blocked
	Review     = "◆" // review pending
	Discussion = "◐" // open discussion thread awaiting the viewer
	Incoming   = "▸" // incoming / inbox
	Done       = "✓" // done / approved / passing
	Rejected   = "✗" // rejected / failed
	Changes    = "↻" // changes requested / pending CI
	Pending    = "□" // pending / empty box / not-yet
	Filled     = "▣" // filled / complete box
	Open       = "○" // open / not started

	Bullet  = "•" // list bullet
	Cursor  = "▸" // selection / focus cursor
	Caret   = "▌" // text input caret
	Gutter  = "▍" // reader gutter marker beside a thread-anchored line
	Clock   = "◷" // stale / age badge
	Urgent  = "▲" // urgent severity marker
	Upgrade = "↑" // newer version available
)

// Toast icons.
const (
	ToastOK   = "✓"
	ToastErr  = "✗"
	ToastInfo = "ℹ"
)

// Structural glyphs. Already mono-width; named here so views never inline them.
const (
	VSep    = "│" // vertical separator
	HRule   = "─" // horizontal rule unit
	Section = "§" // section marker
)

// SpinnerFrames is the mono-width Braille spinner animation.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Wordmark is the boot-splash "spec" logotype, drawn in half-block cells.
// Every cell is exactly one column wide, so the mark measures and centres
// like ordinary text. All rows must be the same width; the splash paints it
// cell by cell to apply its gradient.
var Wordmark = []string{
	"█▀▀ █▀█ █▀▀ █▀▀",
	"▀▀█ █▀▀ █▀▀ █  ",
	"▀▀▀ ▀   ▀▀▀ ▀▀▀",
}
