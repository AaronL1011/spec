package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// intakeFormState tracks the inline triage intake form.
type intakeFormState struct {
	active   bool
	field    int // 0=title, 1=priority, 2=source
	title    string
	priority string
	source   string
}

const (
	intakeFieldTitle    = 0
	intakeFieldPriority = 1
	intakeFieldSource   = 2
	intakeFieldCount    = 3
)

var priorities = []string{"low", "medium", "high", "critical"}

func (f *intakeFormState) open() {
	f.active = true
	f.field = intakeFieldTitle
	f.title = ""
	f.priority = "medium"
	f.source = ""
}

func (f *intakeFormState) close() {
	f.active = false
}

func (f *intakeFormState) nextField() {
	if f.field < intakeFieldCount-1 {
		f.field++
	}
}

func (f *intakeFormState) prevField() {
	if f.field > 0 {
		f.field--
	}
}

func (f *intakeFormState) cyclePriority() {
	for i, p := range priorities {
		if p == f.priority {
			f.priority = priorities[(i+1)%len(priorities)]
			return
		}
	}
	f.priority = "medium"
}

func (f *intakeFormState) appendToField(s string) {
	switch f.field {
	case intakeFieldTitle:
		f.title += s
	case intakeFieldSource:
		f.source += s
	}
}

func (f *intakeFormState) backspaceField() {
	switch f.field {
	case intakeFieldTitle:
		f.title = dropLastRune(f.title)
	case intakeFieldSource:
		f.source = dropLastRune(f.source)
	}
}

func (f intakeFormState) valid() bool {
	return f.title != ""
}

// createTriageItem commits a new triage item to the specs repo.
func createTriageItem(rc *config.ResolvedConfig, title, priority, source string) tea.Cmd {
	return func() tea.Msg {
		// Claim an authoritative triage ID before writing (SPEC-018). Offline is
		// a hard fail surfaced as an action error.
		triageFiles, _ := gitpkg.ListTriageFiles(&rc.Team.SpecsRepo)
		bootstrapMax := markdown.MaxTriageNum(triageFiles)
		triageID, claimErr := gitpkg.ClaimNextID(context.Background(), &rc.Team.SpecsRepo, gitpkg.CounterTriage, bootstrapMax)
		if claimErr != nil {
			return actionResultMsg{Action: "intake", Err: claimErr}
		}
		reportedBy := rc.UserName()

		content := markdown.ScaffoldTriageFromConfig(rc.SpecsRepoRoot(), tuiTemplateConfig(rc),
			markdown.TriageFields{ID: triageID, Title: title, Priority: priority, Source: source, SourceRef: "", ReportedBy: reportedBy, Date: time.Now().Format("2006-01-02")})

		err := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, tuiSyncOpts("intake", triageID), func(repoPath string) (string, error) {
			triageDir := filepath.Join(repoPath, gitpkg.SpecsSubDir, "triage")
			if err := os.MkdirAll(triageDir, 0o755); err != nil {
				return "", err
			}
			triagePath := filepath.Join(triageDir, triageID+".md")
			if err := os.WriteFile(triagePath, []byte(content), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("feat: intake %s — %s", triageID, title), nil
		})

		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "intake", Err: err}
		}
		return actionResultMsg{Action: "intake", SpecID: triageID, Detail: title + " (" + status + ")"}
	}
}
