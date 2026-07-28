package llm

import (
	"strings"
	"testing"
)

// Context budgeting must be deterministic and must never drop the reviewer's
// steer note: dropping the one thing the user asked for explicitly, to fit a
// budget, is the single trim that is never acceptable.

func TestApplyBudget_DropsLowestWeightFirst(t *testing.T) {
	parts := []ContextPart{
		{Label: "primary", Content: strings.Repeat("a", 400), Weight: WeightPrimary},
		{Label: "background", Content: strings.Repeat("b", 400), Weight: WeightBackground},
		{Label: "supporting", Content: strings.Repeat("c", 400), Weight: WeightSupporting},
	}
	// Each part is 100 tokens; a 250 budget forces one drop.
	kept, trimmed := applyBudget(parts, 250)

	if len(trimmed) != 1 || trimmed[0] != "background" {
		t.Fatalf("trimmed = %v, want the lowest-weight part only", trimmed)
	}
	labels := labelsOf(kept)
	if labels != "primary,supporting" {
		t.Errorf("kept = %s, want primary,supporting in declaration order", labels)
	}
}

func TestApplyBudget_TieBreaksByLatestDeclaration(t *testing.T) {
	parts := []ContextPart{
		{Label: "first", Content: strings.Repeat("a", 400), Weight: WeightSupporting},
		{Label: "second", Content: strings.Repeat("b", 400), Weight: WeightSupporting},
	}
	_, trimmed := applyBudget(parts, 100)
	if len(trimmed) != 1 || trimmed[0] != "second" {
		t.Errorf("trimmed = %v, want the later-declared part on a weight tie", trimmed)
	}
}

func TestApplyBudget_IsDeterministic(t *testing.T) {
	build := func() []ContextPart {
		return []ContextPart{
			{Label: "a", Content: strings.Repeat("x", 400), Weight: WeightBackground},
			{Label: "b", Content: strings.Repeat("x", 400), Weight: WeightBackground},
			{Label: "c", Content: strings.Repeat("x", 400), Weight: WeightSupporting},
			{Label: "d", Content: strings.Repeat("x", 400), Weight: WeightPrimary},
		}
	}
	first, firstTrim := applyBudget(build(), 220)
	for i := 0; i < 50; i++ {
		kept, trimmed := applyBudget(build(), 220)
		if labelsOf(kept) != labelsOf(first) || strings.Join(trimmed, ",") != strings.Join(firstTrim, ",") {
			t.Fatalf("budgeting is not deterministic on iteration %d", i)
		}
	}
}

// A steer note carries WeightCritical, so every other part must be dropped
// before it is even considered.
func TestApplyBudget_SteerNoteSurvivesEverything(t *testing.T) {
	in := Input{
		SteerNotes: []string{"lead with the revenue impact"},
	}
	task := Task{
		ID:          "t",
		TokenBudget: 5, // absurdly small: everything droppable must drop
		Build: func(Input) (string, []ContextPart) {
			return "prompt", []ContextPart{
				{Label: "big", Content: strings.Repeat("z", 4000), Weight: WeightPrimary},
				{Label: "also big", Content: strings.Repeat("y", 4000), Weight: WeightSupporting},
			}
		},
	}

	req := Assemble(task, in)
	if len(req.Parts) != 1 {
		t.Fatalf("kept %d parts, want only the steer note: %v", len(req.Parts), labelsOf(req.Parts))
	}
	if req.Parts[0].Label != SteerLabel {
		t.Errorf("surviving part = %q, want %q", req.Parts[0].Label, SteerLabel)
	}
}

// A model that silently received less than the task promised would be misled,
// and a user reading a thin draft deserves to know why.
func TestGenerateRequest_RecordsTrimming(t *testing.T) {
	task := Task{
		ID:          "t",
		TokenBudget: 10,
		Build: func(Input) (string, []ContextPart) {
			return "base prompt", []ContextPart{
				{Label: "dropped-part", Content: strings.Repeat("q", 4000), Weight: WeightBackground},
			}
		},
	}
	req := Assemble(task, Input{})
	if len(req.Trimmed) == 0 {
		t.Fatal("expected trimming to be recorded")
	}
	prompt := req.GenerateRequest(0).Prompt
	if !strings.Contains(prompt, "dropped-part") {
		t.Errorf("prompt should name the omitted context, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "omitted") {
		t.Errorf("prompt should state that context was omitted, got:\n%s", prompt)
	}
}

func TestApplyBudget_NoBudgetKeepsEverything(t *testing.T) {
	parts := []ContextPart{
		{Label: "a", Content: strings.Repeat("x", 100000), Weight: WeightBackground},
	}
	kept, trimmed := applyBudget(parts, 0)
	if len(kept) != 1 || len(trimmed) != 0 {
		t.Errorf("a zero budget means no cap; got kept=%d trimmed=%v", len(kept), trimmed)
	}
}

// Multiple steer notes read as a history rather than a blur, so a sequence of
// nudges is legible to the model.
func TestJoinNotes_NumbersMultipleNotes(t *testing.T) {
	if got := joinNotes(nil); got != "" {
		t.Errorf("no notes = %q, want empty", got)
	}
	if got := joinNotes([]string{"  only  "}); got != "only" {
		t.Errorf("single note = %q, want trimmed and unnumbered", got)
	}
	got := joinNotes([]string{"first", "", "second"})
	if got != "1. first\n2. second" {
		t.Errorf("multiple notes = %q, want a numbered list skipping blanks", got)
	}
}

func TestAssemble_IncludesPriorDraft(t *testing.T) {
	task := Task{ID: "t", Build: func(Input) (string, []ContextPart) { return "p", nil }}
	req := Assemble(task, Input{PriorDraft: "the rejected text"})
	found := false
	for _, p := range req.Parts {
		if p.Label == PriorDraftLabel && p.Content == "the rejected text" {
			found = true
		}
	}
	if !found {
		t.Error("a retry should show the model what it produced before")
	}
}

func labelsOf(parts []ContextPart) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Label)
	}
	return strings.Join(out, ",")
}
