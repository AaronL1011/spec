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
	userHandle := rc.UserHandle()
	userRole := rc.OwnerRole("")
	date := time.Now().Format("2006-01-02")

	fmt.Printf("Your standup — %s — %s\n", userName, date)
	fmt.Println("────────────────────────────────────────────────")

	// Yesterday
	fmt.Println("Yesterday:")
	var yesterday []string
	if len(entries) == 0 {
		fmt.Println("  (no tracked activity)")
	} else {
		for _, e := range entries {
			line := fmt.Sprintf("%s: %s", e.SpecID, e.Summary)
			yesterday = append(yesterday, line)
			fmt.Printf("  • %s\n", line)
		}
	}

	// Today (from active session + owned specs in active stages)
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

	// Owned specs in active stages
	ownedSpecs := collectOwnedSpecs(rc.SpecsRepoDir, userRole, rc.Pipeline())
	for _, s := range ownedSpecs {
		line := fmt.Sprintf("%s: %s [%s]", s.id, s.title, s.stage)
		today = append(today, line)
		fmt.Printf("  • %s\n", line)
	}

	if len(today) == 0 {
		fmt.Println("  (run 'spec do' to start)")
	}

	// PR review requests
	if userHandle != "" && rc.HasIntegration("repo") {
		reviews, err := reg.Repo().RequestedReviews(ctx(), userHandle)
		if err == nil && len(reviews) > 0 {
			fmt.Println("\nPR reviews requested:")
			for _, pr := range reviews {
				fmt.Printf("  • %s #%d: %s\n", pr.Repo, pr.Number, pr.Title)
				today = append(today, fmt.Sprintf("Review %s #%d", pr.Repo, pr.Number))
			}
		}
	}

	// Blockers (from blocked/ejected specs)
	fmt.Println("\nBlockers:")
	blockers := collectBlockers(db)
	if len(blockers) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, b := range blockers {
			fmt.Printf("  • %s\n", b)
		}
	}

	// Mentions from comms
	if rc.HasIntegration("comms") {
		mentions, err := reg.Comms().FetchMentions(ctx(), since)
		if err != nil {
			warnf("could not fetch comms mentions: %v", err)
		} else if len(mentions) > 0 {
			fmt.Println("\nRecent mentions:")
			for _, m := range mentions {
				fmt.Printf("  • %s in #%s: %s\n", m.SpecID, m.Channel, m.Preview)
			}
		}
	}

	// Post option
	if rc.HasIntegration("comms") {
		autoPost := false
		if rc.User != nil {
			autoPost = rc.User.Preferences.StandupAutoPost
		}

		standupReport := adapter.StandupReport{
			UserName:  userName,
			Date:      date,
			Yesterday: yesterday,
			Today:     today,
			Blockers:  blockers,
		}

		should := autoPost
		if !autoPost {
			fmt.Print("\nPost to standup channel? [y/N] ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			should = strings.ToLower(strings.TrimSpace(answer)) == "y"
		}

		if should {
			if err := reg.Comms().PostStandup(ctx(), standupReport); err != nil {
				warnf("could not post standup: %v", err)
			} else {
				fmt.Println("✓ Standup posted.")
			}
		}
	}

	return nil
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
