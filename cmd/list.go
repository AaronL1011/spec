package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List specs filtered by role queue",
	Long: `List specs from the team pipeline with role-based and ownership views.

Use default mode to see specs awaiting your role, --mine to focus on your
work, --all for a stage-grouped pipeline view, and --triage to inspect
unpromoted intake items.`,
	Example: "  spec list\n  spec list --mine\n  spec list --all\n  spec list --triage",
	RunE:  runList,
}

func init() {
	listCmd.Flags().Bool("all", false, "show all specs across all roles and stages")
	listCmd.Flags().Bool("mine", false, "show only specs you own")
	listCmd.Flags().String("role", "", "view from another role's perspective")
	listCmd.Flags().Bool("triage", false, "show open triage items")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	showAll, _ := cmd.Flags().GetBool("all")
	showMine, _ := cmd.Flags().GetBool("mine")
	roleFilter, _ := cmd.Flags().GetString("role")
	showTriage, _ := cmd.Flags().GetBool("triage")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if showTriage {
		return listTriage(rc)
	}

	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	// Ensure specs repo is fresh
	if _, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo); err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	pipeline := rc.Pipeline()

	// Determine the user's role
	userRole := roleFilter
	if userRole == "" {
		var err error
		userRole, err = requireRole(rc)
		if err != nil {
			return err
		}
	}

	// Read all specs
	specs, err := loadAllSpecs(rc)
	if err != nil {
		return err
	}

	// Filter to user's owned specs if --mine
	if showMine {
		return listMine(specs, pipeline, rc.UserName())
	}

	if showAll {
		return listAllByStage(specs, pipeline)
	}

	return listByRole(specs, pipeline, userRole)
}

func listTriage(rc *config.ResolvedConfig) error {
	triageFiles, err := gitpkg.ListTriageFiles(&rc.Team.SpecsRepo)
	if err != nil {
		return err
	}

	if len(triageFiles) == 0 {
		fmt.Println("✓ No open triage items.")
		return nil
	}

	fmt.Println("Open triage items:")
	fmt.Println()
	for _, f := range triageFiles {
		path := gitpkg.TriageFilePath(&rc.Team.SpecsRepo, f)
		meta, err := markdown.ReadTriageMeta(path)
		if err != nil {
			continue
		}
		priorityIcon := priorityIndicator(meta.Priority)
		fmt.Printf("  %s %s  %s  [%s]\n", priorityIcon, meta.ID, meta.Title, meta.Priority)
	}
	return nil
}

type specSummary struct {
	ID      string
	Title   string
	Status  string
	Owner   string
	Blocked bool
	Steps   int
	StepsDone int
}

func loadAllSpecs(rc *config.ResolvedConfig) ([]specSummary, error) {
	specFiles, err := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	if err != nil {
		return nil, err
	}

	var specs []specSummary
	for _, f := range specFiles {
		path := filepath.Join(rc.SpecsRepoDir, f)
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}
		
		// Count steps progress
		var stepsDone, stepsTotal int
		var hasBlocked bool
		for _, step := range meta.Steps {
			stepsTotal++
			if step.Status == "complete" {
				stepsDone++
			}
			if step.Status == "blocked" {
				hasBlocked = true
			}
		}
		
		specs = append(specs, specSummary{
			ID:        meta.ID,
			Title:     meta.Title,
			Status:    meta.Status,
			Owner:     meta.Author,
			Blocked:   hasBlocked,
			Steps:     stepsTotal,
			StepsDone: stepsDone,
		})
	}
	return specs, nil
}

func listByRole(specs []specSummary, pipeline config.PipelineConfig, role string) error {
	var matching []specSummary
	for _, s := range specs {
		stage := pipeline.StageByName(s.Status)
		if stage != nil && stage.HasOwner(role) {
			matching = append(matching, s)
		}
	}

	if len(matching) == 0 {
		fmt.Printf("✓ Nothing awaiting your action. Run 'spec list --all' to see the full pipeline.\n")
		return nil
	}

	fmt.Printf("Specs awaiting %s action:\n\n", role)
	for _, s := range matching {
		fmt.Printf("  %-10s  %-40s  [%s]\n", s.ID, truncate(s.Title, 40), s.Status)
	}
	return nil
}

func listAllByStage(specs []specSummary, pipeline config.PipelineConfig) error {
	if len(specs) == 0 {
		fmt.Println("✓ No specs in the pipeline.")
		return nil
	}

	// Group by stage
	byStage := make(map[string][]specSummary)
	for _, s := range specs {
		byStage[s.Status] = append(byStage[s.Status], s)
	}

	for _, stage := range pipeline.Stages {
		items := byStage[stage.Name]
		if len(items) == 0 {
			continue
		}
		fmt.Printf("─── %s (%s) ───\n", strings.ToUpper(stage.Name), stage.OwnerRole)
		for _, s := range items {
			fmt.Printf("  %-10s  %s\n", s.ID, s.Title)
		}
		fmt.Println()
	}

	// Show blocked separately
	if items := byStage["blocked"]; len(items) > 0 {
		fmt.Printf("─── BLOCKED ───\n")
		for _, s := range items {
			fmt.Printf("  🚫 %-10s  %s\n", s.ID, s.Title)
		}
		fmt.Println()
	}

	fmt.Printf("%d specs in pipeline.\n", len(specs))
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func priorityIndicator(priority string) string {
	switch priority {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "🟢"
	default:
		return "·"
	}
}

func listMine(specs []specSummary, pipeline config.PipelineConfig, userName string) error {
	var mine []specSummary
	for _, s := range specs {
		if strings.EqualFold(s.Owner, userName) {
			mine = append(mine, s)
		}
	}

	if len(mine) == 0 {
		fmt.Printf("✓ You don't own any specs. Run 'spec list --all' to see the full pipeline.\n")
		return nil
	}

	fmt.Printf("Your specs (%d):\n\n", len(mine))

	// Group by status for better readability
	var needsAction, inProgress, blocked []specSummary
	for _, s := range mine {
		if s.Blocked {
			blocked = append(blocked, s)
		} else if s.Status == "build" || s.Status == "engineering" {
			inProgress = append(inProgress, s)
		} else {
			needsAction = append(needsAction, s)
		}
	}

	if len(blocked) > 0 {
		fmt.Println("  ⊘ Blocked:")
		for _, s := range blocked {
			progress := ""
			if s.Steps > 0 {
				progress = fmt.Sprintf(" [%d/%d steps]", s.StepsDone, s.Steps)
			}
			fmt.Printf("    %-10s  %-35s  %s%s\n", s.ID, truncate(s.Title, 35), s.Status, progress)
		}
		fmt.Println()
	}

	if len(inProgress) > 0 {
		fmt.Println("  ▶ In Progress:")
		for _, s := range inProgress {
			progress := ""
			if s.Steps > 0 {
				progress = fmt.Sprintf(" [%d/%d steps]", s.StepsDone, s.Steps)
			}
			fmt.Printf("    %-10s  %-35s  %s%s\n", s.ID, truncate(s.Title, 35), s.Status, progress)
		}
		fmt.Println()
	}

	if len(needsAction) > 0 {
		fmt.Println("  ○ Other:")
		for _, s := range needsAction {
			fmt.Printf("    %-10s  %-35s  %s\n", s.ID, truncate(s.Title, 35), s.Status)
		}
		fmt.Println()
	}

	return nil
}
