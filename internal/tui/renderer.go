package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/glamour"
)

// Renderer renders markdown content into ANSI-styled terminal text.
type Renderer interface {
	Render(ctx context.Context, md string, width int) (string, error)
}

// GlamourRenderer renders markdown using Glamour.
type GlamourRenderer struct{}

// NewGlamourRenderer creates a Glamour-backed renderer.
func NewGlamourRenderer() Renderer {
	return GlamourRenderer{}
}

func (GlamourRenderer) Render(ctx context.Context, md string, width int) (string, error) {
	if strings.TrimSpace(md) == "" {
		return "", nil
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	md = stripHTMLComments(md)
	if width < 30 {
		width = 30
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return md, nil
	}

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := r.Render(md)
		done <- result{out: strings.TrimRight(out, "\n"), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		if res.err != nil {
			return "", res.err
		}
		return res.out, nil
	}
}

// PlainRenderer is a fallback renderer that returns markdown as plain text.
type PlainRenderer struct{}

// NewPlainRenderer creates a plain-text renderer.
func NewPlainRenderer() Renderer {
	return PlainRenderer{}
}

func (PlainRenderer) Render(ctx context.Context, md string, _ int) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return stripHTMLComments(md), nil
}
