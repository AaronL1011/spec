package tui

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// splashTickInterval drives the boot-splash animation. It runs faster than
// the status-bar spinner tick so the splash reads as alive and smooth during
// the brief window before the first dashboard payload lands.
const splashTickInterval = 60 * time.Millisecond

// splashFadeFrames is how many ticks the fade-out toward the dashboard takes
// once the first payload has landed (~400ms at splashTickInterval). Long
// enough to read as an ease, short enough to never feel like waiting.
const splashFadeFrames = 7

// Wordmark shimmer tuning. The shimmer is a single soft sheen — like light
// catching a surface — that crosses the logotype exactly once: a slow smooth
// entry, a quicker glide through the middle, and a slow smooth exit.
const (
	wordmarkGradientDepth = 0.45 // how far the base gradient leans from Accent toward Text
	shimmerStrength       = 0.75 // peak brightening applied at the sheen's centre
	shimmerRadius         = 6    // half-width of the sheen, in cells
	shimmerDelayFrames    = 6    // ticks the mark holds still before the sheen enters (~0.35s)
	shimmerSweepFrames    = 18   // ticks the one eased edge-to-edge pass takes (~2.2s)
)

// splashMinFrames is the minimum number of animation frames the splash holds
// before it may begin fading out: one complete shimmer sweep (~1.4s at
// splashTickInterval). The first payload can land instantly on a warm cache;
// this floor guarantees the opening animation always runs to completion.
const splashMinFrames = shimmerDelayFrames + shimmerSweepFrames + splashFadeFrames

// splashTickMsg advances the boot-splash animation by one frame.
type splashTickMsg time.Time

// splashWords is the pool of words shown beside the boot loader. One is
// picked at random each launch. Edit freely — short, lowercase,
// present-tense words read best.
var splashWords = []string{
	"aligning",
	"cooperating",
	"communicating",
	"orienting",
	"focusing",
	"refining",
	"sharpening",
	"igniting",
	"inspiring",
	"disseminating",
	"sharing",
}

// splashModel is the boot splash shown while the first dashboard fetch is in
// flight, so the app's first real frame is fully contentful.
type splashModel struct {
	word      string
	frame     int
	ready     bool
	fading    bool
	fadeFrame int
}

// newSplash creates a splash with a randomly chosen loader word.
func newSplash() splashModel {
	return splashModel{word: splashWords[rand.IntN(len(splashWords))]}
}

// nextFrame advances the animation clock: the shimmer and spinner every tick,
// and the fade-out counter once the fade has begun. Once the first payload
// has landed (readyForExit), it starts the fade as soon as splashMinFrames
// have elapsed, so the opening animation always completes a full shimmer
// sweep regardless of how fast the data arrives.
func (s *splashModel) nextFrame() {
	s.frame++
	if s.ready && !s.fading && s.frame >= splashMinFrames {
		s.fading = true
	}
	if s.fading {
		s.fadeFrame++
	}
}

// readyForExit records that the first dashboard payload has landed. The fade
// does not start here — nextFrame begins it once splashMinFrames have
// elapsed, guaranteeing a minimum exposure time for the opening animation.
func (s *splashModel) readyForExit() {
	s.ready = true
}

// done reports whether the fade-out has fully landed on the base colour, at
// which point the dashboard can paint as a single clean repaint.
func (s splashModel) done() bool {
	return s.fading && s.fadeFrame >= splashFadeFrames
}

// fadeFraction returns the eased 0→1 progress of the fade-out, or 0 while
// the splash is still holding.
func (s splashModel) fadeFraction() float64 {
	if !s.fading {
		return 0
	}
	return smoothstep(float64(s.fadeFrame) / splashFadeFrames)
}

// splashTick schedules the next animation frame.
func splashTick() tea.Cmd {
	return tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// view renders the full-screen splash: spark, logotype, and loader stacked
// dead-centre of the window. It owns the whole terminal — no chrome. All
// colours derive from the active theme so the splash always sits naturally
// in the user's configured palette.
func (s splashModel) view(width, height int, styles Styles) string {
	t := styles.Theme
	fade := s.fadeFraction()

	spinner := glyph.SpinnerFrames[s.frame%len(glyph.SpinnerFrames)]
	lines := []string{s.paint(IconSpark, t.Warning, t, fade), ""}
	lines = append(lines, s.wordmarkLines(width, t, fade)...)
	lines = append(lines, "", s.paint(spinner+" "+s.word, t.Muted, t, fade))

	var b strings.Builder
	for range max((height-len(lines))/2, 0) {
		b.WriteByte('\n')
	}
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(centerLine(line, width))
	}
	return b.String()
}

// wordmarkLines renders the logotype rows, falling back to a plain-text mark
// when the terminal is too narrow to fit the half-block art.
func (s splashModel) wordmarkLines(width int, t Theme, fade float64) []string {
	if lipgloss.Width(glyph.Wordmark[0]) > width {
		return []string{s.paint("s p e c", t.Accent, t, fade)}
	}
	lines := make([]string, 0, len(glyph.Wordmark))
	for _, row := range glyph.Wordmark {
		lines = append(lines, s.paintWordmarkRow(row, t, fade))
	}
	return lines
}

// paintWordmarkRow colours one row of the logotype cell by cell: a subtle
// left-to-right gradient from Accent toward Text, with a soft shimmer band
// sweeping across in time with the loader.
func (s splashModel) paintWordmarkRow(row string, t Theme, fade float64) string {
	w := lipgloss.Width(row)
	band := s.shimmerCol(w)
	var b strings.Builder
	x := 0
	for _, r := range row {
		if r == ' ' {
			b.WriteByte(' ')
			x++
			continue
		}
		f := 0.0
		if w > 1 {
			f = float64(x) / float64(w-1)
		}
		c := blendColor(t.Accent, t.Text, wordmarkGradientDepth*f)
		if k := shimmerIntensity(x, band); k > 0 {
			c = blendColor(c, t.Text, shimmerStrength*k)
		}
		b.WriteString(s.paint(string(r), c, t, fade))
		x++
	}
	return b.String()
}

// paint renders text in a single colour, blended toward the theme base while
// the splash is easing out so the whole composition fades as one.
func (s splashModel) paint(text string, c color.Color, t Theme, fade float64) string {
	return lipgloss.NewStyle().Foreground(blendColor(c, t.Base, fade)).Render(text)
}

// shimmerCol returns the sheen's centre column for the current frame. The
// sheen makes a single eased pass (smoothstep) across the full mark — easing
// in from the left edge, gliding through the middle like a reflection, and
// easing out past the right edge — then never returns.
func (s splashModel) shimmerCol(w int) float64 {
	f := s.frame - shimmerDelayFrames
	if f < 0 || f > shimmerSweepFrames {
		return -2 * shimmerRadius // before entry or after exit: fully off-mark
	}
	p := smoothstep(float64(f) / shimmerSweepFrames)
	return -shimmerRadius + p*float64(w-1+2*shimmerRadius)
}

// shimmerIntensity returns the brightening factor for column x given the
// band's centre: 1 at the centre, falling linearly to 0 at the band's edge.
func shimmerIntensity(x int, center float64) float64 {
	d := math.Abs(float64(x) - center)
	if d >= shimmerRadius {
		return 0
	}
	return 1 - d/shimmerRadius
}

// smoothstep is the classic ease-in-out curve over f∈[0,1]: gentle start,
// gentle landing. Out-of-range input is clamped.
func smoothstep(f float64) float64 {
	switch {
	case f <= 0:
		return 0
	case f >= 1:
		return 1
	}
	return f * f * (3 - 2*f)
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
