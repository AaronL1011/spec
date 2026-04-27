// Package sync coordinates section-scoped synchronization between local specs and docs providers.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/adapter"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/store"
)

// Sync directions.
const (
	DirectionIn   = "in"
	DirectionOut  = "out"
	DirectionBoth = "both"
)

// Conflict strategies.
const (
	ConflictWarn  = "warn"
	ConflictAbort = "abort"
	ConflictForce = "force"
	ConflictSkip  = "skip"
)

const (
	stateDirectionIn  = "in"
	stateDirectionOut = "out"
)

// ErrSyncConflict is returned when conflicts are found and the strategy is abort.
var ErrSyncConflict = errors.New("sync conflicts detected")

// Options configures one sync run.
type Options struct {
	SpecID           string
	SpecPath         string
	Direction        string
	ConflictStrategy string
	OwnerRole        string
	UserName         string
	DryRun           bool
}

// Engine coordinates sync through adapter interfaces and store APIs.
type Engine struct {
	docs      adapter.DocsAdapter
	db        *store.DB
	readFile  func(string) ([]byte, error)
	writeFile func(string, []byte, os.FileMode) error
}

// NewEngine creates a sync engine.
func NewEngine(docs adapter.DocsAdapter, db *store.DB) *Engine {
	return &Engine{
		docs:      docs,
		db:        db,
		readFile:  os.ReadFile,
		writeFile: os.WriteFile,
	}
}

// Report summarizes the outcome of a sync run.
type Report struct {
	SpecID           string
	Direction        string
	DryRun           bool
	OutboundPushed   bool
	InboundApplied   []string
	OutboundSections []string
	Conflicts        []SectionConflict
	Skipped          []SectionSkip
	Unchanged        []string
	PageMissing      bool
}

// HasBlockingConflicts returns whether conflicts should block the command.
func (r Report) HasBlockingConflicts() bool {
	return len(r.Conflicts) > 0
}

// SectionConflict describes a local/remote section conflict.
type SectionConflict struct {
	Section    string
	Owner      string
	LocalHash  string
	RemoteHash string
	Reason     string
}

// SectionSkip describes a section that was intentionally not updated.
type SectionSkip struct {
	Section string
	Reason  string
}

// PreparedRun contains local changes and deferred external/state side effects.
type PreparedRun struct {
	opts            Options
	Report          *Report
	outboundContent string
	pendingState    []pendingStateUpdate
}

type pendingStateUpdate struct {
	section    string
	localHash  string
	remoteHash string
}

// Run executes a sync run.
func (e *Engine) Run(ctx context.Context, opts Options) (*Report, error) {
	prepared, err := e.Prepare(ctx, opts)
	if err != nil {
		if prepared != nil {
			return prepared.Report, err
		}
		return nil, err
	}
	if err := e.Finalize(ctx, prepared); err != nil {
		return prepared.Report, err
	}
	return prepared.Report, nil
}

// Prepare computes the sync plan and applies only local file changes.
func (e *Engine) Prepare(ctx context.Context, opts Options) (*PreparedRun, error) {
	if e.docs == nil {
		return nil, fmt.Errorf("docs adapter is not configured")
	}
	if opts.SpecID == "" {
		return nil, fmt.Errorf("spec id is required")
	}
	if opts.SpecPath == "" {
		return nil, fmt.Errorf("spec path is required")
	}

	direction, err := normalizeDirection(opts.Direction)
	if err != nil {
		return nil, err
	}
	strategy, err := normalizeConflictStrategy(opts.ConflictStrategy)
	if err != nil {
		return nil, err
	}
	opts.Direction = direction
	opts.ConflictStrategy = strategy

	report := &Report{SpecID: opts.SpecID, Direction: direction, DryRun: opts.DryRun}
	prepared := &PreparedRun{opts: opts, Report: report}

	if direction == DirectionIn || direction == DirectionBoth {
		if err := e.prepareInbound(ctx, prepared); err != nil {
			return prepared, err
		}
	}
	if direction == DirectionOut || direction == DirectionBoth {
		if err := e.prepareOutbound(opts, prepared); err != nil {
			return prepared, err
		}
	}

	return prepared, nil
}

