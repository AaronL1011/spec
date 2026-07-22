// Package dashboard aggregates signals from all configured adapters into
// a single terminal view.
package dashboard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/identity"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/thread"
	"github.com/aaronl1011/spec/internal/urgency"
)

// DashboardItem represents a single item in a dashboard section.
type DashboardItem struct {
	SpecID   string `json:"spec_id"`
	Title    string `json:"title"`
	Stage    string `json:"stage"`
	Detail   string `json:"detail"`
	Urgency  string `json:"urgency"` // "normal", "stale", "critical"
	URL      string `json:"url,omitempty"`
	Assignee string `json:"assignee,omitempty"` // assignee label or "unclaimed" for DO rows

	// StaleFraction is the eased time-urgency intensity (0..1) for this row,
	// driving the gradient colour. 0 means fresh or the stage has no stale
	// window (never stale).
	StaleFraction float64 `json:"stale_fraction,omitempty"`

	// SortTime is the moment this item entered its current state — stage entry
	// for specs, PR-open for reviews, intake date for triage. It drives the
	// oldest-first ordering within each dashboard section. Zero when unknown.
	SortTime time.Time `json:"-"`
}

// DashboardData holds all dashboard sections.
type DashboardData struct {
	Do         []DashboardItem `json:"do"`
	Review     []DashboardItem `json:"review"`
	Discussion []DashboardItem `json:"discussion"`
	Incoming   []DashboardItem `json:"incoming"`
	Blocked    []DashboardItem `json:"blocked"`
	FYI        []DashboardItem `json:"fyi"`
}

// Render outputs the dashboard to the terminal.
func Render(data *DashboardData, userName, role, cycle string) {
	fmt.Print(greeting(time.Now(), userName))
	parts := []string{}
	if role != "" {
		parts = append(parts, role)
	}
	if cycle != "" {
		parts = append(parts, cycle)
	}
	if len(parts) > 0 {
		fmt.Printf("                           %s", strings.Join(parts, " · "))
	}
	fmt.Println()

	anyOutput := false

	if len(data.Do) > 0 {
		fmt.Println()
		fmt.Println("─── DO ──────────────────────────────────────────────────────────")
		for _, item := range data.Do {
			icon := "⚡"
			if item.Urgency == "stale" {
				icon = "⏰"
			}
			stage := item.Stage
			if item.Assignee != "" {
				stage += "  ·  " + item.Assignee
			}
			fmt.Printf("%s %-10s  %-30s  %s\n", icon, item.SpecID, truncStr(item.Title, 30), stage)
			if item.Detail != "" {
				fmt.Printf("   %s\n", item.Detail)
			}
		}
		anyOutput = true
	}

	if len(data.Review) > 0 {
		fmt.Println()
		fmt.Println("─── REVIEW ──────────────────────────────────────────────────────")
		for _, item := range data.Review {
			fmt.Printf("📋 %-10s  %-30s  %s\n", item.SpecID, truncStr(item.Title, 30), item.Detail)
		}
		anyOutput = true
	}

	if len(data.Discussion) > 0 {
		fmt.Println()
		fmt.Println("─── DISCUSSION ──────────────────────────────────────────────────")
		for _, item := range data.Discussion {
			fmt.Printf("💬 %-10s  %-30s  %s\n", item.SpecID, truncStr(item.Title, 30), item.Stage)
			if item.Detail != "" {
				fmt.Printf("   %s\n", item.Detail)
			}
		}
		anyOutput = true
	}

	if len(data.Incoming) > 0 {
		fmt.Println()
		fmt.Println("─── INCOMING ────────────────────────────────────────────────────")
		for _, item := range data.Incoming {
			icon := "📨"
			if item.Urgency == "critical" {
				icon = "🔴"
			}
			fmt.Printf("%s %-10s  %-30s  %s\n", icon, item.SpecID, truncStr(item.Title, 30), item.Stage)
		}
		anyOutput = true
	}

	if len(data.Blocked) > 0 {
		fmt.Println()
		fmt.Println("─── BLOCKED ─────────────────────────────────────────────────────")
		for _, item := range data.Blocked {
			fmt.Printf("🚫 %-10s  %-30s  %s\n", item.SpecID, truncStr(item.Title, 30), item.Detail)
		}
		anyOutput = true
	}

	if !anyOutput {
		completedCount := countCompletedSpecs(data)
		fmt.Println()
		fmt.Printf("✓ All clear. %d specs completed this cycle.\n", completedCount)
	}

	fmt.Println()
}

