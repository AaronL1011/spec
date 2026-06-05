package cmd

import (
	"fmt"
	"os"

	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build [id]",
	Short: "Start or resume the build phase for a spec",
	Long: `Start or resume implementation work for a spec in the build phase.

The command validates the spec stage, resolves the spec source from local
or team repository state, and launches the build engine session.`,
	Example: "  spec build SPEC-042",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runBuild,
}

func init() {
	buildCmd.Flags().Bool("restart", false, "discard the existing build session and re-derive steps from the spec's PR stack")
	buildCmd.Flags().Bool("check", false, "preflight the build (DAG, workspaces, skill routing, capabilities) without launching the agent")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec build <id>")
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Check spec exists and is at build stage
	specPath, err := resolveLocalSpecPath(specID)
	if err != nil {
		// Try from specs repo
		specPath, err = resolveSpecPath(rc, specID)
		if err != nil {
			return fmt.Errorf("%s not found — run 'spec pull %s' to fetch it", specID, specID)
		}
	}

	meta, err := markdown.ReadMeta(specPath)
	if err != nil {
		return err
	}

	check, _ := cmd.Flags().GetBool("check")

	// Validate spec is at build or engineering stage. A preflight (--check) is
	// allowed earlier so engineers can validate wiring before advancing.
	if !check && meta.Status != "build" && meta.Status != "engineering" {
		return fmt.Errorf("%s is at %q stage — advance to 'build' before starting: spec advance %s",
			specID, meta.Status, specID)
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	reg := buildRegistry(rc)
	engine := build.NewEngine(db, reg.Agent(), buildEngineOptions(rc, false))

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	if check {
		return engine.Check(ctx(), specID, specPath, workDir)
	}

	if restart, _ := cmd.Flags().GetBool("restart"); restart {
		if err := db.SessionDelete(specID); err != nil {
			return fmt.Errorf("clearing build session for %s: %w", specID, err)
		}
		fmt.Printf("Cleared existing build session for %s — re-deriving steps.\n", specID)
	}

	return engine.StartOrResume(ctx(), specID, specPath, workDir)
}
