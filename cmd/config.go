package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
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
	Short: "Show resolved config and integration status (no network calls)",
	RunE:  runConfigTest,
}

var configCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Preflight live PM/Jira configuration",
	Long: `Preflight the configured PM integration against its live API. The
current implementation validates Jira authentication, project, issue types,
and board, then prints the project's workflow statuses so you can author an
accurate pm.status_map. Other integration categories are not called by this
command.`,
	RunE: runConfigCheck,
}

func init() {
	configInitCmd.Flags().Bool("user", false, "initialise personal user config (~/.spec/config.yaml)")
	configInitCmd.Flags().String("preset", "", "pipeline preset (minimal, startup, product, platform, kanban)")
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configTestCmd)
	configCmd.AddCommand(configCheckCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigCheck(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	fmt.Println("Preflight checks:")
	fmt.Println()

	if !rc.HasIntegration("pm") {
		fmt.Println("  · PM: not configured")
		return nil
	}

	reg := buildRegistry(rc)
	pm := reg.PM()
	if err := pm.Validate(ctx()); err != nil {
		fmt.Printf("  ✗ PM: %v\n", err)
		return fmt.Errorf("PM preflight failed")
	}
	fmt.Println("  ✓ PM: credentials, project, issue types, and board OK")

	// Print the live workflow statuses so the user can seed pm.status_map.
	if inspector, ok := pm.(pmWorkflowInspector); ok {
		statuses, err := inspector.WorkflowStatuses(ctx())
		if err != nil {
			warnf("could not fetch workflow statuses: %v", err)
		} else if len(statuses) > 0 {
			fmt.Println()
			fmt.Println("  Jira workflow statuses (map your pipeline stages to these in pm.status_map):")
			for _, s := range statuses {
				fmt.Printf("    - %s\n", s)
			}
		}
	}
	return nil
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

	// Handle (spec-canonical identity)
	fmt.Print("Your spec handle — how you're identified inside spec (e.g., alice): ")
	handle, _ := reader.ReadString('\n')
	cfg.User.Handle = strings.TrimSpace(handle)

	// Per-provider identities: only prompt for providers the joined team has
	// actually configured, since a handle differs on every service.
	promptProviderIdentities(reader, cfg)

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

// identityPromptCategories is the stable order in which init asks for
// per-provider handles. Each maps to the team's configured provider, if any.
var identityPromptCategories = []string{"repo", "comms", "pm", "docs", "design", "deploy"}

// promptProviderIdentities asks for the user's handle on each provider the
// joined team has configured. It is a no-op when no team config is found, and
// every prompt is optional (blank input falls back to the canonical handle).
func promptProviderIdentities(reader *bufio.Reader, cfg *config.UserConfig) {
	// Use the full resolution chain (cwd → repo root → joined clone under
	// ~/.spec/repos) so identity setup works from anywhere, not only from
	// inside a specs-repo checkout.
	rc, err := config.Resolve()
	if err != nil || rc.Team == nil {
		return // No team config — skip per-provider identity setup.
	}

	seen := make(map[string]bool)
	var prompted bool
	for _, cat := range identityPromptCategories {
		if !rc.HasIntegration(cat) {
			continue
		}
		provider := strings.ToLower(rc.IntegrationProvider(cat))
		if provider == "" || seen[provider] {
			continue
		}
		seen[provider] = true

		if !prompted {
			fmt.Println()
			fmt.Println("Your handle on each integration (press Enter to reuse your spec handle):")
			prompted = true
		}
		fmt.Printf("  %s handle [%s]: ", provider, cfg.User.Handle)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if cfg.User.Identities == nil {
			cfg.User.Identities = make(map[string]string)
		}
		cfg.User.Identities[provider] = line
	}
}

func runTeamConfigInit(cmd *cobra.Command) error {
	tui.PrintTitle("Welcome to spec")
	fmt.Println()
	fmt.Println("Setting up team configuration (spec.config.yaml)")
	fmt.Println()

	presetName, err := selectTeamPreset(cmd)
	if err != nil {
		return err
	}

	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	teamName := promptLine(reader, "Team name: ", "")
	cycleLabel := promptLine(reader, "Current cycle label (e.g., Cycle 7): ", "")
	repoProvider := strings.ToLower(promptLine(reader, "Specs repo provider (github | gitlab | bitbucket) [github]: ", "github"))
	repoOwner := promptLine(reader, "Specs repo owner/org: ", "")
	repoName := promptLine(reader, "Specs repo name [specs]: ", "specs")

	return writeTeamConfig(teamName, cycleLabel, repoProvider, repoOwner, repoName, presetName)
}

// selectTeamPreset picks the pipeline preset from the --preset flag, an
// interactive selector, or the "minimal" fallback in non-interactive contexts.
func selectTeamPreset(cmd *cobra.Command) (string, error) {
	presetFlag, _ := cmd.Flags().GetString("preset")
	if presetFlag != "" {
		return presetFlag, nil
	}
	if !tui.IsInteractive() {
		fmt.Printf("Using preset: %s (use --preset to specify)\n", "minimal")
		return "minimal", nil
	}

	var presetOptions []tui.PresetOption
	for _, name := range config.PresetNames() {
		desc, features, stages, _ := config.PresetInfo(name)
		presetOptions = append(presetOptions, tui.PresetOption{
			Name:        name,
			Description: desc,
			Stages:      stages,
			Features:    features,
		})
	}

	selected, err := tui.SelectPreset(presetOptions)
	if err != nil {
		return "", err
	}

	for _, p := range presetOptions {
		if p.Name == selected {
			confirmed, err := tui.ConfirmPreset(p)
			if err != nil {
				return "", err
			}
			if !confirmed {
				return "", fmt.Errorf("cancelled")
			}
			break
		}
	}
	return selected, nil
}

// promptLine prints a prompt, reads one trimmed line from the reader, and
// falls back to def when the input is empty.
func promptLine(reader *bufio.Reader, prompt, def string) string {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// writeTeamConfig renders and writes spec.config.yaml from the wizard answers.
func writeTeamConfig(teamName, cycleLabel, repoProvider, repoOwner, repoName, presetName string) error {
	content := fmt.Sprintf(`version: "1"

team:
  name: %q
  cycle_label: %q

specs_repo:
  provider: %s
  owner: %s
  repo: %s
  branch: main
  token: ${SPEC_GITHUB_TOKEN}

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
    token: ${SPEC_GITHUB_TOKEN}
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

	fmt.Println("Configuration status:")
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
