package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/spf13/cobra"
)

// blockerLookbackDays is how far back to search for eject events when building the blockers list.
const blockerLookbackDays = 7

var standupCmd = &cobra.Command{
	Use:   "standup",
	Short: "Auto-generate standup from actual activity",
	RunE:  runStandup,
}

func init() {
	rootCmd.AddCommand(standupCmd)
}

func runStandup(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Build registry once — shared between enrichment sources and posting
	reg := buildRegistry(rc)

	// Get activity from last 24h
	since := time.Now().Add(-24 * time.Hour)
	entries, err := db.ActivitySince(since)
	if err != nil {
		return err
	}

	userName := rc.UserName()
	date := time.Now().Format("2006-01-02")

	fmt.Printf("Your standup — %s — %s\n", userName, date)
	fmt.Println("────────────────────────────────────────────────")

	yesterday := printYesterday(entries)
	today := printToday(rc, db, reg)

	fmt.Println("\nBlockers:")
	blockers := collectBlockers(db)
	if len(blockers) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, b := range blockers {
			fmt.Printf("  • %s\n", b)
		}
	}

	printRecentMentions(rc, reg, since)

	maybePostStandup(rc, reg, adapter.StandupReport{
		UserName:  userName,
		Date:      date,
		Yesterday: yesterday,
		Today:     today,
		Blockers:  blockers,
	})

	return nil
}

// printYesterday renders the last-24h activity section and returns its lines.
func printYesterday(entries []store.ActivityEntry) []string {
	fmt.Println("Yesterday:")
	if len(entries) == 0 {
		fmt.Println("  (no tracked activity)")
		return nil
	}
	var yesterday []string
	for _, e := range entries {
		line := fmt.Sprintf("%s: %s", e.SpecID, e.Summary)
		yesterday = append(yesterday, line)
		fmt.Printf("  • %s\n", line)
	}
	return yesterday
}

// printToday renders the Today section (active session, owned specs, requested
// PR reviews) and returns its lines.
func printToday(rc *config.ResolvedConfig, db *store.DB, reg *adapter.Registry) []string {
	fmt.Println("\nToday:")
	var today []string

	recent, _ := db.SessionMostRecent()
	if recent != "" {
		session, _ := db.SessionGet(recent)
		if session != "" {
			line := fmt.Sprintf("Continue %s", recent)
			today = append(today, line)
			fmt.Printf("  • %s\n", line)
		}
	}

	userRole := rc.OwnerRole("")
	for _, s := range collectOwnedSpecs(rc.SpecsRepoDir, userRole, rc.Pipeline()) {
		line := fmt.Sprintf("%s: %s [%s]", s.id, s.title, s.stage)
		today = append(today, line)
		fmt.Printf("  • %s\n", line)
	}

	if len(today) == 0 {
		fmt.Println("  (run 'spec do' to start)")
	}

	repoHandle := rc.IdentityForCategory("repo")
	if repoHandle != "" && rc.HasIntegration("repo") {
		reviews, err := reg.Repo().RequestedReviews(ctx(), repoHandle)
		if err == nil && len(reviews) > 0 {
			fmt.Println("\nPR reviews requested:")
			for _, pr := range reviews {
				fmt.Printf("  • %s #%d: %s\n", pr.Repo, pr.Number, pr.Title)
				today = append(today, fmt.Sprintf("Review %s #%d", pr.Repo, pr.Number))
			}
		}
	}
	return today
}

// printRecentMentions renders comms mentions since the given time, when a
// comms integration is configured. Fetch failures degrade to a warning.
func printRecentMentions(rc *config.ResolvedConfig, reg *adapter.Registry, since time.Time) {
	if !rc.HasIntegration("comms") {
		return
	}
	mentions, err := reg.Comms().FetchMentions(ctx(), since)
	if err != nil {
		warnf("could not fetch comms mentions: %v", err)
		return
	}
	if len(mentions) == 0 {
		return
	}
	fmt.Println("\nRecent mentions:")
	for _, m := range mentions {
		fmt.Printf("  • %s in #%s: %s\n", m.SpecID, m.Channel, m.Preview)
	}
}

// maybePostStandup posts the report to the standup channel when auto-post is
// enabled or the user confirms. Post failures degrade to a warning.
func maybePostStandup(rc *config.ResolvedConfig, reg *adapter.Registry, report adapter.StandupReport) {
	if !rc.HasIntegration("comms") {
		return
	}
	autoPost := false
	if rc.User != nil {
		autoPost = rc.User.Preferences.StandupAutoPost
	}

	should := autoPost
	if !autoPost {
		fmt.Print("\nPost to standup channel? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		should = strings.ToLower(strings.TrimSpace(answer)) == "y"
	}
	if !should {
		return
	}
	if err := reg.Comms().PostStandup(ctx(), report); err != nil {
		warnf("could not post standup: %v", err)
		return
	}
	fmt.Println("✓ Standup posted.")
}

// collectBlockers returns blocker descriptions from recent ejects and stalled specs.
func collectBlockers(db *store.DB) []string {
	entries, err := db.ActivitySince(time.Now().Add(-time.Duration(blockerLookbackDays) * 24 * time.Hour))
	if err != nil {
		return nil
	}

	var blockers []string
	for _, e := range entries {
		if e.EventType == "eject" {
			blockers = append(blockers, fmt.Sprintf("%s: %s", e.SpecID, e.Summary))
		}
	}
	return blockers
}

type ownedSpec struct {
	id    string
	title string
	stage string
}

// collectOwnedSpecs scans spec files for specs in stages owned by the given role.
func collectOwnedSpecs(specsDir, role string, pipe config.PipelineConfig) []ownedSpec {
	if role == "" {
		return nil
	}

	dirEntries, err := os.ReadDir(specsDir)
	if err != nil {
		return nil
	}

	// Terminal stages are not "active" — skip them for the today list
	terminals := pipeline.TerminalStages(pipe)
	terminalSet := make(map[string]bool, len(terminals))
	for _, s := range terminals {
		terminalSet[s] = true
	}

	var owned []ownedSpec
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(specsDir, e.Name()))
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		if terminalSet[meta.Status] || meta.Status == "" {
			continue
		}
		// Check if user's role owns the spec's current stage
		stage := pipe.StageByName(meta.Status)
		if stage != nil && stage.HasOwner(role) {
			owned = append(owned, ownedSpec{
				id:    meta.ID,
				title: meta.Title,
				stage: meta.Status,
			})
		}
	}
	return owned
}
