package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
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

// pmSpecMeta builds the adapter SpecMeta for PM object creation, attaching the
// cycle, repos, and back-link URL the richer payload uses.
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

// ensurePMObject find-or-creates a spec's PM object, persists its key, and is
// crash-safe: if persistence fails after a create, it queues a repair item so
// a later `spec sync --pm` reconciles rather than orphaning the issue. Returns
// the PM key (possibly "" when PM is unconfigured).
//
// The object's type is decided once, at first sync, and never converted. A
// spec with a parent becomes a task under the initiative's epic; a spec
// without one becomes an epic. A spec that already has a PM object keeps it
// and is merely linked — a Jira issue-type "Move" is lossy and
// provider-specific, and pm_queue replays operations on retry, so a replayed
// lossy Move is exactly how issue history gets destroyed.
func ensurePMObject(rc *config.ResolvedConfig, reg *adapter.Registry, specID string, sm adapter.SpecMeta, parentID string) string {
	pm := reg.PM()
	backlink := specBackLinkURL(rc, specID)

	// Idempotency: adopt an existing PM object before creating a new one.
	if existing, err := pm.FindEpic(ctx(), specID); err != nil {
		warnf("could not query PM for an existing issue: %v", err)
	} else if existing != "" {
		if perr := persistPMKey(rc, specID, existing); perr != nil {
			warnf("could not persist PM key: %v", perr)
		}
		_ = pm.LinkEpic(ctx(), existing, specID, backlink)
		warnAlreadySynced(specID, existing, parentID, rc)
		return existing
	}

	sm.URL = backlink
	key, err := createPMObject(rc, pm, specID, sm, parentID)
	if err != nil {
		warnf("could not create PM issue: %v", err)
		return ""
	}
	if key == "" {
		return ""
	}
	if perr := persistPMKey(rc, specID, key); perr != nil {
		enqueuePMRepair(specID, key, store.PMOpCreate, "", perr)
		warnf("created %s but could not link it to the spec — queued for repair: %v", key, perr)
	}
	return key
}

// createPMObject creates the right kind of PM object for a spec: a task under
// the initiative's epic for a deliverable slice, an epic otherwise.
//
// An initiative that has not synced yet is an ordinary queued retry, not an
// error: the initiative is created before its slices in the normal flow, and
// pm_queue handles the out-of-order case.
func createPMObject(rc *config.ResolvedConfig, pm adapter.PMAdapter, specID string, sm adapter.SpecMeta, parentID string) (string, error) {
	if parentID == "" {
		return pm.CreateEpic(ctx(), sm)
	}
	parentKey := parentPMKey(rc, parentID)
	if parentKey == "" {
		enqueuePMRepair(specID, "", store.PMOpCreate, "", nil)
		warnf("%s is a slice of %s, which has no PM object yet — queued; run 'spec sync --pm' once %s has synced",
			specID, parentID, parentID)
		return "", nil
	}
	return pm.CreateTask(ctx(), sm, parentKey)
}

// parentPMKey reads an initiative's PM key from its frontmatter, or "" when it
// has not been synced (or cannot be read).
func parentPMKey(rc *config.ResolvedConfig, parentID string) string {
	path, err := resolveSpecPath(rc, parentID)
	if err != nil {
		return ""
	}
	meta, err := readSpecMeta(path)
	if err != nil {
		return ""
	}
	return meta.PMKey
}

// warnAlreadySynced reports the degradation from Decision 8: a spec that was
// already an epic and has since gained a parent keeps its epic and is linked,
// rather than being converted. Converting is lossy and unreplayable; a visible
// link is strictly safer.
func warnAlreadySynced(specID, existingKey, parentID string, rc *config.ResolvedConfig) {
	if parentID == "" {
		return
	}
	parentKey := parentPMKey(rc, parentID)
	if parentKey == "" {
		return
	}
	warnf("%s already exists as %s — linked to %s rather than converted", specID, existingKey, parentKey)
}

// enqueuePMRepair records a failed PM operation in the retry queue (best-effort).
func enqueuePMRepair(specID, pmKey, op, payload string, cause error) {
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
		SpecID: specID, PMKey: pmKey, Op: op, Payload: payload, Detail: detail,
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
	resolved := 0
	for _, item := range items {
		opErr := replayPMOp(rc, reg, item)
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
func replayPMOp(rc *config.ResolvedConfig, reg *adapter.Registry, item store.PMQueueItem) error {
	pm := reg.PM()
	switch item.Op {
	case store.PMOpStatus:
		return pm.UpdateStatus(ctx(), item.PMKey, item.Payload)
	case store.PMOpLink:
		return pm.LinkEpic(ctx(), item.PMKey, item.SpecID, item.Payload)
	case store.PMOpCreate:
		return replayPMCreate(rc, reg, item)
	default:
		return fmt.Errorf("unknown PM op %q", item.Op)
	}
}

// replayPMCreate reconciles a queued create. Three cases reach it: a create
// that succeeded remotely but failed to persist its key (repair by writing the
// key), a spec whose PM object exists but was never recorded (adopt it), and a
// deliverable slice queued because its initiative had not synced yet (create
// the task now that the parent's key exists).
func replayPMCreate(rc *config.ResolvedConfig, reg *adapter.Registry, item store.PMQueueItem) error {
	if item.PMKey != "" {
		return persistPMKey(rc, item.SpecID, item.PMKey)
	}
	found, err := reg.PM().FindEpic(ctx(), item.SpecID)
	if err != nil {
		return err
	}
	if found != "" {
		return persistPMKey(rc, item.SpecID, found)
	}

	meta, err := queuedSpecMeta(rc, item.SpecID)
	if err != nil {
		return err
	}
	sm := pmSpecMeta(rc, item.SpecID, meta.Title, &markdownMeta{Status: meta.Status, Repos: meta.Repos})
	if key := ensurePMObject(rc, reg, item.SpecID, sm, meta.Parent); key != "" {
		return nil
	}
	return fmt.Errorf("no PM object for %s yet — its initiative may still be unsynced", item.SpecID)
}

// queuedSpecMeta reads the frontmatter of a spec named by a queued operation.
func queuedSpecMeta(rc *config.ResolvedConfig, specID string) (*markdown.SpecMeta, error) {
	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return nil, err
	}
	return readSpecMeta(path)
}

// pmWorkflowInspector is an optional capability: a PM adapter that can report
// its live workflow statuses, used by `spec config check` to seed status_map.
type pmWorkflowInspector interface {
	WorkflowStatuses(ctx context.Context) ([]string, error)
}
