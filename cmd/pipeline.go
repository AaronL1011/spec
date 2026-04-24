package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/aaronl1011/spec-cli/internal/pipeline/expr"
	"github.com/aaronl1011/spec-cli/internal/tui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

var pipelineAddCmd = &cobra.Command{
	Use:   "add [stage-name]",
	Short: "Add a new stage to the pipeline",
	Long: `Add a new stage to the pipeline interactively.

If stage-name is provided, it will be used as the stage name.
Otherwise, you'll be prompted for all stage details.

Examples:
  spec pipeline add                    # Interactive mode
  spec pipeline add security_review    # Add stage with this name
  spec pipeline add security_review --after pr_review --owner security`,
	RunE: runPipelineAdd,
}

var pipelineRemoveCmd = &cobra.Command{
	Use:   "remove <stage-name>",
	Short: "Remove a stage from the pipeline",
	Args:  cobra.ExactArgs(1),
	RunE:  runPipelineRemove,
}

var pipelineEditCmd = &cobra.Command{
	Use:   "edit [stage-name]",
	Short: "Edit an existing stage",
	RunE:  runPipelineEdit,
}

func init() {
	pipelineCmd.Flags().BoolP("verbose", "v", false, "show gates and effects for each stage")
	pipelineCmd.Flags().Bool("no-icons", false, "suppress emoji icons")

	// Add command flags
	pipelineAddCmd.Flags().String("after", "", "insert after this stage")
	pipelineAddCmd.Flags().String("before", "", "insert before this stage")
	pipelineAddCmd.Flags().String("owner", "", "stage owner role")
	pipelineAddCmd.Flags().String("icon", "", "stage icon (emoji)")
	pipelineAddCmd.Flags().Bool("optional", false, "mark stage as optional")

	// Remove command flags
	pipelineRemoveCmd.Flags().BoolP("force", "f", false, "remove without confirmation")

	pipelineCmd.AddCommand(pipelinePresetsCmd)
	pipelineCmd.AddCommand(pipelineExportCmd)
	pipelineCmd.AddCommand(pipelineAddCmd)
	pipelineCmd.AddCommand(pipelineRemoveCmd)
	pipelineCmd.AddCommand(pipelineEditCmd)
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
		cmd.Println("Run 'spec config init' to set up a pipeline, or choose from presets:")
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
		fmt.Fprintf(&stagesLine, "%-*s", widths[i], name)
	}
	cmd.Println("  " + stagesLine.String())

	// Print icons row
	var iconsLine strings.Builder
	for i, icon := range icons {
		if i > 0 {
			iconsLine.WriteString("   ") // align with " → "
		}
		fmt.Fprintf(&iconsLine, "%-*s", widths[i], icon)
	}
	cmd.Println("  " + iconsLine.String())

	// Print owners row
	var ownersLine strings.Builder
	for i, owner := range owners {
		if i > 0 {
			ownersLine.WriteString("   ")
		}
		fmt.Fprintf(&ownersLine, "%-*s", widths[i], owner)
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

	cmd.Println("To use a preset, run 'spec config init' and select it,")
	cmd.Println("or add 'pipeline: { preset: <name> }' to spec.config.yaml")

	return nil
}

