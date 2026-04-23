package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/ai"
	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var draftCmd = &cobra.Command{
	Use:   "draft <id>",
	Short: "Request an AI draft of a spec section, PR description, or PR stack plan",
	Args:  cobra.ExactArgs(1),
	RunE:  runDraft,
}

func init() {
	draftCmd.Flags().String("section", "", "section slug to draft (e.g., problem_statement)")
	draftCmd.Flags().Bool("pr", false, "draft a PR description")
	draftCmd.Flags().Int("pr-number", 0, "target a specific PR (used with --pr)")
	draftCmd.Flags().Bool("pr-stack", false, "propose a PR stack plan for §7.3")
	rootCmd.AddCommand(draftCmd)
}

func runDraft(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])
	section, _ := cmd.Flags().GetString("section")
	prMode, _ := cmd.Flags().GetBool("pr")
	prStack, _ := cmd.Flags().GetBool("pr-stack")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Check AI is configured
	if !rc.HasIntegration("ai") {
		return fmt.Errorf("AI integration not configured; write the section manually with 'spec edit %s' or run 'spec config init' to configure", specID)
	}

	if !rc.AIDraftsEnabled() {
		return fmt.Errorf("AI drafting is disabled in your preferences; set 'preferences.ai_drafts: true' in ~/.spec/config.yaml to enable")
	}

	reg := buildRegistry(rc)
	aiService := ai.NewService(reg.AI(), true)

	if section != "" {
		return draftSection(rc, aiService, specID, section)
	}
	if prMode {
		return draftPR(rc, aiService, specID)
	}
	if prStack {
		return draftPRStack(rc, aiService, specID)
	}

	return fmt.Errorf("specify what to draft: --section <slug>, --pr, or --pr-stack")
}

func draftSection(rc *config.ResolvedConfig, aiService *ai.Service, specID, sectionSlug string) error {
	if !markdown.IsValidSectionSlug(sectionSlug) {
		return fmt.Errorf("invalid section slug %q — valid slugs: %s",
			sectionSlug, strings.Join(markdown.ValidSectionSlugs(), ", "))
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return err
	}

	// Collect existing context
	existingContext := make(map[string]string)
	for _, s := range sections {
		if strings.TrimSpace(s.Content) != "" {
			existingContext[s.Slug] = s.Content
		}
	}

	prompt := ai.SectionDraftPrompt(sectionSlug, existingContext)
	draft, err := aiService.Draft(context.Background(), prompt)
	if err != nil {
		return err
	}
	if draft == "" {
		fmt.Println("AI provider did not return a draft. Write the section manually with 'spec edit'.")
		return nil
	}

	editor := ""
	if rc.User != nil {
		editor = rc.User.Preferences.Editor
	}
	result, err := ai.PresentDraft(sectionSlug, draft, editor)
	if err != nil {
		return err
	}

	if result.Action == "skip" {
		fmt.Println("Draft skipped.")
		return nil
	}

	// Write the accepted/edited content to the spec
	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		specPath, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		if err := markdown.ReplaceSection(specPath, sectionSlug, result.Content); err != nil {
			return "", err
		}

		return fmt.Sprintf("docs: %s — AI-drafted %s", specID, sectionSlug), nil
	})
}

func draftPR(rc *config.ResolvedConfig, aiService *ai.Service, specID string) error {
	if !rc.HasIntegration("repo") {
		return fmt.Errorf("repo integration not configured — needed for PR drafting")
	}

	// Get the spec context
	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	specData, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Get the current diff
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	diff, err := gitpkg.Diff(context.Background(), workDir, "main")
	if err != nil {
		diff = "(could not generate diff)"
	}

	prompt := ai.PRDescriptionPrompt(diff, string(specData), "")
	draft, err := aiService.Draft(context.Background(), prompt)
	if err != nil {
		return err
	}
	if draft == "" {
		fmt.Println("AI provider did not return a draft.")
		return nil
	}

	editor := ""
	if rc.User != nil {
		editor = rc.User.Preferences.Editor
	}
	result, err := ai.PresentDraft("PR Description", draft, editor)
	if err != nil {
		return err
	}

	if result.Action == "skip" {
		fmt.Println("Draft skipped.")
		return nil
	}

	fmt.Println("PR description drafted.")
	fmt.Println(result.Content)
	return nil
}

func draftPRStack(rc *config.ResolvedConfig, aiService *ai.Service, specID string) error {
	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return err
	}

	solution := ""
	if s := markdown.FindSection(sections, "proposed_solution"); s != nil {
		solution = s.Content
	}
	archNotes := ""
	if s := markdown.FindSection(sections, "architecture_notes"); s != nil {
		archNotes = s.Content
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return fmt.Errorf("reading spec metadata: %w", err)
	}
	repos := meta.Repos

	prompt := ai.PRStackPrompt(solution, archNotes, repos)
	draft, err := aiService.Draft(context.Background(), prompt)
	if err != nil {
		return err
	}
	if draft == "" {
		fmt.Println("AI provider did not return a draft.")
		return nil
	}

	editor := ""
	if rc.User != nil {
		editor = rc.User.Preferences.Editor
	}
	result, err := ai.PresentDraft("PR Stack Plan", draft, editor)
	if err != nil {
		return err
	}

	if result.Action == "skip" {
		fmt.Println("Draft skipped.")
		return nil
	}

	// Write to spec
	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		specPath, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		if err := markdown.ReplaceSection(specPath, "pr_stack_plan", result.Content); err != nil {
			return "", err
		}

		return fmt.Sprintf("docs: %s — AI-drafted PR stack plan", specID), nil
	})
}
