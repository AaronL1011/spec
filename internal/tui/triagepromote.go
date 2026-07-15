package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
)

// triagePromoteResultMsg carries the outcome of a promote action.
type triagePromoteResultMsg struct {
	TriageID string
	SpecID   string
	Err      error
}

// promoteTriageItem claims a SPEC-NNN, scaffolds the spec, pre-populates §1
// from the triage body, deletes the triage file, and commits all in one
// WithSpecsRepo call so the operation is atomic (AC-7).
func promoteTriageItem(rc *config.ResolvedConfig, item triageItem) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Claim the next authoritative SPEC ID (SPEC-018).
		specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
		archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
		bootstrapMax := markdown.MaxSpecNum(append(append([]string{}, specFiles...), archiveFiles...))
		specID, err := gitpkg.ClaimNextID(ctx, &rc.Team.SpecsRepo, gitpkg.CounterSpec, bootstrapMax)
		if err != nil {
			return triagePromoteResultMsg{TriageID: item.ID, Err: fmt.Errorf("claiming spec ID: %w", err)}
		}

		author := gitpkg.UserName(ctx)
		cycle := rc.CycleLabel()
		source := item.ID

		err = gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/promote", specID), func(repoPath string) (string, error) {
			sd := filepath.Join(repoPath, gitpkg.SpecsSubDir)
			if err := os.MkdirAll(sd, 0o755); err != nil {
				return "", fmt.Errorf("creating specs dir: %w", err)
			}

			// Build the spec content and inject the triage body into the
			// Problem Statement section. Resolved inside the sync wrapper so
			// the spec scaffolds from the just-pulled (latest) team template.
			specContent := buildPromotedSpec(repoPath, tuiTemplateConfig(rc), specID, item.Title, author, cycle, source, item.Body)

			// Write the new spec file.
			specPath := filepath.Join(sd, specID+".md")
			if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
				return "", fmt.Errorf("writing spec: %w", err)
			}

			// Delete the triage file in the same commit (atomicity, AC-7).
			triagePath := filepath.Join(sd, "triage", item.ID+".md")
			if err := os.Remove(triagePath); err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("removing triage file: %w", err)
			}

			return fmt.Sprintf("feat: promote %s to %s — %s", item.ID, specID, item.Title), nil
		})
		if err != nil {
			return triagePromoteResultMsg{TriageID: item.ID, SpecID: specID, Err: err}
		}

		return triagePromoteResultMsg{TriageID: item.ID, SpecID: specID}
	}
}

// buildPromotedSpec scaffolds a spec from the effective template at repoDir
// and injects the triage body into the problem_statement section. The section
// is located by slug, not by literal heading text, so a custom team template
// with different numbering, spacing, or owner markers still receives the
// body. If the body is blank — or the template genuinely lacks the section —
// the scaffold is returned unmodified (safe fallback, never corrupts).
func buildPromotedSpec(repoDir string, tc markdown.TemplateConfig, id, title, author, cycle, source, triageBody string) string {
	base := markdown.ScaffoldSpecFromConfig(repoDir, tc,
		markdown.SpecFields{ID: id, Title: title, Author: author, Cycle: cycle, Source: source, Date: time.Now().Format("2006-01-02")})
	body := sanitiseBodyForSpec(strings.TrimSpace(triageBody))
	if body == "" {
		return base
	}
	out, err := markdown.ReplaceSectionContent(base, "problem_statement", "\n"+body+"\n")
	if err != nil {
		// Template lacks a problem_statement section (only possible when the
		// resolved template drifted mid-operation) — keep the scaffold intact.
		return base
	}
	return out
}

// sanitiseBodyForSpec strips or escapes content that would collide with the
// spec skeleton (level-2 headings). Any line beginning with "## " is demoted
// to a bold label so it does not create a spurious section.
func sanitiseBodyForSpec(body string) string {
	if body == "" {
		return ""
	}
	var out strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			// Demote to a bold paragraph label to avoid section collision.
			out.WriteString("**" + strings.TrimPrefix(line, "## ") + "**")
		} else {
			out.WriteString(line)
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

// editTriageItem updates the mutable fields of a triage file and commits.
func editTriageItem(rc *config.ResolvedConfig, triageID, title, priority, source, body string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/edit", triageID), func(repoPath string) (string, error) {
			path := filepath.Join(repoPath, gitpkg.SpecsSubDir, "triage", triageID+".md")
			if err := markdown.UpdateTriageFields(path, title, priority, source, body); err != nil {
				return "", err
			}
			return fmt.Sprintf("feat: edit %s — %s", triageID, title), nil
		})
		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "triage/edit", SpecID: triageID, Err: err}
		}
		return actionResultMsg{Action: "triage/edit", SpecID: triageID, Detail: status}
	}
}

// closeTriageItem archives a triage item with a resolution reason.
func closeTriageItem(rc *config.ResolvedConfig, triageID, reason, note, actor string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/close", triageID), func(repoPath string) (string, error) {
			path := filepath.Join(repoPath, gitpkg.SpecsSubDir, "triage", triageID+".md")
			if err := markdown.ArchiveTriageItem(path, reason, note, actor); err != nil {
				return "", err
			}
			return fmt.Sprintf("feat: close %s — %s", triageID, reason), nil
		})
		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "triage/close", SpecID: triageID, Err: err}
		}
		return actionResultMsg{Action: "triage/close", SpecID: triageID, Detail: reason + " (" + status + ")"}
	}
}

// escalateTriageItem toggles the severity of a triage item between urgent and normal,
// and appends a history entry recording the escalation event.
func escalateTriageItem(rc *config.ResolvedConfig, triageID string, currentlyUrgent bool, actor string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		verb := "escalated"
		if currentlyUrgent {
			verb = "de-escalated"
		}
		err := gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/escalate", triageID), func(repoPath string) (string, error) {
			path := filepath.Join(repoPath, gitpkg.SpecsSubDir, "triage", triageID+".md")
			if err := markdown.EscalateTriageItem(path, currentlyUrgent, actor); err != nil {
				return "", err
			}
			return fmt.Sprintf("feat: %s %s", verb, triageID), nil
		})
		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "triage/escalate", SpecID: triageID, Err: err}
		}
		return actionResultMsg{Action: "triage/escalate", SpecID: triageID, Detail: verb + " (" + status + ")"}
	}
}

// commentTriageItem appends an immutable note to a triage item's history.
func commentTriageItem(rc *config.ResolvedConfig, triageID, note, actor string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/comment", triageID), func(repoPath string) (string, error) {
			path := filepath.Join(repoPath, gitpkg.SpecsSubDir, "triage", triageID+".md")
			if err := markdown.AppendTriageComment(path, actor, note); err != nil {
				return "", fmt.Errorf("appending comment: %w", err)
			}
			return fmt.Sprintf("feat: comment on %s", triageID), nil
		})
		status, fatal := pushOutcome(err)
		if fatal {
			return actionResultMsg{Action: "triage/comment", SpecID: triageID, Err: err}
		}
		return actionResultMsg{Action: "triage/comment", SpecID: triageID, Detail: status}
	}
}
