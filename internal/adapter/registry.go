package adapter

import (
	"github.com/aaronl1011/spec/internal/config"
)

// Registry resolves configuration to concrete adapter implementations.
type Registry struct {
	cfg *config.TeamConfig

	comms  CommsAdapter
	pm     PMAdapter
	docs   DocsAdapter
	repo   RepoAdapter
	agent  AgentAdapter
	deploy DeployAdapter
	ai     AIAdapter
}

// NewRegistry creates a new adapter registry from team configuration.
// Concrete adapters are injected via With* methods. Unconfigured adapters
// are set to their noop implementations by the caller.
func NewRegistry(cfg *config.TeamConfig) *Registry {
	return &Registry{cfg: cfg}
}

// WithComms sets the comms adapter.
func (r *Registry) WithComms(a CommsAdapter) *Registry { r.comms = a; return r }

// WithPM sets the PM adapter.
func (r *Registry) WithPM(a PMAdapter) *Registry { r.pm = a; return r }

// WithDocs sets the docs adapter.
func (r *Registry) WithDocs(a DocsAdapter) *Registry { r.docs = a; return r }

// WithRepo sets the repo adapter.
func (r *Registry) WithRepo(a RepoAdapter) *Registry { r.repo = a; return r }

// WithAgent sets the agent adapter.
func (r *Registry) WithAgent(a AgentAdapter) *Registry { r.agent = a; return r }

// WithDeploy sets the deploy adapter.
func (r *Registry) WithDeploy(a DeployAdapter) *Registry { r.deploy = a; return r }

// WithAI sets the AI adapter.
func (r *Registry) WithAI(a AIAdapter) *Registry { r.ai = a; return r }

// Comms returns the comms adapter.
func (r *Registry) Comms() CommsAdapter { return r.comms }

// PM returns the PM adapter.
func (r *Registry) PM() PMAdapter { return r.pm }

// Docs returns the docs adapter.
func (r *Registry) Docs() DocsAdapter { return r.docs }

// Repo returns the repo adapter.
func (r *Registry) Repo() RepoAdapter { return r.repo }

// Agent returns the agent adapter.
func (r *Registry) Agent() AgentAdapter { return r.agent }

// Deploy returns the deploy adapter.
func (r *Registry) Deploy() DeployAdapter { return r.deploy }

// AI returns the AI adapter.
func (r *Registry) AI() AIAdapter { return r.ai }

// Config returns the team configuration.
func (r *Registry) Config() *config.TeamConfig { return r.cfg }
