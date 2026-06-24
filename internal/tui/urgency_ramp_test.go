package tui

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

// rgb is shared with theme_test.go in this package.

func TestRampColorEndpoints(t *testing.T) {
	th := Theme{
		Text:    lipgloss.Color("#000000"),
		Warning: lipgloss.Color("#888800"),
		Error:   lipgloss.Color("#ff0000"),
	}
	// f<=0 yields the first stop (primary text).
	r, g, b := rgb(th.RampColor(0))
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("RampColor(0) = #%02x%02x%02x, want #000000 (Text)", r, g, b)
	}
	// f>=1 yields the last stop (Error / hottest).
	r, g, b = rgb(th.RampColor(1))
	if r != 0xff || g != 0 || b != 0 {
		t.Errorf("RampColor(1) = #%02x%02x%02x, want #ff0000 (Error)", r, g, b)
	}
	// Over-range clamps.
	r2, g2, b2 := rgb(th.RampColor(5))
	if r2 != 0xff || g2 != 0 || b2 != 0 {
		t.Errorf("RampColor(5) = #%02x%02x%02x, want clamp to Error", r2, g2, b2)
	}
}

func TestRampColorMonotonicRedChannel(t *testing.T) {
	// As f increases from 0→1 the derived ramp (Text→Warning→blend→Error) should
	// move the red channel non-decreasingly for this Text=black, Error=red theme.
	th := Theme{
		Text:    lipgloss.Color("#000000"),
		Warning: lipgloss.Color("#aaaa00"),
		Error:   lipgloss.Color("#ff0000"),
	}
	prev := -1
	for i := 0; i <= 20; i++ {
		f := float64(i) / 20
		r, _, _ := rgb(th.RampColor(f))
		if int(r) < prev {
			t.Fatalf("red channel decreased at f=%v: %d < %d", f, r, prev)
		}
		prev = int(r)
	}
}

func TestRampColorUsesExplicitRamp(t *testing.T) {
	// A theme-provided ramp (e.g. graphite luminance) is used verbatim at stops.
	th := Theme{
		Text: lipgloss.Color("#111111"),
		UrgencyRamp: []color.Color{
			lipgloss.Color("#d8d8d8"),
			lipgloss.Color("#ececec"),
		},
	}
	r, g, b := rgb(th.RampColor(0))
	if r != 0xd8 || g != 0xd8 || b != 0xd8 {
		t.Errorf("RampColor(0) = #%02x%02x%02x, want #d8d8d8 (explicit ramp stop)", r, g, b)
	}
}
