package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
)

// specBackLinkURL builds a best-effort canonical URL to the spec document so a
// PM issue can link back to it (board consumers navigate PM -> spec). Only
// GitHub specs repos are supported today; other providers return "".
func specBackLinkURL(rc *config.ResolvedConfig, specID string) string {
	sr := rc.Team.SpecsRepo
	if sr.Provider != "github" || sr.Owner == "" || sr.Repo == "" {
		return ""
	}
	branch := sr.Branch
	if branch == "" {
		branch = "main"
	}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s/%s.md",
		sr.Owner, sr.Repo, branch, gitpkg.SpecsSubDir, specID)
}

// pmSpecMeta builds the adapter SpecMeta for epic creation, attaching the
// cycle, repos, and back-link URL the richer epic payload uses.
func pmSpecMeta(rc *config.ResolvedConfig, specID, title string, meta *markdownMeta) adapter.SpecMeta {
	sm := adapter.SpecMeta{
		ID:    specID,
		Title: title,
		Cycle: rc.CycleLabel(),
		URL:   specBackLinkURL(rc, specID),
	}
	if meta != nil {
		sm.Status = meta.Status
		sm.Repos = meta.Repos
	}
	return sm
}

// markdownMeta is the minimal projection of spec frontmatter the PM helpers
// need, decoupling them from the full markdown.SpecMeta surface.
type markdownMeta struct {
	Status string
	Repos  []string
}

// ensureEpic find-or-creates a PM epic for a spec, persists its key, and is
// crash-safe: if persistence fails after a create, it queues a repair item so
// a later `spec sync --pm` reconciles rather than orphaning the epic. Returns
// the epic key (possibly "" when PM is unconfigured).
func ensureEpic(rc *config.ResolvedConfig, reg *adapter.Registry, specID string, sm adapter.SpecMeta) string {
	pm := reg.PM()
	backlink := specBackLinkURL(rc, specID)

	// Idempotency: adopt an existing epic before creating a new one.
	if existing, err := pm.FindEpic(ctx(), specID); err != nil {
		warnf("could not query PM for an existing epic: %v", err)
	} else if existing != "" {
		if perr := persistEpicKey(rc, specID, existing); perr != nil {
			warnf("could not persist PM epic key: %v", perr)
		}
		_ = pm.LinkEpic(ctx(), existing, specID, backlink)
		return existing
	}

	sm.URL = backlink
	key, err := pm.CreateEpic(ctx(), sm)
	if err != nil {
		warnf("could not create PM epic: %v", err)
		return ""
	}
	if key == "" {
		return ""
	}
	if perr := persistEpicKey(rc, specID, key); perr != nil {
		enqueuePMRepair(specID, key, store.PMOpCreate, "", perr)
		warnf("created epic %s but could not link it to the spec — queued for repair: %v", key, perr)
	}
	return key
}

// enqueuePMRepair records a failed PM operation in the retry queue (best-effort).
func enqueuePMRepair(specID, epicKey, op, payload string, cause error) {
	db, err := openDB()
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	_, _ = db.PMQueueEnqueue(store.PMQueueItem{
		SpecID: specID, EpicKey: epicKey, Op: op, Payload: payload, Detail: detail,
	})
}

// reconcilePM replays queued PM operations and pushes any drift back to the PM
// tool. It is the explicit "repair the board" lever behind `spec sync --pm`.
// Returns the number of operations resolved.
func reconcilePM(rc *config.ResolvedConfig, reg *adapter.Registry, db *store.DB, specID string) (int, error) {
	items, err := db.PMQueuePending(specID)
	if err != nil {
		return 0, err
	}
	pm := reg.PM()
	resolved := 0
	for _, item := range items {
		opErr := replayPMOp(rc, pm, item)
		if opErr == nil {
			if err := db.PMQueueResolve(item.ID); err != nil {
				return resolved, err
			}
			resolved++
			_ = db.SyncAuditLog(store.SyncAuditEntry{
				Op: "pm-" + item.Op, Actor: rc.UserName(), Surface: store.SurfaceCLI,
				Trigger: "sync-pm", SpecID: item.SpecID, Outcome: store.OutcomeOK, Detail: item.Payload,
			})
			continue
		}
		_ = db.PMQueueMark(item.ID, store.QueueStatusQueued, opErr.Error())
	}
	return resolved, nil
}

// replayPMOp re-executes a single queued PM operation.
func replayPMOp(rc *config.ResolvedConfig, pm adapter.PMAdapter, item store.PMQueueItem) error {
	switch item.Op {
	case store.PMOpStatus:
		return pm.UpdateStatus(ctx(), item.EpicKey, item.Payload)
	case store.PMOpLink:
		return pm.LinkEpic(ctx(), item.EpicKey, item.SpecID, item.Payload)
	case store.PMOpCreate:
		key := item.EpicKey
		if key == "" {
			found, err := pm.FindEpic(ctx(), item.SpecID)
			if err != nil {
				return err
			}
			key = found
		}
		if key == "" {
			return fmt.Errorf("no epic found for %s", item.SpecID)
		}
		return persistEpicKey(rc, item.SpecID, key)
	default:
		return fmt.Errorf("unknown PM op %q", item.Op)
	}
}

// pmWorkflowInspector is an optional capability: a PM adapter that can report
// its live workflow statuses, used by `spec config check` to seed status_map.
type pmWorkflowInspector interface {
	WorkflowStatuses(ctx context.Context) ([]string, error)
}
