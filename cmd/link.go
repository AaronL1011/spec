package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/hierarchy"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link [id]",
	Short: "Attach a resource link, a PM epic, or a parent initiative to a spec",
	Example: "  spec link SPEC-009 --section design_inputs --url https://figma.com/…\n" +
		"  spec link SPEC-009 --parent SPEC-004\n" +
		"  spec link SPEC-009 --parent \"\"",
	Args: cobra.MaximumNArgs(1),
	RunE: runLink,
}

func init() {
	linkCmd.Flags().String("section", "", "section to attach the link to (required)")
	linkCmd.Flags().String("url", "", "resource URL (required)")
	linkCmd.Flags().String("label", "", "optional label for the link")
	linkCmd.Flags().String("epic", "", "adopt an existing PM epic key (e.g. PLAT-123) for this spec")
	linkCmd.Flags().String("parent", "", "attach this spec as a slice of an initiative spec (empty string detaches)")
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
	epic, _ := cmd.Flags().GetString("epic")

	if epic != "" {
		return runLinkEpic(specID, epic)
	}
	// Changed rather than non-empty: `--parent ""` is the detach verb, and an
	// omitted flag must not be read as a request to detach.
	if cmd.Flags().Changed("parent") {
		parent, _ := cmd.Flags().GetString("parent")
		return runLinkParent(cmd, specID, normalizeSpecID(parent))
	}

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

	return gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
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

// runLinkParent attaches a spec to an initiative as one of its deliverable
// slices, or detaches it when parentID is empty.
//
// Every precondition is checked before the write (internal/hierarchy.Link), so
// a refused link never leaves half a relationship on disk. Detaching is always
// allowed and is the escape hatch for every refusal.
func runLinkParent(cmd *cobra.Command, specID, parentID string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}

	return gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		graph, err := loadHierarchyIn(specsDir(repoPath), rc)
		if err != nil {
			return "", err
		}
		if err := hierarchy.Link(graph, specID, parentID, rc.Pipeline()); err != nil {
			return "", err
		}

		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := readSpecMeta(path)
		if err != nil {
			return "", err
		}
		if meta.Parent == parentID {
			fmt.Printf("%s is already %s\n", specID, parentDescription(parentID))
			return "", nil
		}

		previous := meta.Parent
		meta.Parent = parentID
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}

		if parentID == "" {
			fmt.Printf("✓ Detached %s from %s\n", specID, previous)
			warnPMUnchangedOnDetach(meta.EpicKey, specID)
			return fmt.Sprintf("chore: detach %s from %s", specID, previous), nil
		}
		parent, _ := graph.Get(parentID)
		fmt.Printf("✓ %s is now a slice of %s", specID, parentID)
		if parent.Title != "" {
			fmt.Printf(" — %s", parent.Title)
		}
		fmt.Println()
		return fmt.Sprintf("chore: link %s to parent %s", specID, parentID), nil
	})
}

// parentDescription renders the target of a no-op link for the user.
func parentDescription(parentID string) string {
	if parentID == "" {
		return "standalone"
	}
	return "a slice of " + parentID
}

// warnPMUnchangedOnDetach tells the author that detaching moved the spec link
// only. The PM object stays a task under the old epic: converting a PM issue's
// type is lossy and provider-specific, so board hygiene is left to a human.
func warnPMUnchangedOnDetach(pmKey, specID string) {
	if pmKey == "" {
		return
	}
	warnf("%s is detached in the spec, but its PM object %s still sits under the old epic — move it on the board if that matters", specID, pmKey)
}

// runLinkEpic adopts an existing PM epic for a spec: it records the epic key in
// the spec frontmatter and sets a back-link on the PM issue, without creating a
// new epic. Used when work originated in the PM tool.
func runLinkEpic(specID, epic string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}
	if !rc.HasIntegration("pm") {
		return fmt.Errorf("PM integration not configured — set integrations.pm in spec.config.yaml before adopting an epic")
	}

	if err := persistEpicKey(rc, specID, epic); err != nil {
		return fmt.Errorf("recording epic %s on %s: %w", epic, specID, err)
	}

	reg := buildRegistry(rc)
	if err := reg.PM().LinkEpic(context.Background(), epic, specID, specBackLinkURL(rc, specID)); err != nil {
		warnf("epic linked locally but PM back-link failed: %v", err)
	}

	fmt.Printf("✓ Adopted PM epic %s for %s\n", epic, specID)
	return nil
}
