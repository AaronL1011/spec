package hierarchy

import (
	"errors"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name         string
		refs         []SpecRef
		id           string
		wantRules    []string
		wantErrors   bool
		wantContains string
	}{
		{
			name: "healthy two-level tree is silent",
			refs: twoLevelTree(),
			id:   "SPEC-009",
		},
		{
			name: "initiative with live children is silent",
			refs: twoLevelTree(),
			id:   "SPEC-004",
		},
		{
			name:         "dangling parent is an error",
			refs:         []SpecRef{{ID: "SPEC-009", Parent: "SPEC-999"}},
			id:           "SPEC-009",
			wantRules:    []string{RuleParentExists},
			wantErrors:   true,
			wantContains: "SPEC-999",
		},
		{
			name:       "self-parent is an error",
			refs:       []SpecRef{{ID: "SPEC-009", Parent: "SPEC-009"}},
			id:         "SPEC-009",
			wantRules:  []string{RuleSelfParent},
			wantErrors: true,
		},
		{
			name: "depth three is a warning",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
				{ID: "SPEC-010", Status: "build", Parent: "SPEC-009"},
			},
			id:        "SPEC-010",
			wantRules: []string{RuleParentDepth},
		},
		{
			name: "a slice that is also an initiative warns on both sides",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
				{ID: "SPEC-010", Status: "build", Parent: "SPEC-009"},
			},
			id:        "SPEC-009",
			wantRules: []string{RuleChildHasChildren},
		},
		{
			name: "terminal parent is a warning",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "done"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
			},
			id:        "SPEC-009",
			wantRules: []string{RuleParentTerminal},
		},
		{
			name: "archived parent is a warning, not a terminal one",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "closed", Archived: true},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
			},
			id:        "SPEC-009",
			wantRules: []string{RuleParentArchived},
		},
		{
			name: "child reopened after the initiative closed",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "done"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
			},
			id:           "SPEC-004",
			wantRules:    []string{RuleChildReopened},
			wantContains: "SPEC-009 reopened to build",
		},
		{
			name: "closed initiative with all children terminal is silent",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "done"},
				{ID: "SPEC-009", Status: "done", Parent: "SPEC-004"},
			},
			id: "SPEC-004",
		},
		{
			name: "unknown spec yields no findings",
			refs: twoLevelTree(),
			id:   "SPEC-999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := Check(Build(tt.refs), tt.id, testPipeline())
			var rules []string
			var joined string
			for _, f := range findings {
				rules = append(rules, f.Rule)
				joined += f.Message + "\n"
			}
			if len(rules) != len(tt.wantRules) {
				t.Fatalf("rules = %v, want %v (messages: %s)", rules, tt.wantRules, joined)
			}
			for i, want := range tt.wantRules {
				if rules[i] != want {
					t.Errorf("rule[%d] = %q, want %q", i, rules[i], want)
				}
			}
			if HasErrors(findings) != tt.wantErrors {
				t.Errorf("HasErrors = %v, want %v", HasErrors(findings), tt.wantErrors)
			}
			if tt.wantContains != "" && !strings.Contains(joined, tt.wantContains) {
				t.Errorf("messages %q do not mention %q", joined, tt.wantContains)
			}
		})
	}
}

func TestCheck_MessagesNameTheNextAction(t *testing.T) {
	findings := Check(Build([]SpecRef{{ID: "SPEC-009", Parent: "SPEC-999"}}), "SPEC-009", testPipeline())
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if !strings.Contains(findings[0].Message, "spec link") {
		t.Errorf("error message %q must name the fix command", findings[0].Message)
	}
}

func TestLink(t *testing.T) {
	tests := []struct {
		name    string
		refs    []SpecRef
		child   string
		parent  string
		wantErr error
	}{
		{
			name:   "attaches a standalone spec to a live initiative",
			refs:   twoLevelTree(),
			child:  "SPEC-014",
			parent: "SPEC-004",
		},
		{
			name:   "detaching is always permitted",
			refs:   twoLevelTree(),
			child:  "SPEC-009",
			parent: "",
		},
		{
			name: "detaching from a terminal parent is still permitted",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "done"},
				{ID: "SPEC-009", Status: "build", Parent: "SPEC-004"},
			},
			child:  "SPEC-009",
			parent: "",
		},
		{
			name:    "unknown spec",
			refs:    twoLevelTree(),
			child:   "SPEC-999",
			parent:  "SPEC-004",
			wantErr: ErrSpecNotFound,
		},
		{
			name:    "unknown parent",
			refs:    twoLevelTree(),
			child:   "SPEC-014",
			parent:  "SPEC-999",
			wantErr: ErrParentNotFound,
		},
		{
			name:    "self parent",
			refs:    twoLevelTree(),
			child:   "SPEC-014",
			parent:  "SPEC-014",
			wantErr: ErrSelfParent,
		},
		{
			name:    "depth three attempt",
			refs:    twoLevelTree(),
			child:   "SPEC-014",
			parent:  "SPEC-009",
			wantErr: ErrDepthExceeded,
		},
		{
			name:    "a spec with children cannot gain a parent",
			refs:    twoLevelTree(),
			child:   "SPEC-004",
			parent:  "SPEC-014",
			wantErr: ErrChildHasChildren,
		},
		{
			name: "terminal parent",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "done"},
				{ID: "SPEC-014", Status: "draft"},
			},
			child:   "SPEC-014",
			parent:  "SPEC-004",
			wantErr: ErrParentTerminal,
		},
		{
			name: "archived parent",
			refs: []SpecRef{
				{ID: "SPEC-004", Status: "build", Archived: true},
				{ID: "SPEC-014", Status: "draft"},
			},
			child:   "SPEC-014",
			parent:  "SPEC-004",
			wantErr: ErrParentArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Link(Build(tt.refs), tt.child, tt.parent, testPipeline())
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Link = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Link = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLink_TerminalParentNamesTheEscape(t *testing.T) {
	err := Link(Build([]SpecRef{
		{ID: "SPEC-004", Status: "done"},
		{ID: "SPEC-014", Status: "draft"},
	}), "SPEC-014", "SPEC-004", testPipeline())
	if err == nil || !strings.Contains(err.Error(), "spec revert SPEC-004") {
		t.Errorf("refusal %v must name 'spec revert' as the escape", err)
	}
}
