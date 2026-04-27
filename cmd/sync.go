package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	syncengine "github.com/aaronl1011/spec-cli/internal/sync"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [id]",
	Short: "Bidirectional section-scoped sync with external tools",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSync,
}

func init() {
	syncCmd.Flags().String("direction", "both", "sync direction: in | out | both")
	syncCmd.Flags().Bool("dry-run", false, "preview changes without applying")
	syncCmd.Flags().Bool("force", false, "force inbound changes on conflict")
	syncCmd.Flags().Bool("skip", false, "skip conflicting sections")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec sync <id>")
	if err != nil {
		return err
	}
	direction, _ := cmd.Flags().GetString("direction")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	skip, _ := cmd.Flags().GetBool("skip")
	if force && skip {
		return fmt.Errorf("--force and --skip cannot be used together")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	if !rc.HasIntegration("docs") {
		return fmt.Errorf("docs integration not configured; sync requires a docs provider, configure 'integrations.docs' in spec.config.yaml")
	}

	role, err := requireRole(rc)
	if err != nil {
		return err
	}
	strategy := rc.Team.Sync.ConflictStrategy
	if force {
		strategy = syncengine.ConflictForce
	}
	if skip {
		strategy = syncengine.ConflictSkip
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	reg := buildRegistry(rc)
	engine := syncengine.NewEngine(reg.Docs(), db)
	var prepared *syncengine.PreparedRun

	err = gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		prepared, err = engine.Prepare(context.Background(), syncengine.Options{
			SpecID:           specID,
			SpecPath:         path,
			Direction:        direction,
			ConflictStrategy: strategy,
			OwnerRole:        role,
			UserName:         rc.UserName(),
			DryRun:           dryRun,
		})
		if err != nil && !errors.Is(err, syncengine.ErrSyncConflict) {
			return "", err
		}
		if dryRun || prepared == nil || prepared.Report == nil || len(prepared.Report.InboundApplied) == 0 {
			return "", err
		}
		return fmt.Sprintf("chore: sync %s from docs", specID), err
	})
	if err != nil {
		if prepared != nil && prepared.Report != nil {
			printSyncReport(prepared.Report)
		}
		if errors.Is(err, syncengine.ErrSyncConflict) {
			return fmt.Errorf("sync conflicts detected — rerun with --force to accept remote changes or --skip to leave conflicts unchanged")
		}
		return err
	}
	if prepared != nil {
		if err := engine.Finalize(context.Background(), prepared); err != nil {
			return err
		}
		printSyncReport(prepared.Report)
	}
	return nil
}

func printSyncReport(report *syncengine.Report) {
	mode := "applied"
	if report.DryRun {
		mode = "previewed"
	}
	fmt.Printf("✓ Sync %s for %s (%s)\n", mode, report.SpecID, report.Direction)
	if report.PageMissing {
		fmt.Println("  Remote page: missing")
	}
	if report.OutboundPushed {
		if report.DryRun {
			fmt.Printf("  Outbound: would push %d sections\n", len(report.OutboundSections))
		} else {
			fmt.Printf("  Outbound: pushed %d sections\n", len(report.OutboundSections))
		}
	}
	if len(report.InboundApplied) > 0 {
		fmt.Printf("  Inbound: %s\n", strings.Join(report.InboundApplied, ", "))
	}
	if len(report.Conflicts) > 0 {
		fmt.Println("  Conflicts:")
		for _, conflict := range report.Conflicts {
			fmt.Printf("    - %s: %s\n", conflict.Section, conflict.Reason)
		}
	}
	if len(report.Skipped) > 0 {
		fmt.Println("  Skipped:")
		for _, skipped := range report.Skipped {
			fmt.Printf("    - %s: %s\n", skipped.Section, skipped.Reason)
		}
	}
}
