// Package dashboard aggregates signals from all configured adapters into
// a single terminal view.
package dashboard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexl/spec-cli/internal/adapter"
	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/nexl/spec-cli/internal/pipeline"
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
func Aggregate(ctx context.Context, rc *config.ResolvedConfig, reg *adapter.Registry, role string) (*DashboardData, error) {
	data := &DashboardData{}

	// Aggregate live data — no caching. The dashboard reads local files
	// (fast) and at most one API call for PR reviews. Caching added more
	// complexity (TTL, invalidation, mtime checks) than it saved.
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
