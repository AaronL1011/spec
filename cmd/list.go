package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/tui"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List specs filtered by role queue",
	Long: `List specs from the team pipeline with role-based and ownership views.

Use default mode to see specs awaiting your role, --mine to focus on your
work, --all for a stage-grouped pipeline view, and --triage to inspect
unpromoted intake items.`,
	Example: "  spec list\n  spec list --mine\n  spec list --all\n  spec list --triage\n  spec list --parent SPEC-004",
	RunE:    runList,
}

func init() {
	listCmd.Flags().Bool("all", false, "show all specs across all roles and stages")
	listCmd.Flags().Bool("mine", false, "show only specs you own")
	listCmd.Flags().String("role", "", "view from another role's perspective")
	listCmd.Flags().Bool("triage", false, "show open triage items")
	listCmd.Flags().String("parent", "", "show only the deliverable slices of an initiative spec")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	showAll, _ := cmd.Flags().GetBool("all")
	showMine, _ := cmd.Flags().GetBool("mine")
	roleFilter, _ := cmd.Flags().GetString("role")
	showTriage, _ := cmd.Flags().GetBool("triage")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	if showTriage {
		return listTriage(p, rc)
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

	if parentFilter := normalizeSpecID(mustString(cmd, "parent")); parentFilter != "" {
		return listSlices(p, rc, parentFilter)
	}
	if showMine {
		return listMine(p, specs, rc.UserName())
	}
	if showAll {
		return listAllByStage(p, specs, pipeline)
	}
	return listByRole(p, specs, pipeline, userRole)
}

func listTriage(p *printer, rc *config.ResolvedConfig) error {
	triageFiles, err := gitpkg.ListTriageFiles(&rc.Team.SpecsRepo)
	if err != nil {
		return err
	}

	type triageItem struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Priority string `json:"priority"`
	}
	var items []triageItem
	for _, f := range triageFiles {
		path := gitpkg.TriageFilePath(&rc.Team.SpecsRepo, f)
		meta, err := markdown.ReadTriageMeta(path)
		if err != nil {
			continue
		}
		items = append(items, triageItem{ID: meta.ID, Title: meta.Title, Priority: meta.Priority})
	}

	if p.JSONEnabled() {
		return p.JSON(items)
	}
	if len(items) == 0 {
		p.Line("✓ No open triage items.")
		return nil
	}
	p.Line("Open triage items:\n")
	for _, it := range items {
		p.Line("  %s %s  %s  [%s]", priorityIndicator(it.Priority), it.ID, it.Title, it.Priority)
	}
	return nil
}

type specSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Owner     string `json:"owner"`
	Blocked   bool   `json:"blocked"`
	Steps     int    `json:"steps"`
	StepsDone int    `json:"steps_done"`

	// Bounty is the spec's bounty when it carries one. Present in JSON so
	// scripts see the same shape the spec file stores.
	Bounty *markdown.BountyState `json:"bounty,omitempty"`
}

// marker returns the leading glyph for a spec row in plain output: the bounty
// bounty glyph when the spec is bountied, otherwise a blank of the same width so
// columns stay aligned whether or not a bounty is present.
func (s specSummary) marker() string {
	if s.Bounty != nil {
		return tui.IconBounty
	}
	return " "
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
			Bounty:    meta.Bounty,
		})
	}
	return specs, nil
}

func listByRole(p *printer, specs []specSummary, pipeline config.PipelineConfig, role string) error {
	var matching []specSummary
	for _, s := range specs {
		stage := pipeline.StageByName(s.Status)
		if stage != nil && stage.HasOwner(role) {
			matching = append(matching, s)
		}
	}

	if p.JSONEnabled() {
		return p.JSON(matching)
	}
	if len(matching) == 0 {
		p.Line("✓ Nothing awaiting your action. Run 'spec list --all' to see the full pipeline.")
		return nil
	}
	p.Line("Specs awaiting %s action:\n", role)
	for _, s := range matching {
		p.Line("  %s %-10s  %-40s  [%s]", s.marker(), s.ID, truncate(s.Title, 40), s.Status)
	}
	return nil
}

