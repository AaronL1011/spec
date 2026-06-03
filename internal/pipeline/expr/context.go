package expr

import (
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

// Build returns the built context.
func (b *ContextBuilder) Build() Context {
	return b.ctx
}
