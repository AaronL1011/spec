package cmd

import (
	"fmt"
	"strings"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "View and modify pipeline configuration",
	Long: `View and modify the pipeline configuration.

Running 'spec pipeline' with no subcommand shows the current pipeline
with stages, icons, and owners.

Use --verbose to see gates and transition effects for each stage.`,
	RunE: runPipelineShow,
}

var pipelinePresetsCmd = &cobra.Command{
	Use:   "presets",
	Short: "List available pipeline presets",
	RunE:  runPipelinePresets,
}

var pipelineExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the fully resolved pipeline as YAML",
	RunE:  runPipelineExport,
}

func init() {
	pipelineCmd.Flags().BoolP("verbose", "v", false, "show gates and effects for each stage")
	pipelineCmd.Flags().Bool("no-icons", false, "suppress emoji icons")

	pipelineCmd.AddCommand(pipelinePresetsCmd)
	pipelineCmd.AddCommand(pipelineExportCmd)
	rootCmd.AddCommand(pipelineCmd)
}

func runPipelineShow(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	noIcons, _ := cmd.Flags().GetBool("no-icons")

	rc, err := resolveConfig()
	if err != nil {
		// No config - show what presets are available
		cmd.Println("No pipeline configured.")
		cmd.Println()
		cmd.Println("Run 'spec init' to set up a pipeline, or choose from presets:")
		cmd.Println()
		for _, name := range pipeline.PresetNames() {
			desc, _, _, _ := pipeline.PresetInfo(name)
			cmd.Printf("  %-12s %s\n", name, desc)
		}
		cmd.Println()
		cmd.Println("Run 'spec pipeline presets' for more details.")
		return nil
	}

	// Resolve the pipeline
	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		return fmt.Errorf("resolving pipeline: %w", err)
	}

	// Print header
	if resolved.PresetName != "" {
		cmd.Printf("Pipeline: %s preset", resolved.PresetName)
		if len(resolved.SkippedStages) > 0 {
			cmd.Printf(" (skipped: %s)", strings.Join(resolved.SkippedStages, ", "))
		}
		cmd.Println()
	} else {
		cmd.Println("Pipeline: custom")
	}
	cmd.Println()

	if verbose {
		printPipelineVerbose(cmd, resolved, noIcons)
	} else {
		printPipelineCompact(cmd, resolved, noIcons)
	}

	cmd.Println()
	cmd.Println("Commands:")
	cmd.Println("  spec pipeline --verbose     Show gates and effects")
	cmd.Println("  spec pipeline presets       List available presets")
	cmd.Println("  spec pipeline export        Show full YAML config")

	return nil
}

func printPipelineCompact(cmd *cobra.Command, resolved *pipeline.ResolvedPipeline, noIcons bool) {
	// Print stage flow
	var names []string
	var icons []string
	var owners []string

	for _, stage := range resolved.Stages {
		name := stage.Name
		if stage.Optional {
			name += "?"
		}
		names = append(names, name)

		icon := stage.Icon
		if icon == "" {
			icon = "○"
		}
		if noIcons {
			icon = "○"
		}
		icons = append(icons, icon)

		owners = append(owners, stage.GetOwner())
	}

	// Calculate column widths
	widths := make([]int, len(names))
	for i := range names {
		w := len(names[i])
		if len(owners[i]) > w {
			w = len(owners[i])
		}
		// Icons are typically 1-2 chars visually (emoji may be wider)
		if 2 > w {
			w = 2
		}
		widths[i] = w
	}

	// Print stages row with arrows
	var stagesLine strings.Builder
	for i, name := range names {
		if i > 0 {
			stagesLine.WriteString(" → ")
		}
		stagesLine.WriteString(fmt.Sprintf("%-*s", widths[i], name))
	}
	cmd.Println("  " + stagesLine.String())

	// Print icons row
	var iconsLine strings.Builder
	for i, icon := range icons {
		if i > 0 {
			iconsLine.WriteString("   ") // align with " → "
		}
		iconsLine.WriteString(fmt.Sprintf("%-*s", widths[i], icon))
	}
	cmd.Println("  " + iconsLine.String())

	// Print owners row
	var ownersLine strings.Builder
	for i, owner := range owners {
		if i > 0 {
			ownersLine.WriteString("   ")
		}
		ownersLine.WriteString(fmt.Sprintf("%-*s", widths[i], owner))
	}
	cmd.Println("  " + ownersLine.String())
}

