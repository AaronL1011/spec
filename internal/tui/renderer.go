package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

// Renderer renders markdown content into ANSI-styled terminal text.
type Renderer interface {
	Render(ctx context.Context, md string, width int) (string, error)
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
	if isLightColour(string(theme.Base)) {
		return styles.LightStyle
	}
	return styles.DarkStyle
}

// isLightColour returns true when a hex colour string represents a light
// background (perceived luminance > 50%).
func isLightColour(hex string) bool {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) < 6 {
		return false
	}
	var r, g, b uint64
	_, _ = fmt.Sscanf(hex[0:2], "%x", &r)
	_, _ = fmt.Sscanf(hex[2:4], "%x", &g)
	_, _ = fmt.Sscanf(hex[4:6], "%x", &b)
	// BT.601 perceived luminance.
	return 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) > 128
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