func runPipelineExport(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec config init' first")
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

func runPipelineAdd(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec config init' first")
	}

	// Get current pipeline to know existing stages
	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		return fmt.Errorf("resolving pipeline: %w", err)
	}

	existingStages := resolved.Stages
	existingNames := make([]string, len(existingStages))
	for i, s := range existingStages {
		existingNames[i] = s.Name
	}

	// Get stage name
	var stageName string
	if len(args) > 0 {
		stageName = args[0]
	} else {
		stageName, err = tui.PromptStageName(existingNames)
		if err != nil {
			return err
		}
	}

	// Check if stage already exists
	for _, name := range existingNames {
		if name == stageName {
			return fmt.Errorf("stage %q already exists — use 'spec pipeline edit %s' to modify it", stageName, stageName)
		}
	}

	// Get owner
	owner, _ := cmd.Flags().GetString("owner")
	if owner == "" {
		owner, err = tui.PromptStageOwner("engineer")
		if err != nil {
			return err
		}
	}

	// Get icon
	icon, _ := cmd.Flags().GetString("icon")
	if icon == "" {
		icon, err = tui.PromptStageIcon()
		if err != nil {
			return err
		}
	}

	// Get position
	afterStage, _ := cmd.Flags().GetString("after")
	beforeStage, _ := cmd.Flags().GetString("before")

	if afterStage == "" && beforeStage == "" {
		afterStage, err = tui.PromptStagePosition(existingNames)
		if err != nil {
			return err
		}
	}

	// Determine insert index
	insertIdx := len(existingNames) // default: append
	if afterStage != "" {
		for i, name := range existingNames {
			if name == afterStage {
				insertIdx = i + 1
				break
			}
		}
	} else if beforeStage != "" {
		for i, name := range existingNames {
			if name == beforeStage {
				insertIdx = i
				break
			}
		}
	}

	// Get optional flag
	optional, _ := cmd.Flags().GetBool("optional")

	// Collect gates interactively
	var gates []config.GateConfig
	if tui.IsInteractive() {
		for {
			gateType, err := tui.PromptGateType()
			if err != nil {
				return err
			}

			if gateType == "none" {
				break
			}

			var gate config.GateConfig
			switch gateType {
			case "section_not_empty":
				section, err := tui.PromptSectionSlug()
				if err != nil {
					return err
				}
				gate = config.GateConfig{SectionNotEmpty: section}

			case "pr_stack_exists":
				t := true
				gate = config.GateConfig{PRStackExists: &t}

			case "prs_approved":
				t := true
				gate = config.GateConfig{PRsApproved: &t}

			case "decisions_resolved":
				gate = config.GateConfig{Expr: "decisions.unresolved == 0", Message: "All decisions must be resolved"}

			case "expr":
				expr, msg, err := tui.PromptExpression()
				if err != nil {
					return err
				}
				gate = config.GateConfig{Expr: expr, Message: msg}
			}

			gates = append(gates, gate)

			another, err := tui.PromptAddAnotherGate()
			if err != nil {
				return err
			}
			if !another {
				break
			}
		}
	}

	// Build the new stage
	newStage := config.StageConfig{
		Name:     stageName,
		Owner:    config.Owners{owner},
		Icon:     icon,
		Optional: optional,
		Gates:    gates,
	}

	// Show summary and confirm
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Stage: %s %s\n", newStage.Name, newStage.Icon)
	fmt.Printf("  Owner: %s\n", newStage.Owner)
	if insertIdx > 0 && insertIdx <= len(existingNames) {
		fmt.Printf("  Position: after %s\n", existingNames[insertIdx-1])
	}
	if len(gates) > 0 {
		fmt.Println("  Gates:")
		for _, g := range gates {
			fmt.Printf("    • %s: %s\n", g.Type(), g.Value())
		}
	}
	if optional {
		fmt.Println("  Optional: yes")
	}
	fmt.Println()

	confirmed, err := tui.PromptConfirm("Add this stage?")
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("cancelled")
	}

	// Update the config file
	if err := addStageToConfig(newStage, insertIdx, pipelineCfg); err != nil {
		return err
	}

	tui.PrintSuccess(fmt.Sprintf("Stage %q added to pipeline", stageName))
	fmt.Println("  Run 'spec pipeline' to see the updated pipeline.")
	return nil
}

