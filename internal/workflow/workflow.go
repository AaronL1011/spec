// Package workflow holds the orchestration logic for spec pipeline
// transitions — advance, revert, eject, resume. It mutates the spec file on
// disk, runs transition effects, and records activity, returning structured
// results. It performs no terminal I/O: callers (the CLI and the TUI) decide
// how to render outcomes. This keeps cmd/ thin and makes the state machine
// unit-testable against adapter fakes.
package workflow

import (
	"context"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/pipeline/effects"
	"github.com/aaronl1011/spec/internal/store"
)

// Deps carries the collaborators a workflow operation needs. DB may be nil;
// activity logging and DB-backed effects degrade to no-ops in that case.
type Deps struct {
	Config   *config.ResolvedConfig
	Registry *adapter.Registry
	DB       *store.DB
	Role     string
}

// EffectOutcome is a render-ready summary of one executed (or previewed)
// transition effect. It mirrors effects.Result without exposing the executor.
type EffectOutcome struct {
	Message string `json:"message,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Err     string `json:"error,omitempty"`
}

// outcomes converts executor results into render-ready outcomes, dropping
// silent successes (no message, no error) so callers only see signal.
func outcomes(results []effects.Result) []EffectOutcome {
	var out []EffectOutcome
	for _, r := range results {
		switch {
		case r.Error != nil:
			out = append(out, EffectOutcome{Message: r.Message, Err: r.Error.Error()})
		case r.Skipped:
			// Skipped effects are not surfaced; they are expected and noisy.
		case r.Message != "":
			out = append(out, EffectOutcome{Message: r.Message})
		}
	}
	return out
}

// user returns the configured user name.
func (d Deps) user() string { return d.Config.UserName() }

// execContext builds the effect-execution context shared by transitions.
// specDir is the specs/ directory inside the repo clone; epicKey comes from
// the spec frontmatter.
func (d Deps) execContext(specID, title, from, to, specDir, epicKey string, tt effects.TransitionType, dryRun bool) effects.ExecutionContext {
	return effects.ExecutionContext{
		SpecID:         specID,
		SpecTitle:      title,
		FromStage:      from,
		ToStage:        to,
		TransitionType: tt,
		User:           d.user(),
		UserRole:       d.Role,
		DryRun:         dryRun,
		Notifier:       &effects.NotifierAdapter{Comms: d.Registry.Comms(), SpecID: specID, Title: title},
		Syncer: &effects.SyncerAdapter{
			Docs:             d.Registry.Docs(),
			DB:               d.DB,
			SpecDir:          specDir,
			ConflictStrategy: d.Config.Team.Sync.ConflictStrategy,
			OwnerRole:        d.Role,
			UserName:         d.user(),
			DryRun:           dryRun,
		},
		PMUpdater: &effects.PMUpdaterAdapter{PM: d.Registry.PM(), EpicKey: epicKey},
		Webhooker: &effects.WebhookerAdapter{},
		Logger:    &effects.LoggerAdapter{DB: d.DB, SpecDir: specDir, SpecID: specID},
	}
}

// logActivity records an activity-log entry, ignoring failures (best-effort).
func (d Deps) logActivity(specID, eventType, summary, metaJSON string) {
	if d.DB == nil {
		return
	}
	_ = d.DB.ActivityLog(specID, eventType, summary, metaJSON, d.user())
}

// resolvedPipeline returns the resolved pipeline for stage/effect lookup.
func (d Deps) resolvedPipeline() (*pipeline.ResolvedPipeline, error) {
	return pipeline.Resolve(d.Config.Team.Pipeline)
}

// readMeta is a thin wrapper to keep call sites terse.
func readMeta(path string) (*markdown.SpecMeta, error) {
	return markdown.ReadMeta(path)
}

// noopDeadline guards against a nil context at engine entry points.
func ensureCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