func printPipelineVerbose(cmd *cobra.Command, resolved *pipeline.ResolvedPipeline, noIcons bool) {
	for i, stage := range resolved.Stages {
		icon := stage.Icon
		if icon == "" || noIcons {
			icon = "○"
		}

		optional := ""
		if stage.Optional {
			optional = " [optional]"
		}

		cmd.Printf("┌─ %s %s%s\n", stage.Name, icon, optional)
		cmd.Printf("│  Owner: %s\n", stage.GetOwner())

		// Gates
		if len(stage.Gates) > 0 {
			cmd.Println("│  Gates:")
			for _, gate := range stage.Gates {
				cmd.Printf("│    • %s: %s\n", gate.Type(), gate.Value())
			}
		} else {
			cmd.Println("│  Gates: none")
		}

		// Warnings
		if len(stage.Warnings) > 0 {
			cmd.Println("│  Warnings:")
			for _, w := range stage.Warnings {
				cmd.Printf("│    • after %s: %s\n", w.After, w.Message)
			}
		}

		// Transition effects
		if len(stage.Transitions.Advance.Effects) > 0 {
			cmd.Print("│  On advance: ")
			var effects []string
			for _, e := range stage.Transitions.Advance.Effects {
				effects = append(effects, describeEffect(e))
			}
			cmd.Println(strings.Join(effects, ", "))
		}

		if len(stage.Transitions.Revert.Effects) > 0 {
			cmd.Print("│  On revert: ")
			var effects []string
			for _, e := range stage.Transitions.Revert.Effects {
				effects = append(effects, describeEffect(e))
			}
			if len(stage.Transitions.Revert.Require) > 0 {
				effects = append(effects, fmt.Sprintf("require %s", strings.Join(stage.Transitions.Revert.Require, ", ")))
			}
			cmd.Println(strings.Join(effects, ", "))
		}

		cmd.Println("└" + strings.Repeat("─", 60))

		// Arrow to next stage
		if i < len(resolved.Stages)-1 {
			cmd.Println("          │")
			cmd.Println("          ▼")
		}
	}
}

func describeEffect(e config.EffectConfig) string {
	switch {
	case e.Notify != nil:
		if e.Notify.Target != "" {
			return fmt.Sprintf("notify %s", e.Notify.Target)
		}
		if len(e.Notify.Targets) > 0 {
			return fmt.Sprintf("notify %s", strings.Join(e.Notify.Targets, ", "))
		}
		return "notify"
	case e.Sync != "":
		return fmt.Sprintf("sync %s", e.Sync)
	case e.LogDecision != "":
		return "log decision"
	case e.Increment != "":
		return fmt.Sprintf("increment %s", e.Increment)
	case e.Archive:
		return "archive"
	case e.Webhook != nil:
		return "webhook"
	case e.Trigger != "":
		return fmt.Sprintf("trigger %s", e.Trigger)
	default:
		return "effect"
	}
}

func runPipelinePresets(cmd *cobra.Command, args []string) error {
	cmd.Println("Available pipeline presets:")
	cmd.Println()

	for _, name := range pipeline.PresetNames() {
		desc, features, stages, err := pipeline.PresetInfo(name)
		if err != nil {
			continue
		}

		cmd.Printf("  %s\n", name)
		cmd.Printf("  %s\n", desc)
		cmd.Println()

		// Show stages flow
		cmd.Printf("    Stages: %s\n", strings.Join(stages, " → "))
		cmd.Println()

		// Show features
		cmd.Println("    Features:")
		for _, f := range features {
			cmd.Printf("      • %s\n", f)
		}
		cmd.Println()
	}

	cmd.Println("To use a preset, run 'spec init' and select it,")
	cmd.Println("or add 'pipeline: { preset: <name> }' to spec.config.yaml")

	return nil
}

func runPipelineExport(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec init' first")
	}

	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		return fmt.Errorf("resolving pipeline: %w", err)
	}

	// Output as YAML-like format
	cmd.Println("# Fully resolved pipeline configuration")
	if resolved.PresetName != "" {
		cmd.Printf("# Base preset: %s\n", resolved.PresetName)
	}
	if len(resolved.SkippedStages) > 0 {
		cmd.Printf("# Skipped: %s\n", strings.Join(resolved.SkippedStages, ", "))
	}
	cmd.Println()
	cmd.Println("pipeline:")
	cmd.Println("  stages:")

	for _, stage := range resolved.Stages {
		cmd.Printf("    - name: %s\n", stage.Name)
		cmd.Printf("      owner: %s\n", stage.GetOwner())
		if stage.Icon != "" {
			cmd.Printf("      icon: %s\n", stage.Icon)
		}
		if stage.Optional {
			cmd.Println("      optional: true")
		}
		if stage.SkipWhen != "" {
			cmd.Printf("      skip_when: %q\n", stage.SkipWhen)
		}
		if len(stage.Gates) > 0 {
			cmd.Println("      gates:")
			for _, g := range stage.Gates {
				switch g.Type() {
				case "section_not_empty", "section_complete":
					cmd.Printf("        - section_not_empty: %s\n", g.Value())
				case "pr_stack_exists":
					cmd.Println("        - pr_stack_exists: true")
				case "prs_approved":
					cmd.Println("        - prs_approved: true")
				case "duration":
					cmd.Printf("        - duration: %s\n", g.Value())
				case "expr":
					cmd.Printf("        - expr: %q\n", g.Expr)
					if g.Message != "" {
						cmd.Printf("          message: %q\n", g.Message)
					}
				}
			}
		}
		if len(stage.Warnings) > 0 {
			cmd.Println("      warnings:")
			for _, w := range stage.Warnings {
				cmd.Printf("        - after: %s\n", w.After)
				cmd.Printf("          message: %q\n", w.Message)
				if w.Notify != "" {
					cmd.Printf("          notify: %s\n", w.Notify)
				}
			}
		}
		if stage.AutoArchive {
			cmd.Println("      auto_archive: true")
		}
	}

	return nil
}