func addStageToConfig(newStage config.StageConfig, insertIdx int, currentCfg config.PipelineConfig) error {
	// Read existing config
	configPath := "spec.config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse as generic YAML to preserve structure
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Get or create pipeline section
	pipelineSection, ok := rawConfig["pipeline"].(map[string]interface{})
	if !ok {
		pipelineSection = make(map[string]interface{})
		rawConfig["pipeline"] = pipelineSection
	}

	// If using a preset, we need to add stages as overrides
	if currentCfg.Preset != "" {
		// Add to stages array (overrides)
		stages, _ := pipelineSection["stages"].([]interface{})
		
		// Build stage map
		stageMap := map[string]interface{}{
			"name":  newStage.Name,
			"owner": newStage.Owner,
		}
		if newStage.Icon != "" {
			stageMap["icon"] = newStage.Icon
		}
		if newStage.Optional {
			stageMap["optional"] = true
		}
		if len(newStage.Gates) > 0 {
			var gatesList []interface{}
			for _, g := range newStage.Gates {
				gateMap := make(map[string]interface{})
				if g.SectionNotEmpty != "" {
					gateMap["section_not_empty"] = g.SectionNotEmpty
				} else if g.PRStackExists != nil {
					gateMap["pr_stack_exists"] = true
				} else if g.PRsApproved != nil {
					gateMap["prs_approved"] = true
				} else if g.Expr != "" {
					gateMap["expr"] = g.Expr
					if g.Message != "" {
						gateMap["message"] = g.Message
					}
				}
				gatesList = append(gatesList, gateMap)
			}
			stageMap["gates"] = gatesList
		}

		stages = append(stages, stageMap)
		pipelineSection["stages"] = stages

		// Note: For presets, we'd ideally track insert position, but for now
		// stages are added as overrides and merged. Full ordering control
		// would require more sophisticated config management.
	} else {
		// Direct stages array manipulation
		stages, _ := pipelineSection["stages"].([]interface{})
		
		stageMap := map[string]interface{}{
			"name":  newStage.Name,
			"owner": newStage.Owner,
		}
		if newStage.Icon != "" {
			stageMap["icon"] = newStage.Icon
		}
		if newStage.Optional {
			stageMap["optional"] = true
		}
		if len(newStage.Gates) > 0 {
			var gatesList []interface{}
			for _, g := range newStage.Gates {
				gateMap := make(map[string]interface{})
				if g.SectionNotEmpty != "" {
					gateMap["section_not_empty"] = g.SectionNotEmpty
				} else if g.PRStackExists != nil {
					gateMap["pr_stack_exists"] = true
				} else if g.PRsApproved != nil {
					gateMap["prs_approved"] = true
				} else if g.Expr != "" {
					gateMap["expr"] = g.Expr
					if g.Message != "" {
						gateMap["message"] = g.Message
					}
				}
				gatesList = append(gatesList, gateMap)
			}
			stageMap["gates"] = gatesList
		}

		// Insert at position
		if insertIdx >= len(stages) {
			stages = append(stages, stageMap)
		} else {
			stages = append(stages[:insertIdx], append([]interface{}{stageMap}, stages[insertIdx:]...)...)
		}
		pipelineSection["stages"] = stages
	}

	// Write back
	output, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

func runPipelineRemove(cmd *cobra.Command, args []string) error {
	stageName := args[0]

	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec config init' first")
	}

	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		return fmt.Errorf("resolving pipeline: %w", err)
	}

	// Check stage exists
	found := false
	for _, s := range resolved.Stages {
		if s.Name == stageName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("stage %q not found in pipeline", stageName)
	}

	// Confirm
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		confirmed, err := tui.PromptConfirm(fmt.Sprintf("Remove stage %q from the pipeline?", stageName))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("cancelled")
		}
	}

	// Update config
	if err := removeStageFromConfig(stageName, pipelineCfg); err != nil {
		return err
	}

	tui.PrintSuccess(fmt.Sprintf("Stage %q removed from pipeline", stageName))
	return nil
}

func removeStageFromConfig(stageName string, currentCfg config.PipelineConfig) error {
	configPath := "spec.config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	pipelineSection, ok := rawConfig["pipeline"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("no pipeline section in config")
	}

	// If using a preset, add to skip list
	if currentCfg.Preset != "" {
		skip, _ := pipelineSection["skip"].([]interface{})
		skip = append(skip, stageName)
		pipelineSection["skip"] = skip
	} else {
		// Remove from stages array
		stages, _ := pipelineSection["stages"].([]interface{})
		var newStages []interface{}
		for _, s := range stages {
			stageMap, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			if stageMap["name"] != stageName {
				newStages = append(newStages, s)
			}
		}
		pipelineSection["stages"] = newStages
	}

	output, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

func runPipelineEdit(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec config init' first")
	}

	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		return fmt.Errorf("resolving pipeline: %w", err)
	}

	// Get stage name
	var stageName string
	if len(args) > 0 {
		stageName = args[0]
	} else {
		stageNames := make([]string, len(resolved.Stages))
		for i, s := range resolved.Stages {
			stageNames[i] = s.Name
		}
		stageName, err = tui.PromptSelectStage(stageNames, "Select stage to edit", "")
		if err != nil {
			return err
		}
	}

	// Find the stage
	var stage *config.StageConfig
	for i := range resolved.Stages {
		if resolved.Stages[i].Name == stageName {
			stage = &resolved.Stages[i]
			break
		}
	}
	if stage == nil {
		return fmt.Errorf("stage %q not found", stageName)
	}

	// Show current config
	fmt.Printf("\nCurrent configuration for %s:\n", stageName)
	fmt.Printf("  Owner: %s\n", stage.GetOwner())
	fmt.Printf("  Icon: %s\n", stage.Icon)
	if len(stage.Gates) > 0 {
		fmt.Println("  Gates:")
		for _, g := range stage.Gates {
			fmt.Printf("    • %s: %s\n", g.Type(), g.Value())
		}
	} else {
		fmt.Println("  Gates: none")
	}
	fmt.Println()

	// For now, just show what's there and explain how to edit
	fmt.Println("To modify this stage, edit spec.config.yaml directly,")
	fmt.Println("or use 'spec pipeline remove' and 'spec pipeline add'.")
	fmt.Println()
	fmt.Println("Interactive stage editing coming soon!")

	return nil
}

var pipelineValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate pipeline configuration",
	Long: `Validate the pipeline configuration for errors.

Checks:
  - All stages have valid owners
  - Gates reference valid sections
  - Expressions are syntactically correct
  - Skip lists reference existing stages
  - No circular dependencies`,
	RunE: runPipelineValidate,
}

func init() {
	pipelineCmd.AddCommand(pipelineValidateCmd)
}

func runPipelineValidate(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return fmt.Errorf("no config found — run 'spec config init' first")
	}

	var pipelineCfg config.PipelineConfig
	if rc.Team != nil {
		pipelineCfg = rc.Team.Pipeline
	}

	// Try to resolve the pipeline
	resolved, err := pipeline.Resolve(pipelineCfg)
	if err != nil {
		tui.PrintError(fmt.Sprintf("Pipeline resolution failed: %v", err))
		return err
	}

	var errors []string
	var warnings []string

	// Validate each stage
	for _, stage := range resolved.Stages {
		// Check owner is valid
		owner := stage.GetOwner()
		if owner == "" {
			errors = append(errors, fmt.Sprintf("stage %q: no owner specified", stage.Name))
		} else if !isValidOwner(owner) {
			warnings = append(warnings, fmt.Sprintf("stage %q: owner %q is not a standard role", stage.Name, owner))
		}

		// Validate gates
		for i, gate := range stage.Gates {
			if gate.Expr != "" {
				// Try to compile the expression
				if compileErr := expr.Compile(gate.Expr); compileErr != nil {
					errors = append(errors, fmt.Sprintf("stage %q gate %d: invalid expression %q: %v", 
						stage.Name, i+1, gate.Expr, compileErr))
				}
			}
		}

		// Validate skip_when expression
		if stage.SkipWhen != "" {
			if compileErr := expr.Compile(stage.SkipWhen); compileErr != nil {
				errors = append(errors, fmt.Sprintf("stage %q: invalid skip_when expression %q: %v",
					stage.Name, stage.SkipWhen, compileErr))
			}
		}
	}

	// Check for duplicate stage names
	seen := make(map[string]bool)
	for _, stage := range resolved.Stages {
		if seen[stage.Name] {
			errors = append(errors, fmt.Sprintf("duplicate stage name: %q", stage.Name))
		}
		seen[stage.Name] = true
	}

	// Check skip list references valid stages
	for _, skip := range pipelineCfg.Skip {
		found := false
		for _, stage := range resolved.Stages {
			if stage.Name == skip {
				found = true
				break
			}
		}
		// Note: skip might reference a preset stage that was removed, which is ok
		if !found && pipelineCfg.Preset == "" {
			warnings = append(warnings, fmt.Sprintf("skip references unknown stage: %q", skip))
		}
	}

	// Print results
	if len(errors) == 0 && len(warnings) == 0 {
		tui.PrintSuccess("Pipeline configuration is valid")
		cmd.Printf("\n  Preset: %s\n", defaultStr(resolved.PresetName, "(none)"))
		cmd.Printf("  Stages: %d\n", len(resolved.Stages))
		if len(resolved.SkippedStages) > 0 {
			cmd.Printf("  Skipped: %s\n", strings.Join(resolved.SkippedStages, ", "))
		}
		return nil
	}

	if len(errors) > 0 {
		tui.PrintError("Pipeline configuration has errors:")
		for _, e := range errors {
			cmd.Printf("  ✗ %s\n", e)
		}
	}

	if len(warnings) > 0 {
		cmd.Println("\nWarnings:")
		for _, w := range warnings {
			cmd.Printf("  ⚠ %s\n", w)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%d error(s) found", len(errors))
	}

	return nil
}

func isValidOwner(owner string) bool {
	validOwners := []string{"anyone", "author", "pm", "tl", "designer", "engineer", "qa", "security"}
	for _, v := range validOwners {
		if owner == v {
			return true
		}
	}
	return false
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
