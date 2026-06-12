package cmd

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/spf13/cobra"
)

var configLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate team config against the schema with line-precise diagnostics",
	Long: `Validate spec.config.yaml for structural and semantic errors — unknown gate
types, invalid enum values, missing required fields — and report each with its
file, line number, and a suggested fix. Exits non-zero when any error is found.

Use --json for a machine-readable diagnostics array.`,
	RunE: runConfigLint,
}

func init() {
	configCmd.AddCommand(configLintCmd)
}

func runConfigLint(cmd *cobra.Command, _ []string) error {
	p := newPrinter(cmd)

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if rc.TeamConfigPath == "" {
		return fmt.Errorf("team config not found — run 'spec config init' to set up, or ensure spec.config.yaml exists")
	}

	result, err := config.LintTeamConfigFile(rc.TeamConfigPath)
	if err != nil {
		return err
	}

	// Cross-reference the user's per-provider identities against the team's
	// configured integrations. Advisory only (warnings) and best-effort — a
	// missing or unreadable user config is not a lint failure.
	if rc.UserConfigPath != "" {
		if userRes, uerr := config.LintUserIdentitiesFile(rc.UserConfigPath, rc.Team); uerr == nil {
			result.Diagnostics = append(result.Diagnostics, userRes.Diagnostics...)
		}
	}

	if p.JSONEnabled() {
		if err := p.JSON(result); err != nil {
			return err
		}
		if result.HasErrors() {
			return errConfigInvalid
		}
		return nil
	}

	renderLintResult(p, result)
	if result.HasErrors() {
		return errConfigInvalid
	}
	return nil
}

// errConfigInvalid signals a non-zero exit after diagnostics have already been
// rendered, so the top-level handler does not print a redundant second message.
var errConfigInvalid = fmt.Errorf("config invalid")

// lint diagnostic colours, consistent with the TUI theme palette. lipgloss
// auto-degrades to no-op styling on non-TTY output and honours NO_COLOR.
var (
	lintErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true)
	lintWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Bold(true)
	lintOKStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a")).Bold(true)
	lintSuggestStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#787c99"))
)

// renderLintResult prints human-readable, line-precise diagnostics.
func renderLintResult(p *printer, result config.LintResult) {
	if len(result.Diagnostics) == 0 {
		p.Line("%s", lintOKStyle.Render("✓ config valid"))
		return
	}

	errors, warnings := 0, 0
	for _, d := range result.Diagnostics {
		loc := fmt.Sprintf("%s:%d", d.File, d.Line)
		if d.Column > 0 {
			loc = fmt.Sprintf("%s:%d", loc, d.Column)
		}

		var label string
		switch d.Severity {
		case config.SeverityError:
			errors++
			label = lintErrorStyle.Render("error")
		case config.SeverityWarning:
			warnings++
			label = lintWarnStyle.Render("warning")
		}

		field := ""
		if d.Field != "" {
			field = d.Field + ": "
		}
		line := fmt.Sprintf("%s: %s: %s%s", loc, label, field, d.Message)
		if d.Suggestion != "" {
			line += " — " + lintSuggestStyle.Render(d.Suggestion)
		}
		p.Line("%s", line)
	}

	p.Line("")
	p.Line("%d error(s), %d warning(s)", errors, warnings)
}