// Finalize applies external docs writes, sync-state persistence, and activity logging.
func (e *Engine) Finalize(ctx context.Context, prepared *PreparedRun) error {
	if prepared == nil || prepared.Report == nil {
		return fmt.Errorf("prepared sync run is required")
	}
	if prepared.opts.DryRun {
		return nil
	}
	if prepared.outboundContent != "" {
		if err := e.docs.PushFull(ctx, prepared.opts.SpecID, prepared.outboundContent); err != nil {
			return fmt.Errorf("pushing %s to docs provider: %w", prepared.opts.SpecID, err)
		}
		prepared.Report.OutboundPushed = true
	}
	if err := e.persistState(prepared.opts.SpecID, prepared.pendingState); err != nil {
		return err
	}
	e.logActivity(prepared.opts, prepared.Report)
	return nil
}

func (e *Engine) prepareInbound(ctx context.Context, prepared *PreparedRun) error {
	opts := prepared.opts
	report := prepared.Report
	data, err := e.readFile(opts.SpecPath)
	if err != nil {
		return fmt.Errorf("reading local spec %s: %w", opts.SpecPath, err)
	}
	content := string(data)
	sections := markdown.ExtractSections(content)
	localBySlug := sectionsBySlug(sections)

	remoteSections, err := e.docs.FetchSections(ctx, opts.SpecID)
	if err != nil {
		return fmt.Errorf("fetching remote sections for %s: %w", opts.SpecID, err)
	}
	if remoteSections == nil {
		report.PageMissing = true
		return nil
	}

	nextContent := content
	for _, section := range sections {
		remoteContent, ok := remoteSections[section.Slug]
		if !ok {
			report.Skipped = append(report.Skipped, SectionSkip{Section: section.Slug, Reason: "remote section missing"})
			continue
		}

		localHash := Hash(section.Content)
		remoteHash := Hash(remoteContent)
		if localHash == remoteHash {
			report.Unchanged = append(report.Unchanged, section.Slug)
			prepared.pendingState = append(prepared.pendingState, pendingStateUpdate{
				section:    section.Slug,
				localHash:  localHash,
				remoteHash: remoteHash,
			})
			continue
		}

		lastIn := e.stateHash(opts.SpecID, section.Slug, stateDirectionIn)
		lastOut := e.stateHash(opts.SpecID, section.Slug, stateDirectionOut)
		remoteChanged := lastIn == "" || remoteHash != lastIn
		localChanged := localChangedSinceSync(section.Content, localHash, lastOut, lastIn)

		if !remoteChanged {
			report.Unchanged = append(report.Unchanged, section.Slug)
			continue
		}
		if ownerMismatch(section, opts.OwnerRole) && opts.ConflictStrategy != ConflictForce {
			report.Skipped = append(report.Skipped, SectionSkip{
				Section: section.Slug,
				Reason:  fmt.Sprintf("owned by %s", section.Owner),
			})
			continue
		}
		if localChanged && opts.ConflictStrategy != ConflictForce {
			conflict := SectionConflict{
				Section:    section.Slug,
				Owner:      section.Owner,
				LocalHash:  localHash,
				RemoteHash: remoteHash,
				Reason:     "local and remote changed since last sync",
			}
			report.Conflicts = append(report.Conflicts, conflict)
			if opts.ConflictStrategy == ConflictAbort {
				continue
			}
			report.Skipped = append(report.Skipped, SectionSkip{Section: section.Slug, Reason: "conflict"})
			continue
		}

		if _, ok := localBySlug[section.Slug]; ok {
			nextContent, err = markdown.ReplaceSectionContent(nextContent, section.Slug, remoteContent)
			if err != nil {
				return fmt.Errorf("applying inbound section %s: %w", section.Slug, err)
			}
			report.InboundApplied = append(report.InboundApplied, section.Slug)
			prepared.pendingState = append(prepared.pendingState, pendingStateUpdate{
				section:    section.Slug,
				localHash:  remoteHash,
				remoteHash: remoteHash,
			})
		}
	}

	if opts.ConflictStrategy == ConflictAbort && report.HasBlockingConflicts() {
		return ErrSyncConflict
	}
	if !opts.DryRun && nextContent != content {
		if err := e.writeFile(opts.SpecPath, []byte(nextContent), 0o644); err != nil {
			return fmt.Errorf("writing local spec %s: %w", opts.SpecPath, err)
		}
	}
	return nil
}

