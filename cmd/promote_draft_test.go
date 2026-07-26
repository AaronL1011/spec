package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The model is told to emit two exact headings, but a model is not a parser. An
// unexpected shape must degrade to something the user can fix by editing, never
// to a dropped draft.
func TestSplitPromotedDraft(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantProblem string
		wantOutcome string
	}{
		{
			name: "exact headings as instructed",
			content: `## Problem Statement

Checkout times out for 4% of paid conversions.

## Desired Outcome

Every checkout completes or fails actionably within 30s.`,
			wantProblem: "Checkout times out for 4% of paid conversions.",
			wantOutcome: "Every checkout completes or fails actionably within 30s.",
		},
		{
			name: "heading level and wording drift is tolerated",
			content: `# The Problem

It breaks.

### Goals

It should not break.`,
			wantProblem: "It breaks.",
			wantOutcome: "It should not break.",
		},
		{
			// No recognisable headings: keep everything rather than losing it.
			name:        "unheaded prose becomes the problem statement",
			content:     "Checkout is slow and users complain.",
			wantProblem: "Checkout is slow and users complain.",
			wantOutcome: "",
		},
		{
			name:        "outcome only",
			content:     "## Desired Outcome\n\nFast checkout.",
			wantProblem: "",
			wantOutcome: "Fast checkout.",
		},
		{
			name:        "empty input",
			content:     "",
			wantProblem: "",
			wantOutcome: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem, outcome := splitPromotedDraft(tt.content)
			if problem != tt.wantProblem {
				t.Errorf("problem = %q, want %q", problem, tt.wantProblem)
			}
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %q, want %q", outcome, tt.wantOutcome)
			}
		})
	}
}

// Content must survive the split: a parser that silently drops prose would lose
// work the user already accepted.
func TestSplitPromotedDraft_LosesNoContent(t *testing.T) {
	content := "## Problem Statement\n\nline one\nline two\n\n## Desired Outcome\n\nline three"
	problem, outcome := splitPromotedDraft(content)
	joined := problem + outcome
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(joined, want) {
			t.Errorf("split dropped %q", want)
		}
	}
}

func TestStripYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips a frontmatter block",
			in:   "---\nid: TRIAGE-014\ntitle: x\n---\n\nThe body.\n",
			want: "\nThe body.\n",
		},
		{
			name: "leaves content without frontmatter alone",
			in:   "Just a body.\n",
			want: "Just a body.\n",
		},
		{
			name: "unterminated frontmatter is left intact rather than eaten",
			in:   "---\nid: X\nno closing delimiter\n",
			want: "---\nid: X\nno closing delimiter\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripYAMLFrontmatter(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTriageBody_MissingFileDegrades(t *testing.T) {
	if got := triageBody(filepath.Join(t.TempDir(), "nope.md")); got != "" {
		t.Errorf("a missing triage file should yield empty, got %q", got)
	}
}

func TestTriageBody_ReadsProseUnderFrontmatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TRIAGE-014.md")
	body := "---\nid: TRIAGE-014\npriority: high\n---\n\nThree reports this week, all on the 3DS path.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := triageBody(path)
	if !strings.Contains(got, "3DS path") {
		t.Errorf("body = %q, want the prose", got)
	}
	if strings.Contains(got, "priority") {
		t.Errorf("frontmatter should be stripped, got %q", got)
	}
}

// A skipped draft must leave the spec's sections untouched rather than writing
// empty ones: promotion succeeded, the assist simply declined.
func TestApplyPromotedSections_EmptyContentWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SPEC-001.md")
	original := "---\nid: SPEC-001\n---\n\n## 1. Problem Statement\n\nplaceholder\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applyPromotedSections(path, promotedSections{}); err != nil {
		t.Fatalf("applyPromotedSections: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("an empty draft must not modify the spec:\ngot:  %q\nwant: %q", after, original)
	}
}
