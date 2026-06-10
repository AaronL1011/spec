// Package markdown provides parsing and mutation for SPEC.md files.
// It operates on line-level patterns, not a full AST — sufficient for the
// structured SPEC.md format.
package markdown

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SpecMeta represents the YAML frontmatter of a SPEC.md file.
type SpecMeta struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Status      string   `yaml:"status"`
	Version     string   `yaml:"version"`
	Author      string   `yaml:"author"`
	Cycle       string   `yaml:"cycle"`
	EpicKey     string   `yaml:"epic_key,omitempty"`
	Repos       []string `yaml:"repos,omitempty"`
	RevertCount int      `yaml:"revert_count"`
	Source      string   `yaml:"source,omitempty"`
	Created     string   `yaml:"created"`
	Updated     string   `yaml:"updated"`

	// Steps is the structured build plan.
	// Replaces unstructured §7.3 prose with authoritative step tracking.
	Steps []BuildStep `yaml:"steps,omitempty"`

	// Review tracks plan review state for the engineering stage.
	Review *ReviewState `yaml:"review,omitempty"`

	// FastTrack marks this as a fast-track bug fix (skips ceremony stages).
	FastTrack bool `yaml:"fast_track,omitempty"`
}

// BuildStep represents a single step in the build plan.
// Steps are a checklist of work items, not git-level PR stacking.
type BuildStep struct {
	// Repo is the repository this step targets.
	Repo string `yaml:"repo"`

	// Description is a brief summary of what this step accomplishes.
	Description string `yaml:"description"`

	// Branch is the git branch name for this step (auto-generated if empty).
	Branch string `yaml:"branch,omitempty"`

	// PR is the pull request number once created.
	PR int `yaml:"pr,omitempty"`

	// StoryKey is the linked PM story key for this step (e.g. "PLAT-201"),
	// set when story sync is enabled so board analytics track build progress.
	StoryKey string `yaml:"story_key,omitempty"`

	// Status tracks the step's progress: pending, in-progress, complete, blocked.
	Status string `yaml:"status"`

	// BlockedReason explains why this step is blocked (when Status == "blocked").
	BlockedReason string `yaml:"blocked_reason,omitempty"`
}

// Step status constants.
const (
	StepStatusPending    = "pending"
	StepStatusInProgress = "in-progress"
	StepStatusComplete   = "complete"
	StepStatusBlocked    = "blocked"
)

// ReviewState tracks plan review status for async approval workflow.
type ReviewState struct {
	// RequestedAt is when the review was requested.
	RequestedAt string `yaml:"requested_at,omitempty"`

	// Reviewers is the list of users/roles who should review.
	Reviewers []string `yaml:"reviewers,omitempty"`

	// Approvals records who has approved and when.
	Approvals []ReviewApproval `yaml:"approvals,omitempty"`

	// Status is the overall review state: pending, approved, changes_requested.
	Status string `yaml:"status"`

	// Feedback contains reviewer feedback when changes are requested.
	Feedback string `yaml:"feedback,omitempty"`
}

// Review status constants.
const (
	ReviewStatusPending          = "pending"
	ReviewStatusApproved         = "approved"
	ReviewStatusChangesRequested = "changes_requested"
)

// ReviewApproval records a single reviewer's approval.
type ReviewApproval struct {
	Reviewer   string `yaml:"reviewer"`
	ApprovedAt string `yaml:"approved_at"`
}

// CurrentStep returns the index (0-based) of the first non-complete step,
// or -1 if all steps are complete or there are no steps.
func (m *SpecMeta) CurrentStep() int {
	for i, step := range m.Steps {
		if step.Status != StepStatusComplete {
			return i
		}
	}
	return -1
}

// AllStepsComplete returns true if all steps have status "complete".
func (m *SpecMeta) AllStepsComplete() bool {
	if len(m.Steps) == 0 {
		return false
	}
	for _, step := range m.Steps {
		if step.Status != StepStatusComplete {
			return false
		}
	}
	return true
}

// StepsExist returns true if at least one step is defined.
func (m *SpecMeta) StepsExist() bool {
	return len(m.Steps) > 0
}

// IsReviewApproved returns true if the review status is "approved".
func (m *SpecMeta) IsReviewApproved() bool {
	return m.Review != nil && m.Review.Status == ReviewStatusApproved
}

// IsReviewPending returns true if review was requested but not yet completed.
func (m *SpecMeta) IsReviewPending() bool {
	return m.Review != nil && m.Review.Status == ReviewStatusPending
}

// IsReviewChangesRequested returns true if reviewer requested changes.
func (m *SpecMeta) IsReviewChangesRequested() bool {
	return m.Review != nil && m.Review.Status == ReviewStatusChangesRequested
}

// TriageMeta represents the YAML frontmatter of a TRIAGE.md file.
type TriageMeta struct {
	ID         string          `yaml:"id"`
	Title      string          `yaml:"title"`
	Status     string          `yaml:"status"`
	Priority   string          `yaml:"priority"`
	Severity   string          `yaml:"severity,omitempty"`    // "urgent" or "normal" (default normal)
	LinkedSpec string          `yaml:"linked_spec,omitempty"` // SPEC-NNN if manually linked
	Source     string          `yaml:"source,omitempty"`
	SourceRef  string          `yaml:"source_ref,omitempty"`
	ReportedBy string          `yaml:"reported_by,omitempty"`
	Created    string          `yaml:"created"`
	ResolvedAt string          `yaml:"resolved_at,omitempty"` // set when closed/archived
	Comments   []TriageComment `yaml:"comments,omitempty"`    // append-only history log
}

// TriageComment is a single immutable entry in a triage item's history log.
type TriageComment struct {
	Actor   string `yaml:"actor"`
	Message string `yaml:"message"`
	At      string `yaml:"at"` // RFC3339 timestamp
}

