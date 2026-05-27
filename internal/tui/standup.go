package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/store"
)

// standupDataMsg carries the generated standup text.
type standupDataMsg struct {
	Text string
	Err  error
}

// standupOverlay shows the standup in a modal-like overlay.
type standupOverlay struct {
	visible bool
	text    string
	scroll  int
	width   int
	height  int
	styles  Styles
}

func (s *standupOverlay) show(text string) {
	s.text = text
	s.visible = true
	s.scroll = 0
}

func (s *standupOverlay) hide()            { s.visible = false }
func (s *standupOverlay) setSize(w, h int) { s.width = w; s.height = h }

func (s *standupOverlay) scrollUp() {
	if s.scroll > 0 {
		s.scroll--
	}
}

func (s *standupOverlay) scrollDown() {
	s.scroll++
	// Clamp to prevent scrolling past content.
	lines := len(splitLines(s.text)) + 5 // text + header/footer chrome
	visible := s.height
	if visible < 3 {
		visible = 3
	}
	if mx := lines - visible; mx > 0 {
		if s.scroll > mx {
			s.scroll = mx
		}
	} else {
		s.scroll = 0
	}
}

func (s standupOverlay) view() string {
	if !s.visible || s.text == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(s.styles.Title.Render("  Standup"))
	b.WriteString("\n")
	b.WriteString(s.styles.Separator.Render(strings.Repeat("─", s.width-4)))
	b.WriteString("\n")
	b.WriteString(s.text)
	b.WriteString("\n\n")
	b.WriteString(s.styles.Muted.Render("  c copy · esc close · j/k scroll"))

	lines := splitLines(b.String())

	visible := s.height
	if visible < 3 {
		visible = 3
	}
	start := s.scroll
	if start > len(lines)-visible {
		start = max(0, len(lines)-visible)
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

// generateStandup produces standup text from activity and spec state.
func generateStandup(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB) tea.Cmd {
	return func() tea.Msg {
		text, err := buildStandupText(rc, reg, db)
		return standupDataMsg{Text: text, Err: err}
	}
}

func buildStandupText(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB) (string, error) {
	if db == nil {
		opened, err := store.Open(store.DefaultDBPath())
		if err != nil {
			return "", err
		}
		defer func() { _ = opened.Close() }()
		db = opened
	}

	since := time.Now().Add(-24 * time.Hour)
	entries, err := db.ActivitySince(since)
	if err != nil {
		return "", err
	}

	userName := rc.UserName()
	userRole := rc.OwnerRole("")
	date := time.Now().Format("2006-01-02")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s — %s\n\n", userName, date))

	// Yesterday
	b.WriteString("  Yesterday:\n")
	if len(entries) == 0 {
		b.WriteString("    (no tracked activity)\n")
	} else {
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("    • %s: %s\n", e.SpecID, e.Summary))
		}
	}

	// Today
	b.WriteString("\n  Today:\n")
	todayItems := 0

	recent, _ := db.SessionMostRecent()
	if recent != "" {
		b.WriteString(fmt.Sprintf("    • Continue %s\n", recent))
		todayItems++
	}

	owned := standupOwnedSpecs(rc.SpecsRepoDir, userRole, rc.Pipeline())
	for _, s := range owned {
		b.WriteString(fmt.Sprintf("    • %s: %s [%s]\n", s.id, s.title, s.stage))
		todayItems++
	}

	if todayItems == 0 {
		b.WriteString("    (run 'spec do' to start)\n")
	}

	// Blockers
	b.WriteString("\n  Blockers:\n")
	blockers := standupBlockers(db)
	if len(blockers) == 0 {
		b.WriteString("    (none)\n")
	} else {
		for _, bl := range blockers {
			b.WriteString(fmt.Sprintf("    • %s\n", bl))
		}
	}

	return b.String(), nil
}

type standupSpec struct {
	id    string
	title string
	stage string
}

func standupOwnedSpecs(specsDir, role string, pl config.PipelineConfig) []standupSpec {
	if role == "" || specsDir == "" {
		return nil
	}

	terminals := pipeline.TerminalStages(pl)
	terminalSet := make(map[string]bool, len(terminals))
	for _, s := range terminals {
		terminalSet[s] = true
	}

	dirEntries, err := os.ReadDir(specsDir)
	if err != nil {
		return nil
	}

	var owned []standupSpec
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(specsDir, e.Name()))
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		if terminalSet[meta.Status] {
			continue
		}
		stage := pl.StageByName(meta.Status)
		if stage != nil && stage.HasOwner(role) {
			owned = append(owned, standupSpec{
				id:    meta.ID,
				title: meta.Title,
				stage: meta.Status,
			})
		}
	}
	return owned
}

func standupBlockers(db *store.DB) []string {
	entries, err := db.ActivitySince(time.Now().Add(-7 * 24 * time.Hour))
	if err != nil {
		return nil
	}
	var blockers []string
	for _, e := range entries {
		if e.EventType == "eject" || e.EventType == "block" {
			blockers = append(blockers, fmt.Sprintf("%s: %s", e.SpecID, e.Summary))
		}
	}
	return blockers
}
