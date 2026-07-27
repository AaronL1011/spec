// Package agentcheck runs the `spec agent check` preflight: it resolves the
// agent from personal config, reports its capability set, and runs a tiny
// contained completion to prove the whole path works before the user relies on
// it mid-draft. The package is named after the command (spec agent check) and
// the preflight capability, not after "the agent adapter" — that lives in
// internal/adapter — so the two never read as the same thing on grep.
//
// `spec agent check` exists because misconfiguration should surface in a
// diagnostic with a named failing step, not as a confusing failure partway
// through a draft. It mirrors `spec config check` for PM/Jira.
//
// The latency it reports is also the evidence decision 003 defers to: shipping
// headless-only was justified on the assumption that subprocess spawn cost is
// negligible next to model latency, and this is where that assumption becomes
// measurable rather than asserted.
package agentcheck

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
)

// Check runs the agent preflight: reachability, capability detection, and (when
// advertised) a contained Generate round-trip.
//
// Returns a Report for the caller to render and an error only when a step
// failed. The error wraps the underlying failure and names the failed step; a
// caller that wants to render the partial result reads Report, which carries
// FailedStep and Err, so the render is the same on the success and failure
// paths.
func Check(agentCfg config.ProviderConfig, agent adapter.AgentAdapter) (Report, error) {
	report := Report{Provider: agentCfg.Provider}

	// Step 1: the binary or endpoint must be reachable. A missing binary is the
	// most common failure and the cheapest to diagnose.
	if err := checkReachable(agentCfg, &report); err != nil {
		report.FailedStep = "reachability"
		report.Err = err.Error()
		return report, fmt.Errorf("agent check failed at %s: %w", report.FailedStep, err)
	}

	// Step 2: capability detection, so the report says what will actually work.
	caps := agent.Capabilities()
	report.Capabilities = caps

	// Step 3: a real contained round-trip. Capability flags are claims; this is
	// the part that proves them.
	if caps.Generate {
		if err := checkGenerate(agent, &report); err != nil {
			report.FailedStep = "completion"
			report.Err = err.Error()
			return report, fmt.Errorf("agent check failed at %s: %w", report.FailedStep, err)
		}
	}

	report.InertSettings = InertSettings(agentCfg)
	return report, nil
}

// checkReachable verifies the harness binary is on PATH, or that a completion
// endpoint is configured.
func checkReachable(agentCfg config.ProviderConfig, report *Report) error {
	switch agentCfg.Provider {
	case "claude-code", "pi":
		command := agentCfg.Get("command")
		if command == "" {
			for _, h := range KnownHarnesses {
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

// checkGenerate runs a minimal contained completion and records what it cost.
//
// The prompt is deliberately trivial: this measures the path, not the model, and
// a cheap probe keeps the diagnostic usable on a metered provider.
func checkGenerate(agent adapter.AgentAdapter, report *Report) error {
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

// InertSettings names configured values that cannot take effect for the
// resolved provider. A setting that looks effective but is not is worse than one
// that is absent.
func InertSettings(agentCfg config.ProviderConfig) []string {
	var out []string
	switch agentCfg.Provider {
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
