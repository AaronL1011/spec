package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage custom spec and triage templates",
	Long: `Manage team-customisable spec and triage skeletons (SPEC-025).

Templates live at templates/spec.md and templates/triage.md in the specs
repo. A committed team template overrides the built-in default used by
'spec new', 'spec promote', and 'spec intake'. Frontmatter defaults may be
seeded via the templates.frontmatter_defaults block in spec.config.yaml.

Subcommands:
  init      Write the built-in default templates to the specs repo
  validate  Validate the effective (or a given) template
  show      Print the effective (resolved) template`,
}

var templateInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write the built-in default templates to the specs repo",
	Long: `Write the embedded default spec and triage templates to
templates/spec.md and templates/triage.md in the specs repo, committed via
the standard sync wrapper. Refuses to overwrite existing files without
--force. Use this to bootstrap a custom template from a known-good base.`,
	RunE: runTemplateInit,
}

var templateValidateCmd = &cobra.Command{
	Use:   "validate [kind]",
	Short: "Validate the effective spec or triage template",
	Long: `Parse, render, and structurally check the effective template.

kind is "spec" (default) or "triage". The committed team file is validated
when present (even if scaffolding would fall back), otherwise the embedded
built-in default. Validation fails closed on:
  - parse or render errors
  - unresolved <% ... %> placeholders
  - missing gate-critical sections (spec only): problem_statement,
    user_stories, acceptance_criteria

A level-2 section lacking an <!-- owner: ... --> or <!-- auto: ... -->
marker is reported as a warning (non-fatal).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTemplateValidate,
}

var templateShowCmd = &cobra.Command{
	Use:   "show [kind]",
	Short: "Print the effective (resolved) template",
	Long: `Print the template that 'spec new'/'spec intake' would render
against: the committed team file when present and parseable, otherwise the
embedded built-in default. kind is "spec" (default) or "triage".`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTemplateShow,
}

func init() {
	templateInitCmd.Flags().Bool("force", false, "overwrite existing template files")
	templateCmd.AddCommand(templateInitCmd)
	templateCmd.AddCommand(templateValidateCmd)
	templateCmd.AddCommand(templateShowCmd)
	rootCmd.AddCommand(templateCmd)
}

func parseTemplateKindArg(args []string) markdown.TemplateKind {
	if len(args) > 0 && args[0] == "triage" {
		return markdown.TriageTemplate
	}
	return markdown.SpecTemplate
}

func runTemplateInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	specPath := markdown.DefaultSpecPath
	triagePath := markdown.DefaultTriagePath
	if rc.Team.Templates.SpecPath != "" {
		specPath = rc.Team.Templates.SpecPath
	}
	if rc.Team.Templates.TriagePath != "" {
		triagePath = rc.Team.Templates.TriagePath
	}

	commitMsg, err := commitTemplateInit(cmd, rc, specPath, triagePath, force)
	if err != nil {
		return err
	}
	if commitMsg == "" {
		fmt.Println("No changes: templates already present (use --force to overwrite).")
		return nil
	}
	fmt.Printf("✓ Wrote templates (%s)\n", commitMsg)
	fmt.Printf("  %s\n", specPath)
	fmt.Printf("  %s\n", triagePath)
	fmt.Println("\nEdit them to customise, then run 'spec template validate'.")
	return nil
}

// commitTemplateInit writes the embedded default templates to the specs repo
// via the sync wrapper (SPEC-013). It returns "" when both files already
// exist and --force is false (no commit), otherwise the commit message.
// Attributed to surface: cli, trigger: template/init per the spec's audit
// requirement.
func commitTemplateInit(cmd *cobra.Command, rc *config.ResolvedConfig, specPath, triagePath string, force bool) (string, error) {
	specContent := markdown.DefaultSpecTemplate()
	triageContent := markdown.DefaultTriageTemplate()

	const commitMsg = "chore: bootstrap templates from built-in default"
	var wrote bool
	opts := syncOpts(cmd, "")
	opts.Trigger = "template/init"
	err := gitpkg.WithSpecsRepoOpts(ctx(), &rc.Team.SpecsRepo, opts, func(repoPath string) (string, error) {
		for _, w := range []struct{ path, content string }{
			{specPath, specContent},
			{triagePath, triageContent},
		} {
			full := filepath.Join(repoPath, w.path)
			if !force {
				if _, statErr := os.Stat(full); statErr == nil {
					continue
				}
			}
			if mkErr := os.MkdirAll(filepath.Dir(full), 0o755); mkErr != nil {
				return "", fmt.Errorf("creating %s directory: %w", filepath.Dir(w.path), mkErr)
			}
			if wErr := os.WriteFile(full, []byte(w.content), 0o644); wErr != nil {
				return "", fmt.Errorf("writing %s: %w", w.path, wErr)
			}
			wrote = true
		}
		if !wrote {
			return "", nil
		}
		return commitMsg, nil
	})
	if err != nil {
		return "", err
	}
	if !wrote {
		return "", nil
	}
	return commitMsg, nil
}

func runTemplateValidate(cmd *cobra.Command, args []string) error {
	kind := parseTemplateKindArg(args)

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	repoDir, configuredPath := effectiveTemplatePaths(rc, kind)

	// Validate the raw team file when it exists — ResolveTemplate would
	// silently substitute the built-in default for a broken team file, which
	// is exactly what this command must surface.
	content, path, ok := markdown.ReadTeamTemplate(kind, repoDir, configuredPath)
	source := "default (built-in)"
	switch {
	case ok:
		source = "team (" + path + ")"
	case kind == markdown.TriageTemplate:
		content = markdown.DefaultTriageTemplate()
	default:
		content = markdown.DefaultSpecTemplate()
	}
	issues := markdown.ValidateTemplate(kind, content)

	label := "spec"
	if kind == markdown.TriageTemplate {
		label = "triage"
	}
	fmt.Printf("Validating %s template (source: %s)\n\n", label, source)

	fatal := false
	for _, iss := range issues {
		mark := "⚠"
		if iss.Fatal {
			mark = "✗"
			fatal = true
		}
		fmt.Printf("  %s %s\n", mark, iss.Message)
	}
	if len(issues) == 0 {
		fmt.Printf("✓ %s template is valid.\n", label)
		return nil
	}
	fmt.Println()
	if fatal {
		fmt.Println("New specs will scaffold from the built-in default until this is fixed.")
		return fmt.Errorf("template validation failed — resolve the ✗ issues above")
	}
	fmt.Printf("⚠ %s template has warnings but no fatal issues.\n", label)
	return nil
}

func runTemplateShow(cmd *cobra.Command, args []string) error {
	kind := parseTemplateKindArg(args)

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	repoDir, configuredPath := effectiveTemplatePaths(rc, kind)

	content, source := markdown.ResolveTemplate(kind, repoDir, configuredPath)
	fmt.Fprintf(os.Stderr, "# source: %s\n", source)
	fmt.Print(content)
	return nil
}

// effectiveTemplatePaths returns the specs repo root and the configured
// template path for a kind, defaulting to the conventional locations.
func effectiveTemplatePaths(rc *config.ResolvedConfig, kind markdown.TemplateKind) (repoDir, configuredPath string) {
	if rc.Team == nil {
		return "", ""
	}
	repoDir = rc.SpecsRepoRoot()
	if kind == markdown.TriageTemplate {
		configuredPath = rc.Team.Templates.EffectiveTriagePath()
	} else {
		configuredPath = rc.Team.Templates.EffectiveSpecPath()
	}
	return repoDir, configuredPath
}
