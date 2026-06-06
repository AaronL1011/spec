package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

// triageDetailPane renders the read-only detail view for a selected triage item.
// It is embedded in triageModel rather than being a standalone model; it
// dispatches action commands up to the App via tea.Cmd messages.
type triageDetailPane struct {
	item         triageItem
	scroll       int
	contentLines int // cached line count of the scrollable body
	width        int
	height       int
}

// triageDetailOpenMsg signals that a detail pane should open for a triage item.
type triageDetailOpenMsg struct{ Item triageItem }

// triageDetailCloseMsg signals that the detail pane should close.
type triageDetailCloseMsg struct{}

// newTriageDetailPane constructs a detail pane for the given item.
func newTriageDetailPane(item triageItem, width, height int) *triageDetailPane {
	return &triageDetailPane{item: item, width: width, height: height}
}

func (p *triageDetailPane) setSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *triageDetailPane) scrollUp() {
	if p.scroll > 0 {
		p.scroll--
	}
}

func (p *triageDetailPane) scrollDown() {
	mx := p.maxScroll()
	if p.scroll < mx {
		p.scroll++
	}
}

func (p *triageDetailPane) maxScroll() int {
	visible := p.visibleContentRows()
	mx := p.contentLines - visible
	if mx < 0 {
		return 0
	}
	return mx
}

// visibleContentRows returns the number of scrollable body rows available
// after reserving space for the permanently anchored hint strip.
func (p *triageDetailPane) visibleContentRows() int {
	v := p.height - 1
	if v < 1 {
		v = 1
	}
	return v
}

// updateItem replaces the displayed item, preserving scroll position.
func (p *triageDetailPane) updateItem(item triageItem) {
	p.item = item
	mx := p.maxScroll()
	if p.scroll > mx {
		p.scroll = mx
	}
}

// view renders the detail pane with a scrollable body and a permanently
// anchored action hint row at the bottom.
func (p *triageDetailPane) view(styles Styles, rc *config.ResolvedConfig) string {
	body := p.renderBody(styles)
	hints := p.buildHints(styles, rc)

	bodyLines := splitLines(body)
	p.contentLines = len(bodyLines)

	visible := p.visibleContentRows()

	scroll := p.scroll
	if mx := p.maxScroll(); scroll > mx {
		scroll = mx
	}

	end := scroll + visible
	if end > len(bodyLines) {
		end = len(bodyLines)
	}
	window := bodyLines[scroll:end]

	for len(window) < visible {
		window = append(window, "")
	}

	return strings.Join(window, "\n") + "\n" + hints
}

// renderBody builds the full scrollable content (header, meta, body, history).
func (p *triageDetailPane) renderBody(styles Styles) string {
	item := p.item
	var b strings.Builder

	var severityMarker string
	if item.isUrgent() {
		severityMarker = styles.Error.Render(IconUrgent+" ") + styles.Title.Render(item.Title)
	} else {
		severityMarker = styles.Muted.Render(IconActive+" ") + styles.Title.Render(item.Title)
	}
	b.WriteString("  " + severityMarker)
	b.WriteString("\n")

	var metaParts []string
	if item.ReportedBy != "" {
		metaParts = append(metaParts, "@"+item.ReportedBy)
	}
	if item.Created != "" {
		metaParts = append(metaParts, "opened "+item.Created)
	}
	if item.Priority != "" {
		metaParts = append(metaParts, "priority: "+item.Priority)
	}
	if item.isUrgent() {
		metaParts = append(metaParts, "severity: urgent")
	}
	if item.Source != "" {
		metaParts = append(metaParts, "source: "+item.Source)
	}
	if item.SourceRef != "" {
		metaParts = append(metaParts, "ref: "+item.SourceRef)
	}
	if item.LinkedSpec != "" {
		metaParts = append(metaParts, "linked: "+item.LinkedSpec)
	}
	if len(metaParts) > 0 {
		b.WriteString(styles.Muted.Render("  " + strings.Join(metaParts, " · ")))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	body := strings.TrimSpace(item.Body)
	if body == "" {
		b.WriteString(styles.Muted.Render("  (no description)"))
	} else {
		for _, line := range strings.Split(body, "\n") {
			b.WriteString("  " + styles.RowNormal.Render(line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	if len(item.Comments) > 0 {
		b.WriteString(styles.SectionTitle.Render("  History"))
		b.WriteString("\n")
		for _, c := range item.Comments {
			ts := relativeTime(c.At)
			entry := fmt.Sprintf("  └ @%-12s %s  %s",
				c.Actor,
				truncate(c.Message, p.width-30),
				styles.Muted.Render(ts),
			)
			b.WriteString(styles.RowNormal.Render(entry))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// buildHints returns the role-filtered action hint row.
func (p *triageDetailPane) buildHints(styles Styles, rc *config.ResolvedConfig) string {
	var hints []HintPair
	hints = append(hints, Hint("n", "note"))
	if roleAllowed(actionTriageEdit, rc) {
		hints = append(hints, Hint("e", "edit"))
	}
	if roleAllowed(actionTriageClose, rc) {
		hints = append(hints, Hint("c", "close"))
	}
	if roleAllowed(actionTriageEscalate, rc) {
		var escLabel string
		if p.item.isUrgent() {
			escLabel = "de-escalate"
		} else {
			escLabel = "escalate"
		}
		hints = append(hints, Hint("x", escLabel))
	}
	if roleAllowed(actionTriagePromote, rc) {
		hints = append(hints, Hint("p", "promote"))
	}
	hints = append(hints, Hint("esc", "back"))
	return HintStrip(styles, hints...)
}

// relativeTime returns a short human-readable label for an RFC3339 timestamp.
func relativeTime(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()))
	default:
		return fmt.Sprintf("%dd", int(diff.Hours()/24))
	}
}
