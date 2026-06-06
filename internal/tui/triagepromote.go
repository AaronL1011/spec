package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

		// Build the spec content and inject the triage body into §1.
		specContent := buildPromotedSpec(specID, item.Title, author, cycle, source, item.Body)

		err = gitpkg.WithSpecsRepoOpts(ctx, &rc.Team.SpecsRepo, tuiSyncOpts("triage/promote", specID), func(repoPath string) (string, error) {
			sd := filepath.Join(repoPath, gitpkg.SpecsSubDir)
			if err := os.MkdirAll(sd, 0o755); err != nil {
				return "", fmt.Errorf("creating specs dir: %w", err)
			}

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

// buildPromotedSpec scaffolds a spec and injects the triage body into §1 Problem
// Statement. If the body is blank the section is left empty (safe fallback).
func buildPromotedSpec(id, title, author, cycle, source, triageBody string) string {
	base := markdown.ScaffoldSpec(id, title, author, cycle, source)
	body := sanitiseBodyForSpec(strings.TrimSpace(triageBody))
	if body == "" {
		return base
	}
	// Insert the triage body beneath the "## 1. Problem Statement" heading.
	const marker = "## 1. Problem Statement           <!-- owner: pm -->"
	if !strings.Contains(base, marker) {
		// Fallback: heading has unexpected format — return unmodified.
		return base
	}
	replacement := marker + "\n\n" + body
	return strings.Replace(base, marker, replacement, 1)
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
