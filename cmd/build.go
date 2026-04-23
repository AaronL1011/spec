package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/build"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build <id>",
	Short: "Start or resume the build phase for a spec",
	Long: `Start or resume implementation work for a spec in the build phase.

The command validates the spec stage, resolves the spec source from local
or team repository state, and launches the build engine session.`,
	Example: "  spec build SPEC-042",
	Args:  cobra.ExactArgs(1),
	RunE:  runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])

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

	// Validate spec is at build or engineering stage
	if meta.Status != "build" && meta.Status != "engineering" {
		return fmt.Errorf("%s is at %q stage — advance to 'build' before starting: spec advance %s",
			specID, meta.Status, specID)
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	reg := buildRegistry(rc)
	engine := build.NewEngine(db, reg.Agent())

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	return engine.StartOrResume(ctx(), specID, specPath, workDir)
}
