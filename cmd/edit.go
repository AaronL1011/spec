package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Open a spec in $EDITOR or print docs provider URL",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEdit,
}

func init() {
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec edit <id>")
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	// For non-engineer roles with docs provider, offer the URL
	role, _ := requireRole(rc)
	if role != "engineer" && role != "tl" && rc.HasIntegration("docs") {
		reg := buildRegistry(rc)
		url, err := reg.Docs().PageURL(ctx(), specID)
		if err == nil && url != "" {
			fmt.Printf("Edit in docs: %s\n", url)
			fmt.Printf("Or edit locally: spec edit %s --role engineer\n", specID)
			return nil
		}
	}

	editor := os.Getenv("EDITOR")
	if rc.User != nil && rc.User.Preferences.Editor != "" {
		editor = rc.User.Preferences.Editor
	}
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, path)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	return editorCmd.Run()
}