// Aggregate collects data for the dashboard from all sources.
func Aggregate(ctx context.Context, rc *config.ResolvedConfig, reg *adapter.Registry, role string) (*DashboardData, error) {
	data := &DashboardData{}

	// Aggregate live data — no caching. The dashboard reads local files
	// (fast) and at most one API call for PR reviews. Caching added more
	// complexity (TTL, invalidation, mtime checks) than it saved.
	pl := rc.Pipeline()
	viewer := viewerFor(rc, role)
	blockedCfg := blockedConfig(rc)
	curve := dashboardCurve(rc)
	// Shared by REVIEW (PR age) and DISCUSSION (thread age): both are "someone
	// is waiting on you" items, so they share one opt-in staleness window
	// rather than each needing its own config knob.
	reviewWindow, _ := dashboardConfig(rc).ReviewWindow()
	now := time.Now()

	// DO section: specs scoped to the viewer by stage dashboard scope.
	// BLOCKED section: blocked specs scoped by the team blocked config.
	// DISCUSSION section: open threads on any spec (not just ones the viewer
	// owns) where it is the viewer's turn. Claiming a spec counts as
	// involvement in all its threads, so claimants see new comments without
	// an explicit @-mention.
	if rc.SpecsRepoDir != "" {
		specs, err := loadSpecs(rc)
		if err == nil {
			threadStore := thread.NewSidecarStore(rc.SpecsRepoDir)
			for _, s := range specs {
				view := s.view()
				if s.Status == pipeline.StatusBlocked {
					if VisibleInBlocked(pl, blockedCfg, view, viewer) {
						data.Blocked = append(data.Blocked, DashboardItem{
							SpecID:   s.ID,
							Title:    s.Title,
							SortTime: stageEntryTime(s.StageEnteredAt, s.Updated),
						})
					}
				} else if VisibleInDo(pl, view, viewer) {
					frac := StageUrgency(pl, curve, s.Status, s.StageEnteredAt, s.Updated, now)
					data.Do = append(data.Do, DashboardItem{
						SpecID:        s.ID,
						Title:         s.Title,
						Stage:         s.Status,
						Assignee:      doAssigneeLabel(pl, s),
						StaleFraction: frac,
						Urgency:       urgencyLabel(frac),
						SortTime:      stageEntryTime(s.StageEnteredAt, s.Updated),
					})
				}
				claimed := identity.AnyIdentity(s.Assignees, viewer)
				data.Discussion = append(data.Discussion,
					discussionItems(threadStore, s.ID, s.Title, viewer, claimed, reviewWindow, curve, now)...)
			}
		}

		// INCOMING: triage items
		triageItems, _ := loadTriageItems(rc)
		for _, t := range triageItems {
			data.Incoming = append(data.Incoming, DashboardItem{
				SpecID:   t.ID,
				Title:    t.Title,
				Stage:    "triage",
				Urgency:  t.Priority,
				SortTime: parseDay(t.Created),
			})
		}
	}

	// REVIEW section: from repo adapter. Rows warm toward red as a PR's age
	// (now - opened) approaches the opt-in review staleness window.
	if reg != nil {
		prs, err := reg.Repo().RequestedReviews(ctx, rc.IdentityForCategory("repo"))
		if err == nil {
			for _, pr := range prs {
				frac := ReviewUrgency(reviewWindow, curve, pr.CreatedAt, now)
				data.Review = append(data.Review, DashboardItem{
					SpecID:        fmt.Sprintf("PR #%d", pr.Number),
					Title:         pr.Title,
					Detail:        fmt.Sprintf("%s  %s", pr.Repo, timeAgo(pr.CreatedAt)),
					URL:           pr.URL,
					StaleFraction: frac,
					Urgency:       urgencyLabel(frac),
					SortTime:      pr.CreatedAt,
				})
			}
		}
	}

	// Order every section oldest-first so the item that has waited longest leads.
	sortItemsByOldest(data.Blocked)
	sortItemsByOldest(data.Do)
	sortItemsByOldest(data.Review)
	sortItemsByOldest(data.Discussion)
	sortItemsByOldest(data.Incoming)

	return data, nil
}

