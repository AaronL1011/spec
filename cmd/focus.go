package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var focusCmd = &cobra.Command{
	Use:   "focus [id]",
	Short: "Set or clear the focused spec for future commands",
	Long: `Set the focused spec for future commands.

When a spec is focused, spec-scoped commands can omit the spec ID. Passing
an explicit ID to a command still overrides the focused spec for that one
invocation.`,
	Example: "  spec focus SPEC-042\n  spec focus --clear",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runFocus,
}

func init() {
	focusCmd.Flags().Bool("clear", false, "clear the focused spec")
	rootCmd.AddCommand(focusCmd)
}

func runFocus(cmd *cobra.Command, args []string) error {
	clearFocus, _ := cmd.Flags().GetBool("clear")
	if clearFocus {
		if len(args) > 0 {
			return fmt.Errorf("cannot pass a spec ID with --clear")
		}
		db, err := openDB()
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		if err := db.FocusedSpecClear(); err != nil {
			return err
		}
		fmt.Println("Focused spec cleared.")
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("no spec ID provided — use 'spec focus <id>' or 'spec focus --clear'")
	}

	specID := normalizeSpecID(args[0])
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if _, err := resolveLocalSpecPath(specID); err != nil {
		if _, err := resolveSpecPath(rc, specID); err != nil {
			return err
		}
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.FocusedSpecSet(specID); err != nil {
		return err
	}

	fmt.Printf("Focused spec set to %s.\n", specID)
	fmt.Println("Spec-scoped commands can now omit the spec ID.")
	return nil
}
