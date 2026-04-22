// Package dashboard aggregates signals from all configured adapters into
// a single terminal view.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexl/spec-cli/internal/adapter"
	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/nexl/spec-cli/internal/pipeline"
	"github.com/nexl/spec-cli/internal/store"
)

// DashboardItem represents a single item in a dashboard section.
type DashboardItem struct {
	SpecID  string `json:"spec_id"`
	Title   string `json:"title"`
	Stage   string `json:"stage"`
	Detail  string `json:"detail"`
	Urgency string `json:"urgency"` // "normal", "stale", "critical"
}

// DashboardData holds all dashboard sections.
type DashboardData struct {
	Do       []DashboardItem `json:"do"`
	Review   []DashboardItem `json:"review"`
	Incoming []DashboardItem `json:"incoming"`
	Blocked  []DashboardItem `json:"blocked"`
	FYI      []DashboardItem `json:"fyi"`
	Cached   bool            `json:"-"`
}

// Render outputs the dashboard to the terminal.
func Render(data *DashboardData, userName, role, cycle string) {
	fmt.Printf("Good morning, %s.", userName)
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

	if data.Cached {
		fmt.Println("                                                     (cached)")
	}

	anyOutput := false

	if len(data.Do) > 0 {
		fmt.Println()
		fmt.Println("─── DO ──────────────────────────────────────────────────────────")
		for _, item := range data.Do {
			icon := "⚡"
			if item.Urgency == "stale" {
				icon = "⏰"
			}
			fmt.Printf("%s %-10s  %-30s  %s\n", icon, item.SpecID, truncStr(item.Title, 30), item.Stage)
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
func Aggregate(ctx context.Context, rc *config.ResolvedConfig, db *store.DB, reg *adapter.Registry, role string) (*DashboardData, error) {
	data := &DashboardData{}

	ttl := 300
	if rc.Team != nil {
		ttl = rc.Team.Dashboard.RefreshTTL
	}

	// Try cache first — but only if spec files haven't been modified since
	// the cache was written. This catches manual edits, MCP tool writes,
	// and any mutation path that doesn't explicitly invalidate the cache.
	if db != nil {
		entry, err := db.CacheGetEntry("dashboard:data")
		if err == nil && entry.Value != "" && entry.Fresh {
			if !specsModifiedSince(rc.SpecsRepoDir, entry.FetchedAt) {
				if err := json.Unmarshal([]byte(entry.Value), data); err == nil {
					data.Cached = true
					return data, nil
				}
			}
		}
	}

	// Aggregate live data
	pl := rc.Pipeline()

	// DO section: specs where stage owner_role matches user
	if rc.SpecsRepoDir != "" {
		specs, err := loadSpecs(rc)
		if err == nil {
			for _, s := range specs {
				stage := pl.StageByName(s.Status)
				if stage != nil && stage.OwnerRole == role {
					data.Do = append(data.Do, DashboardItem{
						SpecID: s.ID,
						Title:  s.Title,
						Stage:  s.Status,
					})
				}
				if s.Status == pipeline.StatusBlocked {
					data.Blocked = append(data.Blocked, DashboardItem{
						SpecID: s.ID,
						Title:  s.Title,
					})
				}
			}
		}

		// INCOMING: triage items
		triageItems, _ := loadTriageItems(rc)
		for _, t := range triageItems {
			data.Incoming = append(data.Incoming, DashboardItem{
				SpecID:  t.ID,
				Title:   t.Title,
				Stage:   "triage",
				Urgency: t.Priority,
			})
		}
	}

	// REVIEW section: from repo adapter
	if reg != nil {
		prs, err := reg.Repo().RequestedReviews(ctx, rc.UserHandle())
		if err == nil {
			for _, pr := range prs {
				data.Review = append(data.Review, DashboardItem{
					SpecID: fmt.Sprintf("PR #%d", pr.Number),
					Title:  pr.Title,
					Detail: fmt.Sprintf("%s  %s", pr.Repo, timeAgo(pr.CreatedAt)),
				})
			}
		}
	}

	// Cache the results
	if db != nil {
		jsonData, _ := json.Marshal(data)
		_ = db.CacheSet("dashboard:data", string(jsonData), ttl)
	}

	return data, nil
}

type specInfo struct {
	ID     string
	Title  string
	Status string
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
			ID:     meta.ID,
			Title:  meta.Title,
			Status: meta.Status,
		})
	}
	return specs, nil
}

type triageInfo struct {
	ID       string
	Title    string
	Priority string
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
		})
	}
	return items, nil
}

func countCompletedSpecs(data *DashboardData) int {
	return len(data.FYI)
}

// specsModifiedSince returns true if any spec or triage file in the specs
// repo directory has been modified after the given time.
func specsModifiedSince(specsDir string, since time.Time) bool {
	if specsDir == "" {
		return false
	}

	dirs := []string{specsDir, filepath.Join(specsDir, "triage")}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(since) {
				return true
			}
		}
	}
	return false
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
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
