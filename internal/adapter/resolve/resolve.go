// Package resolve creates concrete adapter implementations from team config.
// It lives outside internal/adapter to avoid import cycles — adapter defines
// interfaces, resolve imports both adapter and the concrete implementations.
package resolve

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/anthropic"
	"github.com/aaronl1011/spec/internal/adapter/claude"
	"github.com/aaronl1011/spec/internal/adapter/confluence"
	gh "github.com/aaronl1011/spec/internal/adapter/github"
	"github.com/aaronl1011/spec/internal/adapter/jira"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/ollama"
	"github.com/aaronl1011/spec/internal/adapter/slack"
	"github.com/aaronl1011/spec/internal/adapter/teams"
	"github.com/aaronl1011/spec/internal/config"
)

// All builds a fully-populated Registry from team config.
// Unconfigured or unrecognised providers get noop adapters.
// Returns warnings for providers that are configured but can't be initialised.
func All(cfg *config.TeamConfig) (*adapter.Registry, []string) {
	reg := adapter.NewRegistry(cfg)
	var warnings []string

	// Comms
	comms, warn := resolveComms(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithComms(comms)

	// PM
	pm, warn := resolvePM(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithPM(pm)

	// Docs
	docs, warn := resolveDocs(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithDocs(docs)

	// Repo
	repo, warn := resolveRepo(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithRepo(repo)

	// Agent
	agent, warn := resolveAgent(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithAgent(agent)

	// Deploy
	deploy, warn := resolveDeploy(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithDeploy(deploy)

	// AI
	ai, warn := resolveAI(cfg)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	reg.WithAI(ai)

	return reg, warnings
}

func resolveComms(cfg *config.TeamConfig) (adapter.CommsAdapter, string) {
	provider := cfg.Integrations.Comms.Provider
	switch provider {
	case "", "none":
		return noop.Comms{}, ""
	case "slack":
		token := cfg.Integrations.Comms.Get("token")
		if token == "" {
			return noop.Comms{}, "slack: token not configured — comms disabled"
		}
		defaultCh := cfg.Integrations.Comms.Get("default_channel")
		standupCh := cfg.Integrations.Comms.Get("standup_channel")
		return slack.NewClient(token, defaultCh, standupCh), ""
	case "teams":
		webhookURL := cfg.Integrations.Comms.Get("webhook_url")
		if webhookURL == "" {
			return noop.Comms{}, "teams: webhook_url not configured — comms disabled"
		}
		standupWebhook := cfg.Integrations.Comms.Get("standup_webhook_url")
		graphToken := cfg.Integrations.Comms.Get("graph_token")
		teamID := cfg.Integrations.Comms.Get("team_id")
		channelID := cfg.Integrations.Comms.Get("channel_id")
		if graphToken != "" || teamID != "" || channelID != "" {
			if graphToken == "" || teamID == "" || channelID == "" {
				return teams.NewClient(webhookURL, standupWebhook, graphToken, teamID, channelID),
					"teams: graph_token, team_id, and channel_id are all required for mention sync — outbound comms enabled"
			}
		}
		return teams.NewClient(webhookURL, standupWebhook, graphToken, teamID, channelID), ""
	case "discord":
		return noop.Comms{}, "discord adapter not yet implemented — comms disabled"
	default:
		return noop.Comms{}, fmt.Sprintf("unknown comms provider %q — comms disabled", provider)
	}
}

func resolvePM(cfg *config.TeamConfig) (adapter.PMAdapter, string) {
	provider := cfg.Integrations.PM.Provider
	switch provider {
	case "", "none":
		return noop.PM{}, ""
	case "jira":
		baseURL := cfg.Integrations.PM.Get("base_url")
		projectKey := cfg.Integrations.PM.Get("project_key")
		email := cfg.Integrations.PM.Get("email")
		token := cfg.Integrations.PM.Get("token")
		if baseURL == "" || projectKey == "" || email == "" || token == "" {
			return noop.PM{}, "jira: base_url, project_key, email, and token required — PM disabled"
		}
		return jira.NewClient(baseURL, projectKey, email, token), ""
	case "linear", "github-issues":
		return noop.PM{}, fmt.Sprintf("%s PM adapter not yet implemented — PM disabled", provider)
	default:
		return noop.PM{}, fmt.Sprintf("unknown PM provider %q — PM disabled", provider)
	}
}

func resolveDocs(cfg *config.TeamConfig) (adapter.DocsAdapter, string) {
	provider := cfg.Integrations.Docs.Provider
	switch provider {
	case "", "none":
		return noop.Docs{}, ""
	case "confluence":
		baseURL := cfg.Integrations.Docs.Get("base_url")
		spaceKey := cfg.Integrations.Docs.Get("space_key")
		email := cfg.Integrations.Docs.Get("email")
		token := cfg.Integrations.Docs.Get("token")
		if baseURL == "" || spaceKey == "" || email == "" || token == "" {
			return noop.Docs{}, "confluence: base_url, space_key, email, and token required — docs disabled"
		}
		return confluence.NewClient(baseURL, spaceKey, email, token), ""
	case "notion":
		return noop.Docs{}, "notion adapter not yet implemented — docs disabled"
	default:
		return noop.Docs{}, fmt.Sprintf("unknown docs provider %q — docs disabled", provider)
	}
}

func resolveRepo(cfg *config.TeamConfig) (adapter.RepoAdapter, string) {
	provider := cfg.Integrations.Repo.Provider
	switch provider {
	case "", "none":
		return noop.Repo{}, ""
	case "github":
		token := cfg.Integrations.Repo.Get("token")
		owner := cfg.Integrations.Repo.Get("owner")
		if owner == "" {
			owner = cfg.SpecsRepo.Owner
		}
		if token == "" {
			token = cfg.SpecsRepo.Token
		}
		if token == "" {
			return noop.Repo{}, "github repo: token not configured — repo adapter disabled"
		}
		if owner == "" {
			return noop.Repo{}, "github repo: owner not configured — repo adapter disabled"
		}
		return gh.NewRepoClient(token, owner), ""
	case "gitlab", "bitbucket":
		return noop.Repo{}, fmt.Sprintf("%s repo adapter not yet implemented — repo disabled", provider)
	default:
		return noop.Repo{}, fmt.Sprintf("unknown repo provider %q — repo disabled", provider)
	}
}

func resolveAgent(cfg *config.TeamConfig) (adapter.AgentAdapter, string) {
	provider := cfg.Integrations.Agent.Provider
	switch provider {
	case "", "none":
		return noop.Agent{}, ""
	case "claude-code":
		command := cfg.Integrations.Agent.Get("command")
		return claude.NewAgent(command), ""
	case "cursor", "copilot", "pi":
		return noop.Agent{}, fmt.Sprintf("%s agent adapter not yet implemented — agent disabled", provider)
	default:
		return noop.Agent{}, fmt.Sprintf("unknown agent provider %q — agent disabled", provider)
	}
}

func resolveDeploy(cfg *config.TeamConfig) (adapter.DeployAdapter, string) {
	provider := cfg.Integrations.Deploy.Provider
	switch provider {
	case "", "none":
		return noop.Deploy{}, ""
	case "github-actions":
		token := cfg.Integrations.Repo.Get("token")
		owner := cfg.Integrations.Repo.Get("owner")
		if owner == "" {
			owner = cfg.SpecsRepo.Owner
		}
		if token == "" {
			token = cfg.SpecsRepo.Token
		}
		if token == "" {
			return noop.Deploy{}, "github-actions deploy: token not configured — deploy disabled"
		}
		workflow := "deploy.yml" // default, could be configurable via extra config
		return gh.NewDeployClient(token, owner, workflow), ""
	case "gitlab-ci", "argocd":
		return noop.Deploy{}, fmt.Sprintf("%s deploy adapter not yet implemented — deploy disabled", provider)
	default:
		return noop.Deploy{}, fmt.Sprintf("unknown deploy provider %q — deploy disabled", provider)
	}
}

func resolveAI(cfg *config.TeamConfig) (adapter.AIAdapter, string) {
	provider := cfg.Integrations.AI.Provider
	switch provider {
	case "", "none":
		return noop.AI{}, ""
	case "anthropic":
		token := cfg.Integrations.AI.Get("token")
		if token == "" {
			return noop.AI{}, "anthropic: token not configured — AI disabled"
		}
		model := cfg.Integrations.AI.Get("model")
		return anthropic.NewClient(token, model), ""
	case "ollama":
		model := cfg.Integrations.AI.Get("model")
		embedModel := cfg.Integrations.AI.Get("embed_model")
		baseURL := cfg.Integrations.AI.Get("base_url")
		return ollama.NewClient(model, embedModel, baseURL), ""
	case "openai":
		return noop.AI{}, "openai adapter not yet implemented — AI disabled"
	default:
		return noop.AI{}, fmt.Sprintf("unknown AI provider %q — AI disabled", provider)
	}
}
