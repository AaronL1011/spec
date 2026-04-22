// Package tui provides interactive terminal UI components for spec.
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Common icons for stages
var StageIcons = []string{"📥", "📝", "👀", "🎨", "🔧", "🏗️", "👁️", "✅", "🚀", "📊", "🎉", "📦", "🔒", "💬", "📋", "○"}

// Common roles
var CommonRoles = []string{"anyone", "author", "pm", "tl", "designer", "engineer", "qa", "security"}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
)

// IsInteractive returns true if stdin is a terminal.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// PresetOption represents a pipeline preset for selection.
type PresetOption struct {
	Name        string
	Description string
	Stages      []string
	Features    []string
}

// SelectPreset prompts the user to select a pipeline preset.
func SelectPreset(presets []PresetOption) (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("not a terminal — use --preset flag to specify preset")
	}

	options := make([]huh.Option[string], len(presets))
	for i, p := range presets {
		label := fmt.Sprintf("%-12s %s", p.Name, p.Description)
		options[i] = huh.NewOption(label, p.Name)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How does your team work?").
				Description("Select a pipeline preset that matches your workflow").
				Options(options...).
				Value(&selected),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return selected, nil
}

// ConfirmPreset shows a preset preview and asks for confirmation.
func ConfirmPreset(preset PresetOption) (bool, error) {
	if !IsInteractive() {
		return true, nil
	}

	// Build preview
	var preview strings.Builder
	preview.WriteString(titleStyle.Render(preset.Name) + "\n")
	preview.WriteString(subtitleStyle.Render(preset.Description) + "\n\n")
	preview.WriteString("Stages: " + strings.Join(preset.Stages, " → ") + "\n\n")
	preview.WriteString("Features:\n")
	for _, f := range preset.Features {
		preview.WriteString("  • " + f + "\n")
	}

	fmt.Println(preview.String())

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Use this preset?").
				Affirmative("Yes").
				Negative("No, go back").
				Value(&confirmed),
		),
	)

	err := form.Run()
	if err != nil {
		return false, err
	}

	return confirmed, nil
}

// StageInput holds input for creating a new stage.
type StageInput struct {
	Name     string
	Owner    string
	Icon     string
	Position string // "after:<stage>" or "before:<stage>"
}

// PromptStageName prompts for a stage name.
func PromptStageName(existing []string) (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("not a terminal — use --name flag")
	}

	existingSet := make(map[string]bool)
	for _, s := range existing {
		existingSet[s] = true
	}

	var name string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Stage name").
				Description("Lowercase, underscores allowed (e.g., security_review)").
				Placeholder("stage_name").
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("name is required")
					}
					if existingSet[s] {
						return fmt.Errorf("stage %q already exists", s)
					}
					if strings.ContainsAny(s, " -") {
						return fmt.Errorf("use underscores instead of spaces or dashes")
					}
					return nil
				}).
				Value(&name),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return name, nil
}

// PromptStageOwner prompts for a stage owner role.
func PromptStageOwner(defaultOwner string) (string, error) {
	if !IsInteractive() {
		return defaultOwner, nil
	}

	options := make([]huh.Option[string], len(CommonRoles))
	for i, role := range CommonRoles {
		options[i] = huh.NewOption(role, role)
	}

	var owner string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Owner role").
				Description("Who owns this stage?").
				Options(options...).
				Value(&owner),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return owner, nil
}

// PromptStageIcon prompts for a stage icon.
func PromptStageIcon() (string, error) {
	if !IsInteractive() {
		return "○", nil
	}

	options := make([]huh.Option[string], len(StageIcons))
	for i, icon := range StageIcons {
		options[i] = huh.NewOption(icon, icon)
	}

	var icon string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Icon").
				Description("Displayed in pipeline views").
				Options(options...).
				Value(&icon),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return icon, nil
}

// PromptStagePosition prompts for where to insert a new stage.
func PromptStagePosition(stages []string) (afterStage string, err error) {
	if !IsInteractive() {
		return "", fmt.Errorf("not a terminal — use --after or --before flag")
	}

	options := make([]huh.Option[string], len(stages))
	for i, s := range stages {
		label := fmt.Sprintf("After %s", s)
		options[i] = huh.NewOption(label, s)
	}

	var position string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Position").
				Description("Where should this stage be inserted?").
				Options(options...).
				Value(&position),
		),
	)

	err = form.Run()
	if err != nil {
		return "", err
	}

	return position, nil
}

// GateType represents a type of gate.
type GateType struct {
	Name        string
	Description string
	Value       string
}

