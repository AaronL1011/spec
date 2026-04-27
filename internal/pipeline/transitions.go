package pipeline

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
	"gopkg.in/yaml.v3"
)

// AdvanceResult contains the result of advancing a spec.
type AdvanceResult struct {
	PreviousStage string
	NewStage      string
	SkippedStages []string
}

// Advance updates the spec's status to the next (or target) stage.
func Advance(path string, meta *markdown.SpecMeta, target string) (*AdvanceResult, error) {
	result := &AdvanceResult{
		PreviousStage: meta.Status,
		NewStage:      target,
	}

	meta.Status = result.NewStage
	meta.Updated = time.Now().Format("2006-01-02")

	if err := markdown.WriteMeta(path, meta); err != nil {
		return nil, fmt.Errorf("writing updated status: %w", err)
	}

	return result, nil
}

// Revert sends a spec back to a previous stage.
func Revert(path string, meta *markdown.SpecMeta, targetStage, reason, user string) error {
	previousStage := meta.Status
	meta.Status = targetStage
	meta.RevertCount++
	meta.Updated = time.Now().Format("2006-01-02")

	if err := markdown.WriteMeta(path, meta); err != nil {
		return fmt.Errorf("writing reverted status: %w", err)
	}

	// Log the reversion reason to the decision log
	decisionText := fmt.Sprintf("REVERSION: %s → %s. Reason: %s", previousStage, targetStage, reason)
	if _, err := markdown.AppendDecision(path, decisionText, user); err != nil {
		// Non-fatal: log but don't fail the reversion
		fmt.Printf("Warning: could not log reversion to decision log: %v\n", err)
	}

	return nil
}

// EjectResult contains the result of ejecting a spec.
type EjectResult struct {
	PreviousStage string
}

// Eject transitions a spec to blocked status.
func Eject(path string, meta *markdown.SpecMeta, reason, user string) (*EjectResult, error) {
	result := &EjectResult{
		PreviousStage: meta.Status,
	}

	// Append to escape hatch log
	escapeEntry := fmt.Sprintf("\n- **%s** (%s): Blocked from `%s`. Reason: %s\n",
		time.Now().Format("2006-01-02"), user, meta.Status, reason)

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	body := markdown.Body(string(content))
	sections := markdown.ExtractSections(body)
	existingContent := ""
	if s := markdown.FindSection(sections, "escape_hatch_log"); s != nil {
		existingContent = s.Content
	}

	newContent, err := markdown.ReplaceSectionContent(string(content), "escape_hatch_log",
		existingContent+escapeEntry)
	if err != nil {
		// If escape hatch section doesn't exist, continue without it
		newContent = string(content)
	}

	meta.Status = StatusBlocked
	finalContent, err := replaceFrontmatterInContent(newContent, meta)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, []byte(finalContent), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}

	return result, nil
}

// Resume returns a blocked spec to its pre-block stage.
func Resume(path string, meta *markdown.SpecMeta, previousStage string) error {
	if meta.Status != StatusBlocked {
		return fmt.Errorf("spec is not blocked (status: %s) — 'spec resume' only works on blocked specs", meta.Status)
	}

	meta.Status = previousStage
	meta.Updated = time.Now().Format("2006-01-02")

	return markdown.WriteMeta(path, meta)
}

func replaceFrontmatterInContent(content string, meta *markdown.SpecMeta) (string, error) {
	data, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshalling frontmatter: %w", err)
	}

	if !strings.HasPrefix(content, "---") {
		return "", fmt.Errorf("no frontmatter to replace")
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", fmt.Errorf("no closing --- found")
	}
	body := rest[idx+4:]
	return "---\n" + string(data) + "---" + body, nil
}
