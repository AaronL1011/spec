package expr

import (
	"strings"
	"time"
)

// ContextBuilder helps build an expression context from spec data.
type ContextBuilder struct {
	ctx Context
}

// NewContextBuilder creates a new context builder.
func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{
		ctx: NewContext(),
	}
}

// WithSpec sets spec metadata.
func (b *ContextBuilder) WithSpec(id, title, status string, labels []string, wordCount int, timeInStage time.Duration, revertCount int) *ContextBuilder {
	b.ctx.Spec = SpecContext{
		ID:          id,
		Title:       title,
		Status:      status,
		Labels:      labels,
		WordCount:   wordCount,
		TimeInStage: timeInStage,
		RevertCount: revertCount,
	}
	return b
}

// WithSection adds a section to the context.
func (b *ContextBuilder) WithSection(slug string, content string) *ContextBuilder {
	trimmed := strings.TrimSpace(content)
	b.ctx.Sections[slug] = SectionContext{
		Empty:     trimmed == "",
		WordCount: countWords(trimmed),
		LineCount: countLines(trimmed),
	}
	return b
}

// WithSections adds multiple sections to the context.
func (b *ContextBuilder) WithSections(sections map[string]string) *ContextBuilder {
	for slug, content := range sections {
		b.WithSection(slug, content)
	}
	return b
}

// WithDecisions sets decision log statistics.
func (b *ContextBuilder) WithDecisions(total, resolved int) *ContextBuilder {
	b.ctx.Decisions = DecisionsContext{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}
	return b
}

// WithAcceptanceCriteria sets AC statistics.
func (b *ContextBuilder) WithAcceptanceCriteria(count, checked int) *ContextBuilder {
	b.ctx.AcceptanceCriteria = ACContext{
		Items: ItemsContext{
			Count:     count,
			Checked:   checked,
			Unchecked: count - checked,
		},
	}
	return b
}

// WithPRStack sets PR stack statistics.
func (b *ContextBuilder) WithPRStack(exists bool, steps, completed int, allOpened, allApproved bool) *ContextBuilder {
	b.ctx.PRStack = PRStackContext{
		Exists:      exists,
		Steps:       steps,
		Completed:   completed,
		Pending:     steps - completed,
		AllOpened:   allOpened,
		AllApproved: allApproved,
	}
	return b
}

// WithPRs sets pull request statistics.
func (b *ContextBuilder) WithPRs(open, approved int, threadsResolved bool) *ContextBuilder {
	b.ctx.PRs = PRsContext{
		Open:            open,
		Approved:        approved,
		Pending:         open - approved,
		ThreadsResolved: threadsResolved,
	}
	return b
}

// WithDeploy sets deployment status.
func (b *ContextBuilder) WithDeploy(stagingStatus string, stagingHealthy bool, prodStatus string, prodHealthy bool) *ContextBuilder {
	b.ctx.Deploy = DeployContext{
		Staging:    EnvironmentStatus{Status: stagingStatus, Healthy: stagingHealthy},
		Production: EnvironmentStatus{Status: prodStatus, Healthy: prodHealthy},
	}
	return b
}

// WithAlerts sets alert statistics.
func (b *ContextBuilder) WithAlerts(count, critical, warning int) *ContextBuilder {
	b.ctx.Alerts = AlertsContext{
		Count:    count,
		Critical: critical,
		Warning:  warning,
	}
	return b
}

// Build returns the built context.
func (b *ContextBuilder) Build() Context {
	return b.ctx
}

// countWords counts words in a string.
func countWords(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Fields(s))
}

// countLines counts non-empty lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	lines := strings.Split(s, "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
