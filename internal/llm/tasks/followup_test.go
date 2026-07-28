package tasks

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/llm"
)

// The fast-follow tasks each have one input they cannot lose: the text being
// revised, the plan being reviewed, the log being summarised, the discussion
// being mined. Budgeting drops low-weight context first, so these assert the
// subject survives a budget far too small to hold the surrounding spec.

func TestFollowupTasks_SubjectSurvivesTinyBudget(t *testing.T) {
	bulky := strings.Repeat("surrounding spec context that must be dropped first. ", 400)

	tests := []struct {
		name    string
		taskID  string
		input   llm.Input
		subject string
	}{
		{
			name:   "revise-section keeps the text being revised",
			taskID: ReviseSection,
			input: llm.Input{
				SpecID:  "SPEC-001",
				Section: "proposed_solution",
				Sections: map[string]string{
					"proposed_solution": "THE-TEXT-UNDER-REVISION",
					"problem_statement": bulky,
					"goals_non_goals":   bulky,
				},
			},
			subject: "THE-TEXT-UNDER-REVISION",
		},
		{
			name:   "review-plan keeps the plan",
			taskID: ReviewPlan,
			input: llm.Input{
				SpecID: "SPEC-001",
				Sections: map[string]string{
					"pr_stack_plan":     "THE-PLAN-UNDER-REVIEW",
					"problem_statement": bulky,
					"proposed_solution": bulky,
				},
			},
			subject: "THE-PLAN-UNDER-REVIEW",
		},
		{
			name:   "summarise-activity keeps the log",
			taskID: SummariseActivity,
			input: llm.Input{
				SpecID:   "SPEC-001",
				Extra:    map[string]string{"activity": "THE-ACTIVITY-LOG"},
				Sections: map[string]string{"problem_statement": bulky, "goals_non_goals": bulky},
			},
			subject: "THE-ACTIVITY-LOG",
		},
		{
			name:   "extract-decision keeps the discussion",
			taskID: ExtractDecision,
			input: llm.Input{
				SpecID:   "SPEC-001",
				Extra:    map[string]string{"discussion": "THE-DISCUSSION-THREAD"},
				Sections: map[string]string{"problem_statement": bulky, "proposed_solution": bulky},
			},
			subject: "THE-DISCUSSION-THREAD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := Get(tt.taskID)
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.taskID, err)
			}
			// A budget that cannot fit the bulky context forces eviction.
			task.TokenBudget = 200

			got := llm.Assemble(task, tt.input).String()
			if !strings.Contains(got, tt.subject) {
				t.Errorf("the task's subject was dropped by budgeting:\n%s", got)
			}
			// And the thing that should have gone, went.
			if strings.Count(got, "surrounding spec context") > 20 {
				t.Error("low-weight context should have been evicted first")
			}
		})
	}
}

// A reviewer note is the one instruction the user typed by hand, so it must
// outlive budgeting too.
func TestFollowupTasks_SteerNoteSurvives(t *testing.T) {
	task, err := Get(ReviseSection)
	if err != nil {
		t.Fatal(err)
	}
	task.TokenBudget = 150

	got := llm.Assemble(task, llm.Input{
		SpecID:  "SPEC-001",
		Section: "proposed_solution",
		Sections: map[string]string{
			"proposed_solution": "current text",
			"problem_statement": strings.Repeat("bulk ", 500),
		},
		SteerNotes: []string{"CUT-THE-SECOND-PARAGRAPH"},
	}).String()

	if !strings.Contains(got, "CUT-THE-SECOND-PARAGRAPH") {
		t.Errorf("the reviewer's own instruction was dropped:\n%s", got)
	}
}

// Every fast-follow task must be reachable through the registry, since that is
// the claim the set exists to demonstrate.
func TestFollowupTasks_Registered(t *testing.T) {
	for _, id := range []string{ReviseSection, ReviewPlan, SummariseActivity, ExtractDecision} {
		task, err := Get(id)
		if err != nil {
			t.Errorf("Get(%q): %v", id, err)
			continue
		}
		if task.System == "" {
			t.Errorf("task %q has no system prompt", id)
		}
		if task.Build == nil {
			t.Errorf("task %q has no builder", id)
		}
		if task.TokenBudget <= 0 {
			t.Errorf("task %q has no token budget, so context cannot be bounded", id)
		}
		// Structured output was deliberately not shipped for any task.
		if task.Format == adapter.FormatJSON {
			t.Errorf("task %q requests JSON, which no shipped task should", id)
		}
	}
}