func (e *Engine) prepareOutbound(opts Options, prepared *PreparedRun) error {
	report := prepared.Report
	data, err := e.readFile(opts.SpecPath)
	if err != nil {
		return fmt.Errorf("reading local spec %s: %w", opts.SpecPath, err)
	}
	content := string(data)
	sections := markdown.ExtractSections(content)
	for _, section := range sections {
		report.OutboundSections = append(report.OutboundSections, section.Slug)
	}

	for _, section := range sections {
		hash := Hash(section.Content)
		prepared.pendingState = append(prepared.pendingState, pendingStateUpdate{
			section:    section.Slug,
			localHash:  hash,
			remoteHash: hash,
		})
	}
	if opts.DryRun {
		report.OutboundPushed = true
		return nil
	}
	prepared.outboundContent = content
	return nil
}

// Hash returns the deterministic sync hash for section content.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}

func (e *Engine) stateHash(specID, section, direction string) string {
	if e.db == nil {
		return ""
	}
	entry, err := e.db.SyncStateGet(specID, section, direction)
	if err != nil || entry == nil {
		return ""
	}
	return entry.Hash
}

func (e *Engine) persistState(specID string, updates []pendingStateUpdate) error {
	if e.db == nil {
		return nil
	}
	for _, update := range updates {
		if err := e.db.SyncStateSet(specID, update.section, stateDirectionOut, update.localHash); err != nil {
			return err
		}
		if err := e.db.SyncStateSet(specID, update.section, stateDirectionIn, update.remoteHash); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) logActivity(opts Options, report *Report) {
	if e.db == nil || report == nil || opts.DryRun {
		return
	}
	metadata, err := json.Marshal(map[string]interface{}{
		"direction":         report.Direction,
		"inbound_applied":   report.InboundApplied,
		"outbound_pushed":   report.OutboundPushed,
		"outbound_sections": len(report.OutboundSections),
		"conflicts":         len(report.Conflicts),
		"skipped":           len(report.Skipped),
		"page_missing":      report.PageMissing,
	})
	if err != nil {
		return
	}
	user := opts.UserName
	if user == "" {
		user = "system"
	}
	_ = e.db.ActivityLog(opts.SpecID, "sync", fmt.Sprintf("synced %s", report.Direction), string(metadata), user)
}

func sectionsBySlug(sections []markdown.Section) map[string]markdown.Section {
	bySlug := make(map[string]markdown.Section, len(sections))
	for _, section := range sections {
		bySlug[section.Slug] = section
	}
	return bySlug
}

func localChangedSinceSync(content, localHash, lastOut, lastIn string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	switch {
	case lastOut != "":
		return localHash != lastOut
	case lastIn != "":
		return localHash != lastIn
	default:
		return true
	}
}

func ownerMismatch(section markdown.Section, ownerRole string) bool {
	if section.Owner == "" {
		return false
	}
	if ownerRole == "" {
		return true
	}
	return !strings.EqualFold(section.Owner, ownerRole)
}

func normalizeDirection(direction string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", DirectionBoth:
		return DirectionBoth, nil
	case DirectionIn, "inbound":
		return DirectionIn, nil
	case DirectionOut, "outbound":
		return DirectionOut, nil
	default:
		return "", fmt.Errorf("invalid sync direction %q — use in, out, or both", direction)
	}
}

func normalizeConflictStrategy(strategy string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", ConflictWarn:
		return ConflictWarn, nil
	case ConflictAbort:
		return ConflictAbort, nil
	case ConflictForce:
		return ConflictForce, nil
	case ConflictSkip:
		return ConflictSkip, nil
	default:
		return "", fmt.Errorf("invalid sync conflict strategy %q — use warn, abort, force, or skip", strategy)
	}
}
