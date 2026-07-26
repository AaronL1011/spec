package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var draftCmd = &cobra.Command{
	Use:   "draft [id]",
	Short: "Draft a spec section, acceptance criteria, PR description, or PR stack plan with an agent",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDraft,
}

func init() {
	draftCmd.Flags().String("section", "", "section slug to draft (e.g., problem_statement)")
	draftCmd.Flags().Bool("acceptance", false, "draft acceptance criteria for §6")
	draftCmd.Flags().Bool("pr", false, "draft a PR description")
	draftCmd.Flags().Int("pr-number", 0, "target a specific PR (used with --pr)")
	draftCmd.Flags().Bool("pr-stack", false, "propose a PR stack plan for §7.3")
	draftCmd.Flags().Bool("accept", false, "accept the first draft without review (for scripts and CI)")
	rootCmd.AddCommand(draftCmd)
}

func runDraft(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec draft <id>")
	if err != nil {
		return err
	}
	section, _ := cmd.Flags().GetString("section")
	acceptance, _ := cmd.Flags().GetBool("acceptance")
	prMode, _ := cmd.Flags().GetBool("pr")
	prStack, _ := cmd.Flags().GetBool("pr-stack")
	autoAccept, _ := cmd.Flags().GetBool("accept")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if !rc.AgentDraftsEnabled() {
		if !rc.HasAgent() {
			return fmt.Errorf("no agent configured — set 'agent:' in ~/.spec/config.yaml, or write the section manually with 'spec edit %s'", specID)
		}
		return fmt.Errorf("agent drafting is disabled in your preferences; set 'preferences.agent_drafts: true' in ~/.spec/config.yaml to enable")
	}

	svc := newLLMService(rc)
	if !svc.IsAvailable() {
		return fmt.Errorf("the configured agent has no completion plane — %s; write the section manually with 'spec edit %s', or configure a completion-capable provider (anthropic, openai-compatible, ollama)",
			agentPlaneHint(rc), specID)
	}

	switch {
	case section != "":
		return draftSectionCmd(rc, svc, specID, section, autoAccept)
	case acceptance:
		return draftAcceptanceCmd(rc, svc, specID, autoAccept)
	case prMode:
		return draftPRCmd(rc, svc, specID, autoAccept)
	case prStack:
		return draftPRStackCmd(rc, svc, specID, autoAccept)
	}

	return fmt.Errorf("specify what to draft: --section <slug>, --acceptance, --pr, or --pr-stack")
}

// newLLMService builds the task-running service from the resolved agent.
func newLLMService(rc *config.ResolvedConfig) *llm.Service {
	reg := buildRegistry(rc)
	agentCfg := rc.EffectiveAgentConfig()
	return llm.NewService(reg.Agent(), true).WithMaxTokens(agentCfg.Generate.MaxTokens)
}

// agentPlaneHint explains why a configured agent cannot draft, naming the
// provider so the message is actionable rather than generic.
func agentPlaneHint(rc *config.ResolvedConfig) string {
	provider := rc.EffectiveAgentConfig().Provider
	if provider == "" {
		return "no agent is configured"
	}
	return fmt.Sprintf("%q supports sessions but not one-shot completions", provider)
}

// draftInput assembles task input from a spec on disk. Every draft command needs
// the same spec context, so this is the one place that reads it.
func draftInput(rc *config.ResolvedConfig, specID string) (llm.Input, error) {
	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return llm.Input{}, err
	}

	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return llm.Input{}, err
	}
	byslug := make(map[string]string, len(sections))
	for _, s := range sections {
		byslug[s.Slug] = s.Content
	}

	in := llm.Input{SpecID: specID, Sections: byslug, Meta: map[string]string{}}
	if meta, err := markdown.ReadMeta(path); err == nil && meta != nil {
		in.Meta["title"] = meta.Title
		in.Repos = meta.Repos
	}
	return in, nil
}

