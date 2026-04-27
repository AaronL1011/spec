package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link [id]",
	Short: "Attach a resource link to a spec section",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLink,
}

func init() {
	linkCmd.Flags().String("section", "", "section to attach the link to (required)")
	linkCmd.Flags().String("url", "", "resource URL (required)")
	linkCmd.Flags().String("label", "", "optional label for the link")
	rootCmd.AddCommand(linkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec link <id>")
	if err != nil {
		return err
	}
	section, _ := cmd.Flags().GetString("section")
	url, _ := cmd.Flags().GetString("url")
	label, _ := cmd.Flags().GetString("label")

	if section == "" {
		return fmt.Errorf("--section is required — specify which section to attach the link to")
	}
	if url == "" {
		return fmt.Errorf("--url is required — provide the resource URL")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	return gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}

		sections, err := markdown.ExtractSectionsFromFile(path)
		if err != nil {
			return "", err
		}

		targetSection := markdown.FindSection(sections, section)
		if targetSection == nil {
			return "", fmt.Errorf("section %q not found in %s", section, specID)
		}

		// Build the link entry
		linkText := url
		if label != "" {
			linkText = fmt.Sprintf("[%s](%s)", label, url)
		}
		entry := fmt.Sprintf("\n- %s — added by %s on %s\n",
			linkText, rc.UserName(), time.Now().Format("2006-01-02"))

		// Append to section
		newContent := strings.TrimRight(targetSection.Content, "\n") + entry
		if err := markdown.ReplaceSection(path, section, newContent); err != nil {
			return "", err
		}

		fmt.Printf("✓ Link attached to %s §%s\n", specID, section)
		return fmt.Sprintf("docs: %s — link attached to %s", specID, section), nil
	})
}
