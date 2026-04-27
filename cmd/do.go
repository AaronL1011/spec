package cmd

import (
	"fmt"
	"os"

	"github.com/aaronl1011/spec-cli/internal/awareness"
	"github.com/aaronl1011/spec-cli/internal/build"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var doCmd = &cobra.Command{
	Use:   "do [id]",
	Short: "Resume work — picks up where you left off",
	Long: `Resume active build work with full execution context.

When no spec ID is provided, this command detects the current spec from
your branch name first, then falls back to the most recent local build
session.`,
	Example: "  spec do\n  spec do SPEC-042",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runDo,
}

func init() {
	rootCmd.AddCommand(doCmd)
}

func runDo(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Show passive awareness (unless user disabled it during build)
	if rc.User == nil || rc.User.Preferences.ShowPassiveAwarenessDuringBuild() {
		awareness.Print(rc)
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	specID, err := resolveSpecIDFromArgs(args)
	if err != nil {
		return fmt.Errorf("no active build session found — start one with 'spec build <id>' or set one with 'spec focus <id>'")
	}

	// Find the spec
	specPath, err := resolveLocalSpecPath(specID)
	if err != nil {
		specPath, err = resolveSpecPath(rc, specID)
		if err != nil {
			return fmt.Errorf("%s not found — run 'spec pull %s' to fetch it", specID, specID)
		}
	}

	// Validate spec is at an appropriate stage (build, engineering, or has an active session)
	meta, err := markdown.ReadMeta(specPath)
	if err != nil {
		return err
	}
	hasSession, _ := db.SessionGet(specID)
	if hasSession == "" && meta.Status != "build" && meta.Status != "engineering" {
		return fmt.Errorf("%s is at %q stage — advance to 'build' before starting: spec advance %s",
			specID, meta.Status, specID)
	}

	reg := buildRegistry(rc)
	engine := build.NewEngine(db, reg.Agent())

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	return engine.StartOrResume(ctx(), specID, specPath, workDir)
}
