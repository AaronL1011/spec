package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// splashTickInterval drives the boot-splash loader animation. It runs faster
// than the status-bar spinner tick so the splash reads as alive and smooth
// during the brief window before the first dashboard payload lands.
const splashTickInterval = 80 * time.Millisecond

// splashTickMsg advances the boot-splash loader by one frame.
type splashTickMsg time.Time

// splashWords is the pool of words shown beside the boot loader. One is
// picked at random each launch. Edit freely — short, lowercase,
// present-tense words read best.
var splashWords = []string{
	"aligning",
	"cooperating",
	"orienting",
	"focusing",
	"refining",
	"sharpening",
	"igniting",
	"inspiring",
}

// splashModel is the boot splash shown while the first dashboard fetch is in
// flight, so the app's first real frame is fully contentful.
type splashModel struct {
	word  string
	frame int
}

// newSplash creates a splash with a randomly chosen loader word.
func newSplash() splashModel {
	return splashModel{word: splashWords[rand.IntN(len(splashWords))]}
}

// nextFrame advances the braille loader animation by one frame.
func (s *splashModel) nextFrame() {
	s.frame = (s.frame + 1) % len(glyph.SpinnerFrames)
}

// splashTick schedules the next loader animation frame.
func splashTick() tea.Cmd {
	return tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// view renders the full-screen splash: the wordmark dead-centre of the
// window with the braille loader beneath it. It owns the whole terminal —
// no chrome. Colours come from the active theme so the splash always sits
// naturally in the user's configured palette.
func (s splashModel) view(width, height int, styles Styles) string {
	spark := lipgloss.NewStyle().Foreground(styles.Theme.Warning)
	wordmark := lipgloss.NewStyle().Foreground(styles.Theme.Accent).Bold(true)

	title := spark.Render(IconSpark) + "  " + wordmark.Render("s p e c")
	spinner := glyph.SpinnerFrames[s.frame%len(glyph.SpinnerFrames)]
	loader := styles.Muted.Render(spinner + " " + s.word)

	// The wordmark sits on the window's true centre row; the loader hangs a
	// blank line beneath it so the mark — not the block — is what reads as
	// centred.
	top := (height - 1) / 2

	var b strings.Builder
	for range max(top, 0) {
		b.WriteByte('\n')
	}
	b.WriteString(centerLine(title, width))
	b.WriteString("\n\n")
	b.WriteString(centerLine(loader, width))
	return b.String()
}

// centerLine pads line with leading spaces so it sits horizontally centred
// within width. Lines wider than the terminal are returned unpadded.
func centerLine(line string, width int) string {
	pad := (width - lipgloss.Width(line)) / 2
	if pad <= 0 {
		return line
	}
	return strings.Repeat(" ", pad) + line
}
