package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
	"github.com/spf13/cobra"
)

// `spec review <id> --plan --assist` prints advisory notes on a build plan.
//
// Reviewing a plan before building is cheaper than discovering a bad split
// halfway through a stack, but a feasibility read is exactly the kind of work an
// agent does well and a busy reviewer skips. So the notes are offered as a
// reading aid.
//
// The notes carry no verdict weight, and this path deliberately writes nothing:
// no review status, no spec content, no frontmatter. An agent that could nudge
// an approval would make the human gate advisory instead of the other way
// around, and the value here is a reviewer who reads the plan more carefully,
// not one who defers to a model.

// runPlanAssist generates advisory review notes for a spec's PR stack plan and
// prints them without touching the plan's review state.
func runPlanAssist(cmd *cobra.Command, rc *config.ResolvedConfig, specID string) error {
	if !rc.AgentDraftsEnabled() {
		if !rc.HasAgent() {
			return fmt.Errorf("no agent configured — set 'agent:' in ~/.spec/config.yaml, then rerun with --assist")
		}
		return fmt.Errorf("agent assistance is disabled in your preferences; set 'preferences.agent_drafts: true' in ~/.spec/config.yaml to enable")
	}

	svc := newLLMService(rc)
	if !svc.IsAvailable() {
		return fmt.Errorf("the configured agent has no completion plane — %s; review the plan yourself with 'spec review %s --plan', or configure a completion-capable provider",
			agentPlaneHint(rc), specID)
	}

	in, err := draftInput(rc, specID)
	if err != nil {
		return err
	}
	if in.Sections["pr_stack_plan"] == "" {
		return fmt.Errorf("%s", planAssistMissingPlanHint(specID))
	}

	task, err := tasks.Get(tasks.ReviewPlan)
	if err != nil {
		return err
	}

	res, err := svc.Run(context.Background(), task, in)
	if err != nil {
		return draftError(err)
	}
	if res == nil || res.Text == "" {
		return fmt.Errorf("the agent returned no advisory notes — check the provider with 'spec agent check'")
	}

	p := newPrinter(cmd)
	p.Line("ADVISORY · plan review · %s", specID)
	p.Line("These notes are advisory only. The approve/request-changes verdict is yours.")
	p.Line("")
	p.Line("%s", res.Text)
	if status := generationStatus(res); status != "" {
		p.Line("")
		p.Line("%s", status)
	}

	// Recorded like any other generation so retry counts and latency stay
	// comparable across tasks, and attributed to the human who asked for it.
	recordGeneration(rc, specID, &llm.Outcome{
		Action:   llm.ActionAccept,
		Content:  res.Text,
		Attempts: []llm.Attempt{{Content: res.Text, Result: res}},
	}, task.ID)

	return nil
}

// planAssistMissingPlanHint explains an empty §7.3 and names the command that
// fills it.
//
// Sending an empty plan to a model would produce confident notes about nothing,
// which is worse than declining: advisory output reads as authoritative, so it
// must never be generated from absent input.
func planAssistMissingPlanHint(specID string) string {
	return fmt.Sprintf("%s has no PR stack plan to review — draft one with 'spec draft %s --pr-stack'", specID, specID)
}

// generationStatus renders what a generation cost, so a slow local model reads
// as slow rather than broken.
func generationStatus(res *llm.Result) string {
	if res == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if res.Model != "" {
		parts = append(parts, res.Model)
	}
	if res.Duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", res.Duration.Seconds()))
	}
	if res.Tokens.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens", res.Tokens.Total))
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " · " + p
	}
	return out
}