// sortItemsByOldest orders items oldest-first by SortTime. Items with a known
// time lead (earliest first); undated items sort last in stable order so they
// never displace genuinely-aged work.
func sortItemsByOldest(items []DashboardItem) {
	slices.SortStableFunc(items, func(a, b DashboardItem) int {
		az, bz := a.SortTime.IsZero(), b.SortTime.IsZero()
		switch {
		case az && bz:
			return 0
		case az:
			return 1
		case bz:
			return -1
		default:
			return a.SortTime.Compare(b.SortTime)
		}
	})
}

type specInfo struct {
	ID             string
	Title          string
	Status         string
	Author         string
	Assignees      []string
	BlockedFrom    string
	StageEnteredAt string
	Updated        string
}

// view projects a specInfo into the resolver's SpecView.
func (s specInfo) view() SpecView {
	return SpecView{
		Author:      s.Author,
		Assignees:   s.Assignees,
		Status:      s.Status,
		BlockedFrom: s.BlockedFrom,
	}
}

// viewerFor builds a Viewer from resolved config and the active role.
func viewerFor(rc *config.ResolvedConfig, role string) Viewer {
	return Viewer{
		Role:       role,
		Name:       rc.UserName(),
		Handle:     rc.CanonicalHandle(),
		Identities: rc.UserIdentities(),
	}
}

// dashboardConfig returns the team dashboard config, or the zero value when
// team config is absent.
func dashboardConfig(rc *config.ResolvedConfig) config.DashboardConfig {
	if rc.Team == nil {
		return config.DashboardConfig{}
	}
	return rc.Team.Dashboard
}

// dashboardCurve resolves the team's configured easing curve for the urgency
// gradient, defaulting to ease-in when team config is absent.
func dashboardCurve(rc *config.ResolvedConfig) urgency.Curve {
	return dashboardConfig(rc).EasingCurve()
}

// ReviewUrgency computes the eased time-urgency intensity (0..1) for a REVIEW
// row from the PR's age (now - createdAt) against the configured review window.
// Returns 0 when no window is configured (window <= 0) or the PR has no opened
// timestamp, so REVIEW colouring is strictly opt-in.
func ReviewUrgency(window time.Duration, curve urgency.Curve, createdAt, now time.Time) float64 {
	if window <= 0 || createdAt.IsZero() {
		return 0
	}
	return urgency.Value(now.Sub(createdAt), window, curve)
}

// StageUrgency computes the eased time-urgency intensity (0..1) for a spec at
// stageName, from its stage-entry time against the stage's configured stale
// window. Returns 0 when the stage has no window (never stale) or the entry
// time cannot be resolved. Shared by the dashboard and the pipeline screen so
// a task reads at the same intensity on both.
func StageUrgency(pl config.PipelineConfig, curve urgency.Curve, stageName, stageEnteredAt, updated string, now time.Time) float64 {
	stage := pl.StageByName(stageName)
	if stage == nil {
		return 0
	}
	window, ok := stage.StaleWindow()
	if !ok {
		return 0
	}
	entered, ok := parseStageEntry(stageEnteredAt, updated)
	if !ok {
		return 0
	}
	return urgency.Value(now.Sub(entered), window, curve)
}

// stageEntryTime resolves when a spec entered its current stage, returning the
// zero time when neither stage_entered_at nor updated can be parsed (such rows
// sort last under oldest-first ordering).
func stageEntryTime(stageEnteredAt, updated string) time.Time {
	t, _ := parseStageEntry(stageEnteredAt, updated)
	return t
}

