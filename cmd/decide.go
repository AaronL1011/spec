package cmd

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec-cli/internal/config"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var decideCmd = &cobra.Command{
	Use:   "decide [id]",
	Short: "Manage the decision log for a spec",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDecide,
}

func init() {
	decideCmd.Flags().String("question", "", "append a new question to the decision log")
	decideCmd.Flags().Int("resolve", 0, "resolve an existing decision by number")
	decideCmd.Flags().String("decision", "", "the decision made (used with --resolve)")
	decideCmd.Flags().String("rationale", "", "rationale for the decision (used with --resolve)")
	decideCmd.Flags().Bool("list", false, "display the current decision log")
	rootCmd.AddCommand(decideCmd)
}

func runDecide(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec decide <id>")
	if err != nil {
		return err
	}
	question, _ := cmd.Flags().GetString("question")
	resolveNum, _ := cmd.Flags().GetInt("resolve")
	decision, _ := cmd.Flags().GetString("decision")
	rationale, _ := cmd.Flags().GetString("rationale")
	listMode, _ := cmd.Flags().GetBool("list")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if listMode {
		return listDecisions(rc, specID)
	}

	if question != "" {
		return addQuestion(rc, specID, question)
	}

	if resolveNum > 0 {
		if decision == "" {
			return fmt.Errorf("--decision is required when resolving — what was decided?")
		}
		return resolveDecision(rc, specID, resolveNum, decision, rationale)
	}

	return fmt.Errorf("use --question, --resolve, or --list — run 'spec decide --help' for usage")
}

func listDecisions(rc *config.ResolvedConfig, specID string) error {
	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	entries, err := markdown.ParseDecisionLogFromFile(path)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Printf("No decisions recorded for %s.\n", specID)
		fmt.Printf("Add one with: spec decide %s --question \"...\"\n", specID)
		return nil
	}

	fmt.Printf("Decision log for %s:\n\n", specID)
	fmt.Print(markdown.FormatDecisionTable(entries))
	return nil
}

func addQuestion(rc *config.ResolvedConfig, specID, question string) error {
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	var decisionNum int
	err := gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		num, err := markdown.AppendDecision(path, question, rc.UserName())
		if err != nil {
			return "", err
		}
		decisionNum = num

		return fmt.Sprintf("docs: %s — decision #%03d: %s", specID, num, question), nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("✓ Decision #%03d added to %s\n", decisionNum, specID)
	fmt.Printf("  Question: %s\n", question)
	fmt.Printf("  Resolve with: spec decide %s --resolve %d --decision \"...\" --rationale \"...\"\n", specID, decisionNum)

	if db, dbErr := openDB(); dbErr == nil {
		defer func() { _ = db.Close() }()
		metaJSON := fmt.Sprintf(`{"number":%d}`, decisionNum)
		_ = db.ActivityLog(specID, "decide", fmt.Sprintf("decision #%03d: %s", decisionNum, question), metaJSON, rc.UserName())
	}

	return nil
}

func resolveDecision(rc *config.ResolvedConfig, specID string, number int, decision, rationale string) error {
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		if err := markdown.ResolveDecision(path, number, decision, rationale, rc.UserName()); err != nil {
			return "", err
		}

		fmt.Printf("✓ Decision #%03d resolved in %s\n", number, specID)
		fmt.Printf("  Decision: %s\n", decision)
		if rationale != "" {
			fmt.Printf("  Rationale: %s\n", rationale)
		}

		if db, dbErr := openDB(); dbErr == nil {
			defer func() { _ = db.Close() }()
			metaJSON := fmt.Sprintf(`{"number":%d}`, number)
			_ = db.ActivityLog(specID, "decide_resolve", fmt.Sprintf("resolved #%03d: %s", number, decision), metaJSON, rc.UserName())
		}

		return fmt.Sprintf("docs: %s — resolve decision #%03d: %s", specID, number, decision), nil
	})
}
