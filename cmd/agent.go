package cmd

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/agentcheck"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Inspect and verify the configured coding agent",
}

var agentCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify the configured agent end to end",
	Long: "Resolves the agent from personal config, reports its capability set, and runs a tiny " +
		"contained completion to prove the whole path works before you rely on it mid-draft.",
	RunE: runAgentCheck,
}

func init() {
	agentCmd.AddCommand(agentCheckCmd)
	rootCmd.AddCommand(agentCmd)
}

func runAgentCheck(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	agentCfg := rc.EffectiveAgentConfig()
	provider := agentCfg.Provider
	if provider == "" || provider == "none" {
		return fmt.Errorf("no agent configured — add 'agent:' to ~/.spec/config.yaml, or run 'spec config init'%s",
			agentcheck.DetectedHint())
	}

	report, err := agentcheck.Check(agentCfg, buildRegistry(rc).Agent())
	renderAgentReport(p, report)
	if err != nil {
		return err
	}
	if !report.Capabilities.Generate && !report.Capabilities.MCP {
		return fmt.Errorf("agent %q supports neither drafting nor sessions — check the provider name", provider)
	}
	return nil
}

// renderAgentReport prints the diagnostic, or emits it as JSON.
func renderAgentReport(p *printer, report agentcheck.Report) {
	if p.JSONEnabled() {
		_ = p.JSON(report)
		return
	}

	p.Line("Agent: %s", report.Provider)
	if report.Binary != "" {
		p.Line("  binary:       %s", report.Binary)
	}
	if report.Endpoint != "" {
		p.Line("  endpoint:     %s", report.Endpoint)
	}

	// Name the planes rather than dumping flags: "drafting" and "sessions" are
	// what the user experiences.
	var planes []string
	if report.Capabilities.Generate {
		planes = append(planes, "drafting")
	}
	if report.Capabilities.MCP {
		planes = append(planes, "sessions")
	}
	if len(planes) == 0 {
		planes = append(planes, "none")
	}
	p.Line("  capabilities: %s", strings.Join(planes, " + "))
	if report.Capabilities.StructuredOutput {
		p.Line("                (native JSON schema support)")
	}

	if report.Err != "" {
		p.Line("")
		p.Line("  ✗ %s: %s", report.FailedStep, report.Err)
		return
	}

	if report.Capabilities.Generate {
		p.Line("  round-trip:   %s", formatLatency(report.LatencyMS))
		if report.Model != "" {
			p.Line("  responded as: %s", report.Model)
		}
		if report.Tokens > 0 {
			p.Line("  tokens:       %d", report.Tokens)
		}
	}

	for _, inert := range report.InertSettings {
		p.Warn("ignored setting: %s", inert)
	}

	p.Line("")
	switch {
	case report.Capabilities.Generate && report.Capabilities.MCP:
		p.Line("✓ Drafting and interactive sessions are both available.")
	case report.Capabilities.Generate:
		p.Line("✓ Drafting is available. This provider does not support sessions, so 'spec build' will degrade.")
	case report.Capabilities.MCP:
		p.Line("✓ Sessions are available. This provider does not serve one-shot completions, so 'spec draft' will degrade.")
	}
}

// formatLatency renders a duration in the unit that reads best at its magnitude.
func formatLatency(ms int64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dms", ms)
}

// detectedHarnesses is a thin shim so cmd/config.go's setup prompt can offer
// installed agents first, without importing the preflight package's test
// surface. The detection table lives in internal/agentcheck so the preflight and
// the setup flow cannot disagree about what spec recognises.
func detectedHarnesses() []string {
	return agentcheck.Detected()
}