func listAllByStage(p *printer, specs []specSummary, pipeline config.PipelineConfig) error {
	if p.JSONEnabled() {
		return p.JSON(specs)
	}
	if len(specs) == 0 {
		p.Line("✓ No specs in the pipeline.")
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
		p.Line("─── %s (%s) ───", strings.ToUpper(stage.Name), stage.OwnerRole)
		for _, s := range items {
			p.Line("  %s %-10s  %s", s.marker(), s.ID, s.Title)
		}
		p.Line("")
	}

	// Show blocked separately
	if items := byStage["blocked"]; len(items) > 0 {
		p.Line("─── BLOCKED ───")
		for _, s := range items {
			p.Line("  🚫 %-10s  %s", s.ID, s.Title)
		}
		p.Line("")
	}

	p.Line("%d specs in pipeline.", len(specs))
	return nil
}

// listSlices renders an initiative's deliverable slices with its rollup. An
// initiative that has no slices is reported as such rather than as an empty
// list, because "nothing is linked yet" and "everything is filtered out" are
// different problems with different fixes.
func listSlices(p *printer, rc *config.ResolvedConfig, parentID string) error {
	tree := specHierarchyView(rc, parentID, "")

	type sliceRow struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	rows := make([]sliceRow, 0, len(tree.Children))
	for _, c := range tree.Children {
		rows = append(rows, sliceRow{ID: c.ID, Title: c.Title, Status: c.Status})
	}

	if p.JSONEnabled() {
		return p.JSON(rows)
	}
	if len(rows) == 0 {
		p.Line("%s has no slices — attach one with 'spec link <id> --parent %s'.", parentID, parentID)
		return nil
	}
	p.Line("Slices of %s: %d/%d complete\n", parentID, tree.Rollup.Complete, tree.Rollup.Total)
	for _, r := range rows {
		p.Line("  %-10s  %-40s  [%s]", r.ID, truncate(r.Title, 40), r.Status)
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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

func listMine(p *printer, specs []specSummary, userName string) error {
	var mine []specSummary
	for _, s := range specs {
		if strings.EqualFold(s.Owner, userName) {
			mine = append(mine, s)
		}
	}

	if p.JSONEnabled() {
		return p.JSON(mine)
	}
	if len(mine) == 0 {
		p.Line("✓ You don't own any specs. Run 'spec list --all' to see the full pipeline.")
		return nil
	}

	p.Line("Your specs (%d):\n", len(mine))

	// Group by status for better readability
	var needsAction, inProgress, blocked []specSummary
	for _, s := range mine {
		switch {
		case s.Blocked:
			blocked = append(blocked, s)
		case s.Status == "build" || s.Status == "engineering":
			inProgress = append(inProgress, s)
		default:
			needsAction = append(needsAction, s)
		}
	}

	if len(blocked) > 0 {
		p.Line("  ⊘ Blocked:")
		for _, s := range blocked {
			p.Line("    %-10s  %-35s  %s%s", s.ID, truncate(s.Title, 35), s.Status, stepsSuffix(s))
		}
		p.Line("")
	}

	if len(inProgress) > 0 {
		p.Line("  ▶ In Progress:")
		for _, s := range inProgress {
			p.Line("    %-10s  %-35s  %s%s", s.ID, truncate(s.Title, 35), s.Status, stepsSuffix(s))
		}
		p.Line("")
	}

	if len(needsAction) > 0 {
		p.Line("  ○ Other:")
		for _, s := range needsAction {
			p.Line("    %-10s  %-35s  %s", s.ID, truncate(s.Title, 35), s.Status)
		}
		p.Line("")
	}

	return nil
}

// stepsSuffix renders a compact step-progress suffix, or empty when none.
func stepsSuffix(s specSummary) string {
	if s.Steps == 0 {
		return ""
	}
	return fmt.Sprintf(" [%d/%d steps]", s.StepsDone, s.Steps)
}
