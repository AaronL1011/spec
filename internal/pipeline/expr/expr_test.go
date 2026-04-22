package expr

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		ctx     Context
		want    bool
		wantErr bool
	}{
		{
			name: "decisions unresolved equals zero",
			expr: "decisions.unresolved == 0",
			ctx: Context{
				Decisions: DecisionsContext{Total: 5, Resolved: 5, Unresolved: 0},
			},
			want: true,
		},
		{
			name: "decisions unresolved not zero",
			expr: "decisions.unresolved == 0",
			ctx: Context{
				Decisions: DecisionsContext{Total: 5, Resolved: 3, Unresolved: 2},
			},
			want: false,
		},
		{
			name: "acceptance criteria count",
			expr: "acceptance_criteria.items.count >= 3",
			ctx: Context{
				AcceptanceCriteria: ACContext{Items: ItemsContext{Count: 5}},
			},
			want: true,
		},
		{
			name: "acceptance criteria count insufficient",
			expr: "acceptance_criteria.items.count >= 3",
			ctx: Context{
				AcceptanceCriteria: ACContext{Items: ItemsContext{Count: 2}},
			},
			want: false,
		},
		{
			name: "pr stack exists",
			expr: "pr_stack.exists",
			ctx: Context{
				PRStack: PRStackContext{Exists: true, Steps: 3},
			},
			want: true,
		},
		{
			name: "all prs approved",
			expr: "prs.approved == prs.open",
			ctx: Context{
				PRs: PRsContext{Open: 3, Approved: 3},
			},
			want: true,
		},
		{
			name: "section not empty",
			expr: "sections.problem_statement.empty == false",
			ctx: Context{
				Sections: map[string]SectionContext{
					"problem_statement": {Empty: false, WordCount: 100},
				},
			},
			want: true,
		},
		{
			name: "section word count",
			expr: "sections.technical_implementation.word_count > 50",
			ctx: Context{
				Sections: map[string]SectionContext{
					"technical_implementation": {Empty: false, WordCount: 150},
				},
			},
			want: true,
		},
		{
			name: "complex and condition",
			expr: "decisions.unresolved == 0 && pr_stack.exists",
			ctx: Context{
				Decisions: DecisionsContext{Unresolved: 0},
				PRStack:   PRStackContext{Exists: true},
			},
			want: true,
		},
		{
			name: "complex or condition",
			expr: "prs.approved >= 1 || 'skip-review' in spec.labels",
			ctx: Context{
				PRs:  PRsContext{Approved: 0},
				Spec: SpecContext{Labels: []string{"skip-review"}},
			},
			want: true,
		},
		{
			name: "labels contains",
			expr: "'urgent' in spec.labels",
			ctx: Context{
				Spec: SpecContext{Labels: []string{"bug", "urgent"}},
			},
			want: true,
		},
		{
			name: "labels not contains",
			expr: "'urgent' in spec.labels",
			ctx: Context{
				Spec: SpecContext{Labels: []string{"feature"}},
			},
			want: false,
		},
		{
			name: "time in stage comparison",
			expr: "spec.time_in_stage > duration('24h')",
			ctx: Context{
				Spec: SpecContext{TimeInStage: 48 * time.Hour},
			},
			want: true,
		},
		{
			name: "deploy staging healthy",
			expr: "deploy.staging.healthy",
			ctx: Context{
				Deploy: DeployContext{
					Staging: EnvironmentStatus{Status: "success", Healthy: true},
				},
			},
			want: true,
		},
		{
			name: "alerts count zero",
			expr: "alerts.count == 0",
			ctx: Context{
				Alerts: AlertsContext{Count: 0},
			},
			want: true,
		},
		{
			name: "revert count check",
			expr: "spec.revert_count < 3",
			ctx: Context{
				Spec: SpecContext{RevertCount: 1},
			},
			want: true,
		},
		{
			name: "invalid expression",
			expr: "this.is.not.valid ++ 123",
			ctx:  NewContext(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize sections map if nil
			if tt.ctx.Sections == nil {
				tt.ctx.Sections = make(map[string]SectionContext)
			}

			got, err := Evaluate(tt.expr, tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompile(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "valid simple expression",
			expr:    "decisions.unresolved == 0",
			wantErr: false,
		},
		{
			name:    "valid complex expression",
			expr:    "decisions.unresolved == 0 && pr_stack.exists",
			wantErr: false,
		},
		{
			name:    "valid contains expression",
			expr:    "'bug' in spec.labels",
			wantErr: false,
		},
		{
			name:    "invalid syntax",
			expr:    "this is not valid",
			wantErr: true,
		},
		{
			name:    "invalid operator",
			expr:    "decisions.unresolved === 0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Compile(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Compile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	if ctx.Sections == nil {
		t.Error("NewContext() should initialize Sections map")
	}
}
