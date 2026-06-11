package workflow

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// syncPMStatus reflects the spec's new stage onto the linked PM issue's board
// status. It is on by default (no pipeline effect required), best-effort, and
// idempotent: an unconfigured PM, an empty epic key, or a stage with no
// configured status mapping is a clean no-op. On failure it enqueues a retry
// and records an audit row so the board never silently drifts from spec state
// (docs/JIRA_HARDENING_PLAN.md §P3, §P5).
func (d Deps) syncPMStatus(ctx context.Context, specID, epicKey, stage string) *EffectOutcome {
	if epicKey == "" || d.Registry == nil {
		return nil
	}
	pm := d.Registry.PM()
	if pm == nil {
		return nil
	}
	if err := pm.UpdateStatus(ctx, epicKey, stage); err != nil {
		d.enqueuePM(specID, epicKey, store.PMOpStatus, stage, err)
		return &EffectOutcome{Message: "Jira status sync deferred (queued for retry)", Err: err.Error()}
	}
	return nil
}

// syncPMStories reconciles per-step PM stories under the spec's epic when story
// sync is enabled. It is opt-in (pm.sync_stories), idempotent (find-or-create
// by marker label), and best-effort: returned story keys are written back to
// the spec frontmatter; failures enqueue a retry. Returns an outcome to surface
// when work was deferred (docs/JIRA_HARDENING_PLAN.md §P4).
func (d Deps) syncPMStories(ctx context.Context, specID, specPath, epicKey string, steps []markdown.BuildStep) *EffectOutcome {
	if epicKey == "" || len(steps) == 0 || d.Registry == nil || d.Config == nil || d.Config.Team == nil {
		return nil
	}
	if !d.Config.Team.Integrations.PM.Jira().SyncStories {
		return nil
	}

	specs := make([]adapter.StorySpec, len(steps))
	for i, step := range steps {
		specs[i] = adapter.StorySpec{
			StepID:      fmt.Sprintf("%s/%d", specID, i),
			Repo:        step.Repo,
			Description: step.Description,
			Status:      step.Status,
		}
	}

	links, err := d.Registry.PM().SyncStories(ctx, epicKey, specs)
	if err != nil {
		d.enqueuePM(specID, epicKey, store.PMOpStory, "", err)
		return &EffectOutcome{Message: "Jira story sync deferred (queued for retry)", Err: err.Error()}
	}
	d.writeStoryKeys(specPath, links)
	return nil
}

// writeStoryKeys records returned story keys back onto the matching build steps
// in the spec frontmatter (best-effort).
func (d Deps) writeStoryKeys(specPath string, links []adapter.StoryLink) {
	if len(links) == 0 {
		return
	}
	meta, err := markdown.ReadMeta(specPath)
	if err != nil {
		return
	}
	byStep := make(map[string]string, len(links))
	for _, l := range links {
		byStep[l.StepID] = l.StoryKey
	}
	changed := false
	for i := range meta.Steps {
		key := byStep[fmt.Sprintf("%s/%d", meta.ID, i)]
		if key != "" && meta.Steps[i].StoryKey != key {
			meta.Steps[i].StoryKey = key
			changed = true
		}
	}
	if changed {
		_ = markdown.WriteMeta(specPath, meta)
	}
}

// enqueuePM records a failed PM operation for later retry and audits it. Both
// writes are best-effort: a nil DB (e.g. in unit tests) degrades to a no-op.
func (d Deps) enqueuePM(specID, epicKey, op, payload string, cause error) {
	if d.DB == nil {
		return
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	_, _ = d.DB.PMQueueEnqueue(store.PMQueueItem{
		SpecID:  specID,
		EpicKey: epicKey,
		Op:      op,
		Payload: payload,
		Detail:  detail,
	})
	_ = d.DB.SyncAuditLog(store.SyncAuditEntry{
		Op:      "pm-" + op,
		Actor:   d.user(),
		Surface: store.SurfaceCLI,
		Trigger: "advance",
		SpecID:  specID,
		Outcome: store.OutcomeQueued,
		Detail:  fmt.Sprintf("%s: %s", payload, detail),
	})
}