// parseDay parses a YYYY-MM-DD date, returning the zero time on empty or
// malformed input.
func parseDay(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse("2006-01-02", s)
	return t
}

// parseStageEntry resolves when a spec entered its current stage. It prefers the
// RFC3339 stage_entered_at stamp and falls back to the day-granularity updated
// date for legacy specs written before the field existed.
func parseStageEntry(stageEnteredAt, updated string) (time.Time, bool) {
	if stageEnteredAt != "" {
		if t, err := time.Parse(time.RFC3339, stageEnteredAt); err == nil {
			return t, true
		}
	}
	if updated != "" {
		if t, err := time.Parse("2006-01-02", updated); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// urgencyLabel derives the legacy discrete urgency string from the continuous
// fraction so existing icon/sort logic keeps working: a fully-elapsed window
// (f>=1) reads as "stale".
func urgencyLabel(f float64) string {
	if f >= 1 {
		return "stale"
	}
	return ""
}

// blockedConfig returns the team BLOCKED-section config, or the zero value
// (which means "all roles, all blocked specs") when team config is absent.
func blockedConfig(rc *config.ResolvedConfig) config.BlockedConfig {
	if rc.Team == nil {
		return config.BlockedConfig{}
	}
	return rc.Team.Dashboard.Blocked
}

func loadSpecs(rc *config.ResolvedConfig) ([]specInfo, error) {
	entries, err := os.ReadDir(rc.SpecsRepoDir)
	if err != nil {
		return nil, err
	}

	var specs []specInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(rc.SpecsRepoDir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		specs = append(specs, specInfo{
			ID:             meta.ID,
			Title:          meta.Title,
			Status:         meta.Status,
			Author:         meta.Author,
			Assignees:      meta.Assignees,
			BlockedFrom:    meta.BlockedFrom,
			StageEnteredAt: meta.StageEnteredAt,
			Updated:        meta.Updated,
		})
	}
	return specs, nil
}

type triageInfo struct {
	ID       string
	Title    string
	Priority string
	Created  string
}

func loadTriageItems(rc *config.ResolvedConfig) ([]triageInfo, error) {
	triageDir := filepath.Join(rc.SpecsRepoDir, "triage")
	entries, err := os.ReadDir(triageDir)
	if err != nil {
		return nil, err
	}

	var items []triageInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(triageDir, e.Name())
		meta, err := markdown.ReadTriageMeta(path)
		if err != nil {
			continue
		}
		items = append(items, triageInfo{
			ID:       meta.ID,
			Title:    meta.Title,
			Priority: meta.Priority,
			Created:  meta.Created,
		})
	}
	return items, nil
}

// doAssigneeLabel returns the DO-row assignee indicator: a compact assignee
// label when the spec is claimed, or "unclaimed" when an assignee-scoped stage
// is still waiting for someone to pick it up. Role-scoped, unassigned specs get
// no label.
func doAssigneeLabel(pl config.PipelineConfig, s specInfo) string {
	if len(s.Assignees) > 0 {
		return assigneeLabel(s.Assignees)
	}
	if stage := pl.StageByName(s.Status); stage != nil && stage.Dashboard.Scope() == config.DoScopeAssignee {
		return "unclaimed"
	}
	return ""
}

// assigneeLabel renders assignees compactly: the first name, plus "+N" when
// there are more.
func assigneeLabel(assignees []string) string {
	if len(assignees) == 0 {
		return ""
	}
	if len(assignees) == 1 {
		return assignees[0]
	}
	return fmt.Sprintf("%s +%d", assignees[0], len(assignees)-1)
}

func countCompletedSpecs(data *DashboardData) int {
	return len(data.FYI)
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func greeting(t time.Time, name string) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return fmt.Sprintf("Good morning, %s.", name)
	case hour >= 12 && hour < 17:
		return fmt.Sprintf("Afternoon, %s.", name)
	case hour >= 17 && hour < 21:
		return fmt.Sprintf("Good evening, %s.", name)
	default:
		return fmt.Sprintf("Burning the midnight oil are we, %s?", name)
	}
}