// CommonGateTypes are the built-in gate types.
var CommonGateTypes = []GateType{
	{Name: "Section has content", Description: "Require a section to be non-empty", Value: "section_not_empty"},
	{Name: "PR stack exists", Description: "Require PR stack plan in §7.3", Value: "pr_stack_exists"},
	{Name: "PRs approved", Description: "All PRs must be approved", Value: "prs_approved"},
	{Name: "Decisions resolved", Description: "All decisions must be resolved", Value: "decisions_resolved"},
	{Name: "Custom expression", Description: "Write a custom expression", Value: "expr"},
	{Name: "No gate", Description: "Skip adding a gate", Value: "none"},
}

// PromptGateType prompts the user to select a gate type.
func PromptGateType() (string, error) {
	if !IsInteractive() {
		return "none", nil
	}

	options := make([]huh.Option[string], len(CommonGateTypes))
	for i, g := range CommonGateTypes {
		label := fmt.Sprintf("%-25s %s", g.Name, subtitleStyle.Render(g.Description))
		options[i] = huh.NewOption(label, g.Value)
	}

	var gateType string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Gate type").
				Description("What must be true to advance from this stage?").
				Options(options...).
				Value(&gateType),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return gateType, nil
}

// PromptSectionSlug prompts for a section slug.
func PromptSectionSlug() (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("not a terminal — use --section flag")
	}

	commonSections := []string{
		"problem_statement",
		"goals_non_goals",
		"user_stories",
		"proposed_solution",
		"design_inputs",
		"acceptance_criteria",
		"technical_implementation",
	}

	options := make([]huh.Option[string], len(commonSections))
	for i, s := range commonSections {
		options[i] = huh.NewOption(s, s)
	}

	var section string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Section").
				Description("Which section must have content?").
				Options(options...).
				Value(&section),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return section, nil
}

// PromptExpression prompts for a custom expression.
func PromptExpression() (string, string, error) {
	if !IsInteractive() {
		return "", "", fmt.Errorf("not a terminal — use --expr flag")
	}

	var expression, message string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Expression").
				Description("e.g., decisions.unresolved == 0").
				Placeholder("expression").
				Value(&expression),
			huh.NewInput().
				Title("Failure message").
				Description("Shown when the gate fails (optional)").
				Placeholder("message").
				Value(&message),
		),
	)

	err := form.Run()
	if err != nil {
		return "", "", err
	}

	return expression, message, nil
}

// PromptAddAnotherGate asks if the user wants to add another gate.
func PromptAddAnotherGate() (bool, error) {
	if !IsInteractive() {
		return false, nil
	}

	var another bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add another gate?").
				Affirmative("Yes").
				Negative("No").
				Value(&another),
		),
	)

	err := form.Run()
	if err != nil {
		return false, err
	}

	return another, nil
}

// PromptConfirm asks for yes/no confirmation.
func PromptConfirm(title string) (bool, error) {
	if !IsInteractive() {
		return false, fmt.Errorf("not a terminal — use -y flag to confirm")
	}

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative("Yes").
				Negative("No").
				Value(&confirmed),
		),
	)

	err := form.Run()
	if err != nil {
		return false, err
	}

	return confirmed, nil
}

// PromptSelectStage prompts to select a stage from a list.
func PromptSelectStage(stages []string, title, description string) (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("not a terminal — specify stage as argument")
	}

	options := make([]huh.Option[string], len(stages))
	for i, s := range stages {
		options[i] = huh.NewOption(s, s)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(description).
				Options(options...).
				Value(&selected),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return selected, nil
}

// PromptMultiSelectStages prompts to select multiple stages.
func PromptMultiSelectStages(stages []string, title, description string) ([]string, error) {
	if !IsInteractive() {
		return nil, fmt.Errorf("not a terminal — use flags")
	}

	options := make([]huh.Option[string], len(stages))
	for i, s := range stages {
		options[i] = huh.NewOption(s, s)
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(title).
				Description(description).
				Options(options...).
				Value(&selected),
		),
	)

	err := form.Run()
	if err != nil {
		return nil, err
	}

	return selected, nil
}

// PrintSuccess prints a success message.
func PrintSuccess(message string) {
	fmt.Println(successStyle.Render("✓ " + message))
}

// PrintError prints an error message.
func PrintError(message string) {
	fmt.Println(errorStyle.Render("✗ " + message))
}

// PrintTitle prints a title.
func PrintTitle(title string) {
	fmt.Println(titleStyle.Render(title))
}
