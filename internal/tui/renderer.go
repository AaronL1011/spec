package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/colorprofile"
)

// Renderer renders markdown content into ANSI-styled terminal text.
type Renderer interface {
	Render(ctx context.Context, md string, width int) (string, error)
}

// newRenderer returns the markdown renderer appropriate for the current
// output environment. When colour is disabled (NO_COLOR / a dumb terminal)
// it returns the unstyled PlainRenderer; otherwise the Glamour renderer
// themed from the resolved Theme.
func newRenderer(theme Theme) Renderer {
	if colourDisabled() {
		return NewPlainRenderer()
	}
	return NewGlamourRenderer(theme)
}

// colourDisabled reports whether ANSI styling should be suppressed, honouring
// the NO_COLOR convention (https://no-color.org) and a profile-less terminal.
// colorprofile.Detect already folds in NO_COLOR and TTY detection, returning
// NoTTY or Ascii when colour must be off.
func colourDisabled() bool {
	p := colorprofile.Detect(os.Stdout, os.Environ())
	return p == colorprofile.NoTTY || p == colorprofile.Ascii
}

// GlamourRenderer renders markdown using Glamour with a pre-resolved style.
// The TermRenderer is constructed once per (style, width) pair and cached, so
// termenv.HasDarkBackground is never called on the hot render path.
type GlamourRenderer struct {
	mu    sync.Mutex
	style string
	cache map[int]*glamour.TermRenderer // keyed by word-wrap width
}

// NewGlamourRenderer creates a renderer whose style is derived from the
// already-resolved Theme.  No terminal I/O is performed at construction time.
func NewGlamourRenderer(theme Theme) Renderer {
	return &GlamourRenderer{
		style: glamourStyleForTheme(theme),
		cache: make(map[int]*glamour.TermRenderer),
	}
}

// Render implements Renderer.
func (g *GlamourRenderer) Render(ctx context.Context, md string, width int) (string, error) {
	if strings.TrimSpace(md) == "" {
		return "", nil
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if width < 30 {
		width = 30
	}
	md = stripHTMLComments(md)

	r, err := g.rendererForWidth(width)
	if err != nil {
		// Fall back to unstyled text rather than block the user.
		return stripHTMLComments(md), nil
	}

	g.mu.Lock()
	out, err := r.Render(md)
	g.mu.Unlock()

	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// rendererForWidth returns a cached TermRenderer for the given width,
// building it if necessary.  Building uses WithStandardStyle (no terminal
// probe) and WithWordWrap (pure config).
func (g *GlamourRenderer) rendererForWidth(width int) (*glamour.TermRenderer, error) {
	g.mu.Lock()
	r, ok := g.cache[width]
	g.mu.Unlock()
	if ok {
		return r, nil
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(g.style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("glamour renderer: %w", err)
	}

	g.mu.Lock()
	g.cache[width] = r
	g.mu.Unlock()
	return r, nil
}

// glamourStyleForTheme maps a resolved Theme to a Glamour standard style by
// inspecting the base background colour luminance.  This avoids terminal I/O.
func glamourStyleForTheme(theme Theme) string {
	if isLightColour(theme.Base) {
		return styles.LightStyle
	}
	return styles.DarkStyle
}

// isLightColour returns true when a colour represents a light background
// (perceived luminance > 50%). lipgloss v2 colours implement color.Color, so
// luminance is computed from the resolved RGBA rather than a hex string.
func isLightColour(c color.Color) bool {
	if c == nil {
		return false
	}
	// color.Color.RGBA returns 16-bit channels (0..65535); shift down to 0..255.
	r, g, b, _ := c.RGBA()
	rf := float64(r >> 8)
	gf := float64(g >> 8)
	bf := float64(b >> 8)
	// BT.601 perceived luminance.
	return 0.299*rf+0.587*gf+0.114*bf > 128
}

// PlainRenderer is a plain-text fallback with no ANSI styling.
type PlainRenderer struct{}

// NewPlainRenderer creates a plain-text renderer.
func NewPlainRenderer() Renderer { return PlainRenderer{} }

func (PlainRenderer) Render(ctx context.Context, md string, _ int) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return stripHTMLComments(md), nil
}
