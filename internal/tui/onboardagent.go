package tui

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/aaronl1011/spec/internal/agentcheck"
	"github.com/aaronl1011/spec/internal/config"
)

// Onboarding's agent step (§4.2.8, decision 014).
//
// A personal-config feature that no setup flow offers is invisible to exactly
// the users it is for. Onboarding used to collect only name/role/handle, so the
// flagship integration was discoverable from release notes alone — and the TUI
// is the primary surface, so `spec config init` having the step was not enough.
//
// The step's one-line blurb is where most people first learn the two planes
// exist. Detected harnesses are offered first so the common case is a single
// keystroke, and skipping leaves provider unset: no nagging, and no preference
// pointing at an agent that does not exist.

// skipAgentChoice is the sentinel for declining agent setup.
const skipAgentChoice = ""

// runAgentStep offers optional agent setup and amends the personal config.
//
// A failure to configure an agent is never fatal to onboarding: spec is fully
// usable with no agent, so anything short of the user cancelling the wizard
// leaves them with a working install.
func runAgentStep() error {
	detected := agentcheck.Detected()

	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Coding agent (optional)").
				Description("Step 2 of 3 — an agent drafts spec sections ('d' in the reader) and runs builds.\n"+
					"Skip this and everything else still works."),
			huh.NewSelect[string]().
				Title("Use a coding agent?").
				Options(agentStepOptions(detected)...).
				Value(&choice),
		),
	)
	if err := runForm(form); err != nil {
		return err
	}
	if choice == skipAgentChoice {
		return nil
	}

	agentCfg := &config.ProviderConfig{Provider: choice, Extra: map[string]string{}}

	// An endpoint provider needs a base_url; without one the adapter cannot
	// resolve, so collecting it here is the difference between a configured
	// agent and a broken one.
	if choice == providerOpenAICompatible {
		baseURL, model, err := runEndpointForm()
		if err != nil {
			return err
		}
		if strings.TrimSpace(baseURL) == "" {
			// No endpoint means nothing to write: leaving provider unset is
			// better than persisting a config that cannot work.
			return nil
		}
		agentCfg.Generate.BaseURL = strings.TrimSpace(baseURL)
		if m := strings.TrimSpace(model); m != "" {
			agentCfg.Generate.Model = m
		}
	}

	return writeUserAgent(agentCfg)
}

// providerOpenAICompatible is the escape hatch for any conforming completions
// server, local or hosted. It is the general-purpose option rather than a vendor.
const providerOpenAICompatible = "openai-compatible"

// agentStepOptions builds the choice list: detected harnesses first, then the
// endpoint option, then skip.
//
// Ordering is the whole point — a user with a harness installed should be one
// keystroke from done, and a user with none should still see that an endpoint
// works rather than concluding spec needs a vendor they do not have.
func agentStepOptions(detected []string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(detected)+2)
	for _, provider := range detected {
		options = append(options, huh.NewOption(provider+"  (found on PATH)", provider))
	}
	options = append(options, huh.NewOption(
		providerOpenAICompatible+"  (Ollama, llama.cpp, LM Studio, a gateway…)", providerOpenAICompatible))
	options = append(options, huh.NewOption("Skip — set one up later", skipAgentChoice))
	return options
}

// runEndpointForm collects the completions endpoint and optional model.
func runEndpointForm() (baseURL, model string, err error) {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Endpoint base URL").
				Description("the OpenAI-compatible base, e.g. http://localhost:11434/v1").
				Placeholder("http://localhost:11434/v1").
				Value(&baseURL),
			huh.NewInput().
				Title("Model").
				Description("blank uses the server default (optional)").
				Value(&model),
		),
	)
	if err := runForm(form); err != nil {
		return "", "", err
	}
	return baseURL, model, nil
}

// writeUserAgent merges an agent into the personal config on disk.
//
// It re-reads rather than reusing the identity step's in-memory struct so the
// step is independently testable and cannot clobber a field written between the
// two steps.
func writeUserAgent(agentCfg *config.ProviderConfig) error {
	path := config.UserConfigPath()
	cfg, err := config.LoadUserConfig(path)
	if err != nil {
		return fmt.Errorf("reading personal config to add the agent: %w", err)
	}
	cfg.Agent = agentCfg
	if err := config.WriteUserConfig(path, cfg); err != nil {
		return fmt.Errorf("writing agent config: %w", err)
	}
	return nil
}
