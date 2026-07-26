package tasks

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/markdown"
)

// Golden tests pin the exact text every task sends to a provider. A prompt is
// behaviour, so a wording change should show up as a reviewable diff rather than
// an invisible shift in what the model is asked to do.
//
// Regenerate with: go test ./internal/llm/tasks -update

var update = flag.Bool("update", false, "rewrite golden prompt files")

// fixtureInput is one spec's worth of context, shared by the section-scoped
// tasks so their goldens differ only where the task differs.
func fixtureInput() llm.Input {
	return llm.Input{
		SpecID: "SPEC-034",
		Meta: map[string]string{
			"title": "Checkout timeout handling",
		},
		Repos: []string{"payments-api", "checkout-web"},
		Sections: map[string]string{
			"problem_statement": "Checkout requests exceeding 30s time out silently for ~4% of paid conversions.",
			"goals_non_goals":   "Goal: every checkout completes or reports an actionable failure within 30s.",
			"proposed_solution": "Queue the capture step and reconcile asynchronously.",
			"decision_log":      "| 001 | Queue vs synchronous retry | Queue | Bounded latency |",
			"escape_hatch_log":  "",
		},
	}
}

func TestGoldenPrompts(t *testing.T) {
	cases := []struct {
		name   string
		taskID string
		in     llm.Input
	}{
		{
			name:   "draft-section",
			taskID: DraftSection,
			in: func() llm.Input {
				in := fixtureInput()
				in.Section = "goals_non_goals"
				return in
			}(),
		},
		{
			name:   "draft-acceptance",
			taskID: DraftAcceptance,
			in:     fixtureInput(),
		},
		{
			name:   "draft-pr",
			taskID: DraftPR,
			in: func() llm.Input {
				in := fixtureInput()
				in.Diff = "--- a/pay.go\n+++ b/pay.go\n@@\n-timeout := 30\n+timeout := 10\n"
				in.Extra = map[string]string{"stack_position": "node 2 of 4"}
				return in
			}(),
		},
		{
			name:   "draft-pr-stack",
			taskID: DraftPRStack,
			in:     fixtureInput(),
		},
		{
			name:   "promote-triage",
			taskID: PromoteTriage,
			in: llm.Input{
				Meta: map[string]string{
					"title":    "Checkout hangs on slow card auth",
					"source":   "slack",
					"priority": "high",
				},
				Extra: map[string]string{"body": "Three reports this week; all on the 3DS path."},
			},
		},
		{
			name:   "draft-section-with-steer-and-prior",
			taskID: DraftSection,
			in: func() llm.Input {
				in := fixtureInput()
				in.Section = "problem_statement"
				in.PriorDraft = "The checkout is slow sometimes."
				in.SteerNotes = []string{"lead with the revenue impact", "two sentences max on the mechanism"}
				return in
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task, err := Get(tc.taskID)
			if err != nil {
				t.Fatalf("Get(%q): %v", tc.taskID, err)
			}
			got := llm.Assemble(task, tc.in).String()

			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden (run with -update to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("assembled prompt differs from golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// Every registered task must have a golden file, so adding a task without one
// fails here rather than shipping an untested prompt.
func TestEveryTaskHasAGolden(t *testing.T) {
	for _, id := range IDs() {
		found := false
		entries, err := os.ReadDir("testdata")
		if err != nil {
			t.Fatalf("reading testdata: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), id+".golden") || e.Name() == id+".golden" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("task %q has no golden prompt file in testdata/", id)
		}
	}
}

func TestGet_UnknownTaskNamesKnownOnes(t *testing.T) {
	_, err := Get("no-such-task")
	if err == nil {
		t.Fatal("expected an error for an unknown task")
	}
	if !strings.Contains(err.Error(), DraftSection) {
		t.Errorf("error should list known tasks, got %q", err)
	}
}

// A steer note is the one thing a reviewer asked for explicitly, so it must
// carry maximum weight and survive any budget.
func TestSteerNote_IsCriticalWeight(t *testing.T) {
	task, err := Get(DraftSection)
	if err != nil {
		t.Fatal(err)
	}
	in := fixtureInput()
	in.Section = "goals_non_goals"
	in.SteerNotes = []string{"be terse"}

	req := llm.Assemble(task, in)
	var found bool
	for _, p := range req.Parts {
		if p.Label == llm.SteerLabel {
			found = true
			if p.Weight != llm.WeightCritical {
				t.Errorf("steer weight = %d, want WeightCritical (%d)", p.Weight, llm.WeightCritical)
			}
		}
	}
	if !found {
		t.Fatalf("steer note missing from assembled parts")
	}
}

// The section being drafted must not be fed back as context: it invites the
// model to echo stale content instead of rewriting it.
func TestSectionTask_ExcludesTargetSection(t *testing.T) {
	task, err := Get(DraftSection)
	if err != nil {
		t.Fatal(err)
	}
	in := fixtureInput()
	in.Section = "problem_statement"

	req := llm.Assemble(task, in)
	for _, p := range req.Parts {
		if p.Label == "Problem Statement" {
			t.Error("the target section should not appear in its own context")
		}
	}
}

// Assembly must be deterministic: the same inputs produce byte-identical
// prompts, or golden tests are meaningless and caching is impossible.
func TestAssembly_IsDeterministic(t *testing.T) {
	task, err := Get(DraftAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	first := llm.Assemble(task, fixtureInput()).String()
	for i := 0; i < 20; i++ {
		if got := llm.Assemble(task, fixtureInput()).String(); got != first {
			t.Fatalf("assembly is not deterministic on iteration %d", i)
		}
	}
}

// The section-weight table keys must be real slugs. A typo or an invented slug
// would silently demote a section to background weight and sort it into the
// alphabetical tail, which is exactly the kind of bug a reader cannot see.
func TestSectionWeights_UseRealSlugs(t *testing.T) {
	valid := make(map[string]bool)
	for _, s := range markdown.ValidSectionSlugs() {
		valid[s] = true
	}
	for slug := range sectionWeights {
		if !valid[slug] {
			t.Errorf("section weight table names %q, which is not a valid section slug", slug)
		}
	}
}