// reviewDraft runs the gate for a task and returns the accepted content, or an
// empty string when the reviewer skipped.
//
// autoAccept exists for scripts and CI: the gate needs a terminal, so a piped
// invocation must either opt out of review explicitly or be told why it cannot
// proceed. Silently prompting at a pipe that will never answer is the one
// behaviour that would be indefensible.
func reviewDraft(rc *config.ResolvedConfig, svc *llm.Service, taskID string, in llm.Input, autoAccept bool) (string, error) {
	task, err := tasks.Get(taskID)
	if err != nil {
		return "", err
	}

	gen := func(ctx context.Context, notes []string, prior string) (*llm.Result, error) {
		attemptIn := in
		attemptIn.SteerNotes = notes
		attemptIn.PriorDraft = prior
		return svc.Run(ctx, task, attemptIn)
	}

	ctx := context.Background()

	if autoAccept {
		res, err := gen(ctx, nil, "")
		if err != nil {
			return "", draftError(err)
		}
		if res == nil || strings.TrimSpace(res.Text) == "" {
			return "", fmt.Errorf("the agent returned an empty draft for %s", task.ID)
		}
		return res.Text, nil
	}

	if !llm.IsInteractive() {
		return "", fmt.Errorf("%w — rerun with --accept to take the first draft unreviewed", llm.ErrNotInteractive)
	}

	editor := ""
	if rc.User != nil {
		editor = rc.User.Preferences.Editor
	}
	prompter := llm.NewCLIPrompter(task.Title, editor)

	// Escalation to an interactive session lands with `spec draft
	// --interactive`; until then the gate does not advertise an action it
	// cannot perform.
	caps := llm.GateCapabilities{CanEscalate: false}

	outcome, err := llm.Review(ctx, gen, prompter, caps)
	if err != nil {
		return "", draftError(err)
	}
	if outcome.Action != llm.ActionAccept {
		fmt.Println("Draft skipped — nothing written.")
		return "", nil
	}
	return outcome.Content, nil
}

// draftError turns service-level failures into messages that name the next
// action. The service never prints, so wording lives here.
func draftError(err error) error {
	switch {
	case errors.Is(err, llm.ErrUnavailable):
		return fmt.Errorf("no agent completion plane available — configure 'agent:' in ~/.spec/config.yaml and verify it with 'spec agent check'")
	case errors.Is(err, llm.ErrNoDraft):
		return fmt.Errorf("the agent returned an empty draft — check the provider with 'spec agent check'")
	case errors.Is(err, llm.ErrNotInteractive):
		return err
	default:
		return err
	}
}

func draftSectionCmd(rc *config.ResolvedConfig, svc *llm.Service, specID, sectionSlug string, autoAccept bool) error {
	if !markdown.IsValidSectionSlug(sectionSlug) {
		return fmt.Errorf("invalid section slug %q — valid slugs: %s",
			sectionSlug, strings.Join(markdown.ValidSectionSlugs(), ", "))
	}

	in, err := draftInput(rc, specID)
	if err != nil {
		return err
	}
	in.Section = sectionSlug

	content, err := reviewDraft(rc, svc, tasks.DraftSection, in, autoAccept)
	if err != nil || content == "" {
		return err
	}
	return writeSpecSection(rc, specID, sectionSlug, content, fmt.Sprintf("docs: %s — agent-drafted %s", specID, sectionSlug))
}

func draftAcceptanceCmd(rc *config.ResolvedConfig, svc *llm.Service, specID string, autoAccept bool) error {
	in, err := draftInput(rc, specID)
	if err != nil {
		return err
	}
	in.Section = "acceptance_criteria"

	content, err := reviewDraft(rc, svc, tasks.DraftAcceptance, in, autoAccept)
	if err != nil || content == "" {
		return err
	}
	return writeSpecSection(rc, specID, "acceptance_criteria", content,
		fmt.Sprintf("docs: %s — agent-drafted acceptance criteria", specID))
}

func draftPRCmd(rc *config.ResolvedConfig, svc *llm.Service, specID string, autoAccept bool) error {
	in, err := draftInput(rc, specID)
	if err != nil {
		return err
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	diff, err := gitpkg.Diff(context.Background(), workDir, "main")
	if err != nil {
		return fmt.Errorf("could not diff against main: %w — run this from the repo with your change", err)
	}
	in.Diff = diff

	content, err := reviewDraft(rc, svc, tasks.DraftPR, in, autoAccept)
	if err != nil || content == "" {
		return err
	}

	// A PR description is not spec content: print it for the user to paste or
	// pipe rather than writing it into the spec.
	fmt.Println(content)
	return nil
}

func draftPRStackCmd(rc *config.ResolvedConfig, svc *llm.Service, specID string, autoAccept bool) error {
	in, err := draftInput(rc, specID)
	if err != nil {
		return err
	}

	content, err := reviewDraft(rc, svc, tasks.DraftPRStack, in, autoAccept)
	if err != nil || content == "" {
		return err
	}
	return writeSpecSection(rc, specID, "pr_stack_plan", content,
		fmt.Sprintf("docs: %s — agent-drafted PR stack plan", specID))
}

// writeSpecSection commits accepted content to the specs repo through the
// markdown engine, so a drafted section is validated and formatted exactly like
// a hand-written one.
func writeSpecSection(rc *config.ResolvedConfig, specID, slug, content, message string) error {
	return gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(nil, specID), func(repoPath string) (string, error) {
		specPath, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		if err := markdown.ReplaceSection(specPath, slug, content); err != nil {
			return "", err
		}
		return message, nil
	})
}
