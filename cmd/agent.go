package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/llm/tasks"
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

// `spec agent check` exists because misconfiguration should surface in a
// diagnostic with a named failing step, not as a confusing failure partway
// through a draft. It mirrors `spec config check` for PM/Jira.
//
// The latency it reports is also the evidence decision 003 defers to: shipping
// headless-only was justified on the assumption that subprocess spawn cost is
// negligible next to model latency, and this is where that assumption becomes
// measurable rather than asserted.

// knownHarnesses are the coding agents spec can detect on PATH, with the binary
// each provider expects.
var knownHarnesses = []struct {
	Provider string
	Command  string
}{
	{Provider: "claude-code", Command: "claude"},
	{Provider: "pi", Command: "pi"},
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
			detectedHarnessHint())
	}

	report := agentReport{Provider: provider}

	// Step 1: the binary or endpoint must be reachable. A missing binary is the
	// most common failure and the cheapest to diagnose.
	if err := checkAgentReachable(agentCfg, &report); err != nil {
		report.FailedStep = "reachability"
		report.Err = err.Error()
		renderAgentReport(p, report)
		return fmt.Errorf("agent check failed at %s: %w", report.FailedStep, err)
	}

	// Step 2: capability detection, so the report says what will actually work.
	agent := buildRegistry(rc).Agent()
	caps := agent.Capabilities()
	report.Capabilities = caps

	// Step 3: a real contained round-trip. Capability flags are claims; this is
	// the part that proves them.
	if caps.Generate {
		if err := checkAgentGenerate(agent, &report); err != nil {
			report.FailedStep = "completion"
			report.Err = err.Error()
			renderAgentReport(p, report)
			return fmt.Errorf("agent check failed at %s: %w", report.FailedStep, err)
		}
	}

	report.InertSettings = inertGenerateSettings(provider, agentCfg)

	renderAgentReport(p, report)
	if !caps.Generate && !caps.MCP {
		return fmt.Errorf("agent %q supports neither drafting nor sessions — check the provider name", provider)
	}
	return nil
}

// agentReport is the diagnostic result.
type agentReport struct {
	Provider      string               `json:"provider"`
	Binary        string               `json:"binary,omitempty"`
	Endpoint      string               `json:"endpoint,omitempty"`
	Capabilities  adapter.Capabilities `json:"capabilities"`
	Model         string               `json:"model,omitempty"`
	LatencyMS     int64                `json:"latency_ms,omitempty"`
	Tokens        int                  `json:"tokens,omitempty"`
	InertSettings []string             `json:"inert_settings,omitempty"`
	FailedStep    string               `json:"failed_step,omitempty"`
	Err           string               `json:"error,omitempty"`
}

// checkAgentReachable verifies the harness binary is on PATH, or that a
// completion endpoint is configured.
func checkAgentReachable(agentCfg config.ProviderConfig, report *agentReport) error {
	switch agentCfg.Provider {
	case "claude-code", "pi":
		command := agentCfg.Get("command")
		if command == "" {
			for _, h := range knownHarnesses {
				if h.Provider == agentCfg.Provider {
					command = h.Command
				}
			}
		}
		path, err := exec.LookPath(command)
		if err != nil {
			return fmt.Errorf("%q not found in PATH — install it, or set agent.command in ~/.spec/config.yaml", command)
		}
		report.Binary = path
		return nil

	case "anthropic":
		if agentCfg.Generate.Token == "" && agentCfg.Get("token") == "" {
			return errors.New("no token configured — set agent.generate.token to an env reference such as ${SPEC_LLM_TOKEN}")
		}
		report.Endpoint = "https://api.anthropic.com"
		return nil

	default:
		// Completion endpoints: the adapter already refuses an empty base_url
		// with an actionable message, so resolution is the check.
		if url := agentCfg.Generate.BaseURL; url != "" {
			report.Endpoint = url
		} else if url := agentCfg.Get("base_url"); url != "" {
			report.Endpoint = url
		}
		return nil
	}
}

// checkAgentGenerate runs a minimal contained completion and records what it cost.
//
// The prompt is deliberately trivial: this measures the path, not the model, and
// a cheap probe keeps the diagnostic usable on a metered provider.
func checkAgentGenerate(agent adapter.AgentAdapter, report *agentReport) error {
	svc := llm.NewService(agent, true).WithMaxTokens(32)

	task, err := tasks.Get(tasks.DraftSection)
	if err != nil {
		return err
	}
	// Override the task's system prompt so the probe cannot be mistaken for real
	// drafting work by a model that likes to elaborate.
	task.System = "Reply with exactly the word: ok"
	task.TokenBudget = 0
	task.Build = func(llm.Input) (string, []llm.ContextPart) {
		return "Reply with exactly the word: ok", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
	defer cancel()

	start := time.Now()
	res, err := svc.Run(ctx, task, llm.Input{})
	elapsed := time.Since(start)
	if err != nil {
		return err
	}
	if res == nil || strings.TrimSpace(res.Text) == "" {
		detail := ""
		if res != nil && res.Raw != "" {
			detail = fmt.Sprintf(" (provider output: %s)", res.Raw)
		}
		return fmt.Errorf("the provider returned no text%s", detail)
	}

	report.LatencyMS = elapsed.Milliseconds()
	report.Model = res.Model
	report.Tokens = res.Tokens.Total
	return nil
}

// inertGenerateSettings names configured values that cannot take effect for the
// resolved provider. A setting that looks effective but is not is worse than one
// that is absent.
func inertGenerateSettings(provider string, agentCfg config.ProviderConfig) []string {
	var out []string
	switch provider {
	case "claude-code", "pi":
		if agentCfg.Generate.MaxTokens != 0 {
			out = append(out, "generate.max_tokens (the harness CLI exposes no token cap)")
		}
		if agentCfg.Generate.BaseURL != "" {
			out = append(out, "generate.base_url (a harness uses its own auth and endpoint)")
		}
		if agentCfg.Generate.Token != "" {
			out = append(out, "generate.token (a harness uses its own auth)")
		}
	}
	return out
}

// renderAgentReport prints the diagnostic, or emits it as JSON.
func renderAgentReport(p *printer, report agentReport) {
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

// detectedHarnesses returns the providers whose binary is on PATH, so setup can
// offer what is installed first and make the common case a single keystroke.
func detectedHarnesses() []string {
	var found []string
	for _, h := range knownHarnesses {
		if _, err := exec.LookPath(h.Command); err == nil {
			found = append(found, h.Provider)
		}
	}
	return found
}

// detectedHarnessHint suggests installed harnesses in an error message.
func detectedHarnessHint() string {
	found := detectedHarnesses()
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf(" (detected on PATH: %s)", strings.Join(found, ", "))
}