// ReadMeta reads and parses the YAML frontmatter from a markdown file.
func ReadMeta(path string) (*SpecMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ParseMeta(string(data))
}

// ParseMeta parses YAML frontmatter from markdown content.
func ParseMeta(content string) (*SpecMeta, error) {
	fm, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}
	var meta SpecMeta
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}
	return &meta, nil
}

// ReadTriageMeta reads and parses triage frontmatter.
func ReadTriageMeta(path string) (*TriageMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ParseTriageMeta(string(data))
}

// ParseTriageMeta parses triage YAML frontmatter.
func ParseTriageMeta(content string) (*TriageMeta, error) {
	fm, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}
	var meta TriageMeta
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, fmt.Errorf("parsing triage frontmatter: %w", err)
	}
	return &meta, nil
}

// WriteTriageMeta updates triage frontmatter in a file, preserving the body.
func WriteTriageMeta(path string, meta *TriageMeta) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	newContent, err := replaceFrontmatter(string(data), meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(newContent), 0o644)
}

// AppendTriageComment appends a new comment to a triage file's history log and
// persists it in place. It is additive-only; existing entries are never touched.
func AppendTriageComment(path, actor, message string) error {
	meta, err := ReadTriageMeta(path)
	if err != nil {
		return fmt.Errorf("reading triage meta: %w", err)
	}
	meta.Comments = append(meta.Comments, TriageComment{
		Actor:   actor,
		Message: message,
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	return WriteTriageMeta(path, meta)
}

// UpdateTriageFields updates the mutable fields of a triage file and replaces
// the body, while preserving existing comments and immutable metadata.
func UpdateTriageFields(path, title, priority, source, body string) error {
	meta, err := ReadTriageMeta(path)
	if err != nil {
		return fmt.Errorf("reading triage: %w", err)
	}
	meta.Title = title
	meta.Priority = priority
	meta.Source = source
	if err := WriteTriageMeta(path, meta); err != nil {
		return fmt.Errorf("writing triage meta: %w", err)
	}
	return replaceTriageBody(path, body)
}

// ArchiveTriageItem sets a triage item's status to "archived", records the
// resolution timestamp, and appends a closing history entry.
func ArchiveTriageItem(path, reason, note, actor string) error {
	meta, err := ReadTriageMeta(path)
	if err != nil {
		return fmt.Errorf("reading triage: %w", err)
	}
	meta.Status = "archived"
	meta.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
	msg := reason
	if note != "" {
		msg += ": " + note
	}
	meta.Comments = append(meta.Comments, TriageComment{
		Actor:   actor,
		Message: msg,
		At:      meta.ResolvedAt,
	})
	if err := WriteTriageMeta(path, meta); err != nil {
		return fmt.Errorf("writing triage meta: %w", err)
	}
	return nil
}

// EscalateTriageItem toggles severity between "urgent" and "" (normal) and
// appends a history entry recording the escalation event.
func EscalateTriageItem(path string, currentlyUrgent bool, actor string) error {
	meta, err := ReadTriageMeta(path)
	if err != nil {
		return fmt.Errorf("reading triage: %w", err)
	}
	verb := "escalated to urgent"
	if currentlyUrgent {
		meta.Severity = ""
		verb = "de-escalated to normal"
	} else {
		meta.Severity = "urgent"
	}
	meta.Comments = append(meta.Comments, TriageComment{
		Actor:   actor,
		Message: verb,
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	if err := WriteTriageMeta(path, meta); err != nil {
		return fmt.Errorf("writing triage meta: %w", err)
	}
	return nil
}

// replaceTriageBody replaces the markdown body of a file (everything after the
// frontmatter) while preserving the frontmatter block intact.
func replaceTriageBody(path, body string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(data)
	fmBlock := extractFrontmatterBlock(content)
	newContent := fmBlock + "\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(newContent), 0o644)
}

// extractFrontmatterBlock returns the raw "---\n...\n---\n" block from content.
func extractFrontmatterBlock(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	return "---" + rest[:idx] + "\n---\n"
}

// WriteMeta updates the frontmatter in a file, preserving the body content.
func WriteMeta(path string, meta *SpecMeta) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	meta.Updated = time.Now().Format("2006-01-02")

	newContent, err := replaceFrontmatter(string(data), meta)
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(newContent), 0o644)
}

// UpdateFrontmatter returns content with updated frontmatter, preserving the body.
// It also updates the 'updated' field to today's date.
func UpdateFrontmatter(content string, meta *SpecMeta) (string, error) {
	meta.Updated = time.Now().Format("2006-01-02")
	return replaceFrontmatter(content, meta)
}

// extractFrontmatter returns the YAML content between --- delimiters.
func extractFrontmatter(content string) (string, error) {
	if !strings.HasPrefix(content, "---") {
		return "", fmt.Errorf("no frontmatter found: file does not start with ---")
	}

	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", fmt.Errorf("no closing --- found for frontmatter")
	}

	return strings.TrimSpace(rest[:idx]), nil
}

// replaceFrontmatter replaces the YAML frontmatter in content.
func replaceFrontmatter(content string, meta interface{}) (string, error) {
	yamlBytes, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshalling frontmatter: %w", err)
	}

	// Find the end of existing frontmatter
	if !strings.HasPrefix(content, "---") {
		return "", fmt.Errorf("no frontmatter to replace")
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", fmt.Errorf("no closing --- found")
	}

	body := rest[idx+4:] // skip \n---
	return "---\n" + string(yamlBytes) + "---" + body, nil
}

// Body returns the content after the frontmatter.
func Body(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	body := rest[idx+4:]
	return strings.TrimLeft(body, "\n")
}
