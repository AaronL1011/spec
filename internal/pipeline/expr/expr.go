// Package expr provides expression evaluation for pipeline gates.
package expr

import (
	"fmt"
	"time"

	"github.com/expr-lang/expr"
)

// Context is the environment available to gate expressions.
// All fields are exported and accessible in expressions.
type Context struct {
	// Spec contains spec metadata
	Spec SpecContext `expr:"spec"`

	// Sections contains per-section metadata keyed by slug
	Sections map[string]SectionContext `expr:"sections"`

	// Decisions contains decision log statistics
	Decisions DecisionsContext `expr:"decisions"`

	// AcceptanceCriteria contains AC statistics
	AcceptanceCriteria ACContext `expr:"acceptance_criteria"`

	// PRStack contains PR stack plan statistics
	PRStack PRStackContext `expr:"pr_stack"`

	// PRs contains pull request statistics
	PRs PRsContext `expr:"prs"`

	// Deploy contains deployment status
	Deploy DeployContext `expr:"deploy"`

	// Alerts contains alert statistics
	Alerts AlertsContext `expr:"alerts"`
}

// SpecContext contains spec metadata for expressions.
type SpecContext struct {
	ID          string        `expr:"id"`
	Title       string        `expr:"title"`
	Status      string        `expr:"status"`
	Labels      []string      `expr:"labels"`
	TimeInStage time.Duration `expr:"time_in_stage"`
	RevertCount int           `expr:"revert_count"`
	WordCount   int           `expr:"word_count"`
}

// SectionContext contains metadata about a spec section.
type SectionContext struct {
	Empty     bool `expr:"empty"`
	WordCount int  `expr:"word_count"`
	LineCount int  `expr:"line_count"`
}

// DecisionsContext contains decision log statistics.
type DecisionsContext struct {
	Total      int `expr:"total"`
	Resolved   int `expr:"resolved"`
	Unresolved int `expr:"unresolved"`
}

// ACContext contains acceptance criteria statistics.
type ACContext struct {
	Items ItemsContext `expr:"items"`
}

// ItemsContext contains item count information.
type ItemsContext struct {
	Count    int `expr:"count"`
	Checked  int `expr:"checked"`
	Unchecked int `expr:"unchecked"`
}

// PRStackContext contains PR stack plan statistics.
type PRStackContext struct {
	Exists      bool `expr:"exists"`
	Steps       int  `expr:"steps"`
	Completed   int  `expr:"completed"`
	Pending     int  `expr:"pending"`
	AllOpened   bool `expr:"all_opened"`
	AllApproved bool `expr:"all_approved"`
}

// PRsContext contains pull request statistics.
type PRsContext struct {
	Open            int  `expr:"open"`
	Approved        int  `expr:"approved"`
	Pending         int  `expr:"pending"`
	ThreadsResolved bool `expr:"threads_resolved"`
}

// DeployContext contains deployment status.
type DeployContext struct {
	Staging    EnvironmentStatus `expr:"staging"`
	Production EnvironmentStatus `expr:"production"`
}

// EnvironmentStatus contains status for a deployment environment.
type EnvironmentStatus struct {
	Status  string `expr:"status"` // pending, running, success, failed
	Healthy bool   `expr:"healthy"`
}

// AlertsContext contains alert statistics.
type AlertsContext struct {
	Count    int `expr:"count"`
	Critical int `expr:"critical"`
	Warning  int `expr:"warning"`
}

// Evaluate compiles and runs an expression against the context.
// Returns true if the expression evaluates to true, false otherwise.
func Evaluate(expression string, ctx Context) (bool, error) {
	program, err := expr.Compile(expression, expr.Env(ctx), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("compiling expression: %w", err)
	}

	result, err := expr.Run(program, ctx)
	if err != nil {
		return false, fmt.Errorf("evaluating expression: %w", err)
	}

	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression must return boolean, got %T", result)
	}

	return b, nil
}

// Compile validates an expression at config load time.
// Returns an error if the expression is invalid.
func Compile(expression string) error {
	// Use an empty context for type checking
	ctx := Context{
		Sections: make(map[string]SectionContext),
	}
	_, err := expr.Compile(expression, expr.Env(ctx), expr.AsBool())
	if err != nil {
		return fmt.Errorf("invalid expression: %w", err)
	}
	return nil
}

// CompileWithContext validates an expression with a specific context.
func CompileWithContext(expression string, ctx Context) error {
	_, err := expr.Compile(expression, expr.Env(ctx), expr.AsBool())
	if err != nil {
		return fmt.Errorf("invalid expression: %w", err)
	}
	return nil
}

// NewContext creates a new empty context with initialized maps.
func NewContext() Context {
	return Context{
		Sections: make(map[string]SectionContext),
	}
}



func init() {
	// Register custom functions available in expressions
	// The expr library automatically handles "contains" for slices
}
