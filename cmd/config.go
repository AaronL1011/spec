package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/tui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage spec configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive wizard for configuration setup",
	RunE:  runConfigInit,
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Validate all configured integrations",
	RunE:  runConfigTest,
}

func init() {
	configInitCmd.Flags().Bool("user", false, "initialise personal user config (~/.spec/config.yaml)")
	configInitCmd.Flags().String("preset", "", "pipeline preset (minimal, startup, product, platform, kanban)")
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configTestCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	isUser, _ := cmd.Flags().GetBool("user")

	if isUser {
		return runUserConfigInit()
	}
	return runTeamConfigInit(cmd)
}

func runUserConfigInit() error {
	reader := bufio.NewReader(os.Stdin)
	cfg := &config.UserConfig{}
	aiDrafts := true
	cfg.Preferences.AIDrafts = &aiDrafts

	fmt.Println("Setting up your personal spec identity (~/.spec/config.yaml)")
	fmt.Println()

	// Name
	fmt.Print("Your name: ")
	name, _ := reader.ReadString('\n')
	cfg.User.Name = strings.TrimSpace(name)

	// Role
	fmt.Printf("Your role (%s): ", strings.Join(config.ValidRoles(), " | "))
	role, _ := reader.ReadString('\n')
	role = strings.TrimSpace(strings.ToLower(role))
	if !config.IsValidRole(role) {
		return fmt.Errorf("invalid role %q — must be one of: %s", role, strings.Join(config.ValidRoles(), ", "))
	}
	cfg.User.OwnerRole = role

	// Handle
	fmt.Print("Your comms handle (e.g., @aaron or aaron@org.com): ")
	handle, _ := reader.ReadString('\n')
	cfg.User.Handle = strings.TrimSpace(handle)

	// Editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	fmt.Printf("Preferred editor [%s]: ", editor)
	editorInput, _ := reader.ReadString('\n')
	editorInput = strings.TrimSpace(editorInput)
	if editorInput != "" {
		editor = editorInput
	}
	cfg.Preferences.Editor = editor

	path := config.UserConfigPath()
	if err := config.WriteUserConfig(path, cfg); err != nil {
		return err
	}

	fmt.Printf("\n✓ User config written to %s\n", path)
	return nil
}

func runTeamConfigInit(cmd *cobra.Command) error {
	reader := bufio.NewReader(os.Stdin)

	tui.PrintTitle("Welcome to spec")
	fmt.Println()
	fmt.Println("Setting up team configuration (spec.config.yaml)")
	fmt.Println()

	// Select pipeline preset
	var presetName string
	presetFlag, _ := cmd.Flags().GetString("preset")
	if presetFlag != "" {
		presetName = presetFlag
	} else if tui.IsInteractive() {
		// Build preset options from available presets
		var presetOptions []tui.PresetOption
		for _, name := range pipeline.PresetNames() {
			desc, features, stages, _ := pipeline.PresetInfo(name)
			presetOptions = append(presetOptions, tui.PresetOption{
				Name:        name,
				Description: desc,
				Stages:      stages,
				Features:    features,
			})
		}

		selected, err := tui.SelectPreset(presetOptions)
		if err != nil {
			return err
		}
		presetName = selected

		// Show preview and confirm
		for _, p := range presetOptions {
			if p.Name == selected {
				confirmed, err := tui.ConfirmPreset(p)
				if err != nil {
					return err
				}
				if !confirmed {
					return fmt.Errorf("cancelled")
				}
				break
			}
		}
	} else {
		// Non-interactive: use minimal preset
		presetName = "minimal"
		fmt.Printf("Using preset: %s (use --preset to specify)\n", presetName)
	}

	fmt.Println()

	// Team name
	fmt.Print("Team name: ")
	teamName, _ := reader.ReadString('\n')
	teamName = strings.TrimSpace(teamName)

	// Cycle label
	fmt.Print("Current cycle label (e.g., Cycle 7): ")
	cycleLabel, _ := reader.ReadString('\n')
	cycleLabel = strings.TrimSpace(cycleLabel)

	// Specs repo provider
	fmt.Print("Specs repo provider (github | gitlab | bitbucket) [github]: ")
	repoProvider, _ := reader.ReadString('\n')
	repoProvider = strings.TrimSpace(strings.ToLower(repoProvider))
	if repoProvider == "" {
		repoProvider = "github"
	}

	// Specs repo owner
	fmt.Print("Specs repo owner/org: ")
	repoOwner, _ := reader.ReadString('\n')
	repoOwner = strings.TrimSpace(repoOwner)

	// Specs repo name
	fmt.Print("Specs repo name [specs]: ")
	repoName, _ := reader.ReadString('\n')
	repoName = strings.TrimSpace(repoName)
	if repoName == "" {
		repoName = "specs"
	}

	content := fmt.Sprintf(`version: "1"

team:
  name: %q
  cycle_label: %q

specs_repo:
  provider: %s
  owner: %s
  repo: %s
  branch: main
  token: ${GITHUB_TOKEN}

integrations:
  comms:
    provider: none
  pm:
    provider: none
  docs:
    provider: none
  repo:
    provider: %s
    owner: %s
    token: ${GITHUB_TOKEN}
  agent:
    provider: none
  ai:
    provider: none
  design:
    provider: none
  deploy:
    provider: none

sync:
  outbound_on_advance: true
  conflict_strategy: warn

archive:
  directory: archive

dashboard:
  stale_threshold: 48h
  refresh_ttl: 300

pipeline:
  preset: %s
`, teamName, cycleLabel, repoProvider, repoOwner, repoName, repoProvider, repoOwner, presetName)

	path := "spec.config.yaml"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing team config: %w", err)
	}

	fmt.Println()
	tui.PrintSuccess(fmt.Sprintf("Team config written to %s", path))
	fmt.Println("  Edit the integrations section to connect your tools.")
	fmt.Println("  Run 'spec pipeline' to see your pipeline.")
	fmt.Println("  Run 'spec pipeline add/edit' to customise stages.")
	return nil
}

func runConfigTest(cmd *cobra.Command, args []string) error {
	rc, err := config.Resolve()
	if err != nil {
		return err
	}

	fmt.Println("Configuration test results:")
	fmt.Println()

	// User config
	if rc.User != nil && rc.User.User.OwnerRole != "" {
		fmt.Printf("  ✓ User config: %s (role: %s)\n", rc.UserConfigPath, rc.User.User.OwnerRole)
	} else {
		fmt.Printf("  ✗ User config: not configured — run 'spec config init --user'\n")
	}

	// Team config
	if rc.Team != nil {
		fmt.Printf("  ✓ Team config: %s (team: %s)\n", rc.TeamConfigPath, rc.Team.Team.Name)
	} else {
		fmt.Printf("  ✗ Team config: not found — run 'spec config init'\n")
	}

	if rc.Team == nil {
		return nil
	}

	// Check integrations
	categories := []struct {
		name     string
		category string
	}{
		{"Comms", "comms"},
		{"PM", "pm"},
		{"Docs", "docs"},
		{"Repo", "repo"},
		{"Agent", "agent"},
		{"AI", "ai"},
		{"Design", "design"},
		{"Deploy", "deploy"},
	}

	fmt.Println()
	fmt.Println("  Integrations:")
	for _, cat := range categories {
		if rc.HasIntegration(cat.category) {
			fmt.Printf("    ✓ %s: configured\n", cat.name)
		} else {
			fmt.Printf("    · %s: not configured\n", cat.name)
		}
	}

	return nil
}
