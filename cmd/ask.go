package cmd

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/thread"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask [id] \"<question>\"",
	Short: "Ask a question on a spec section (inline discussion)",
	Long: `Ask a section-anchored question on a spec, creating an open discussion thread.

Threads are lightweight conversation — a question, replies, and an open/resolved
flag — stored alongside the spec and synced via the specs repo. Use 'spec answer'
to reply and 'spec resolve' to close a thread.`,
	Args: cobra.MaximumNArgs(2),
	RunE: runAsk,
}

func init() {
	askCmd.Flags().String("section", "", "section slug to anchor the question to (e.g. 'technical_implementation')")
	askCmd.Flags().Bool("list", false, "list all discussion threads for the spec")
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	listMode, _ := cmd.Flags().GetBool("list")
	section, _ := cmd.Flags().GetString("section")

	// The first positional arg is the spec ID when it looks like one;
	// otherwise it is treated as the question and the ID comes from focus.
	specID, question, err := splitAskArgs(args)
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if listMode {
		return listThreads(p, rc, specID)
	}
	if question == "" {
		return fmt.Errorf("no question provided — usage: spec ask <id> --section <slug> \"<question>\"")
	}
	if section == "" {
		return fmt.Errorf("--section is required — anchor the question to a section slug (see 'spec ask <id> --list')")
	}

	var created thread.Thread
	err = withThreadStore(rc, specID, func(store *thread.SidecarStore) (string, error) {
		t, err := store.Create(specID, section, threadAuthor(rc), question, nil)
		if err != nil {
			return "", err
		}
		created = t
		return fmt.Sprintf("docs: %s — ask %s [%s]: %s", specID, t.ID, section, question), nil
	})
	if err != nil {
		return err
	}

	logThreadActivity(rc, specID, fmt.Sprintf("asked %s on §%s", created.ID, section), created.ID)
	notifyThreadParticipants(p, rc, specID,
		fmt.Sprintf("%s asked on §%s: %s", threadAuthor(rc), section, question))

	if p.JSONEnabled() {
		return p.JSON(created)
	}
	p.Line("✓ Asked %s on §%s of %s", created.ID, section, specID)
	p.Line("  Reply with: spec answer %s \"...\"", created.ID)
	return nil
}

// splitAskArgs resolves the spec ID and question from positional args,
// supporting both "spec ask SPEC-1 \"q\"" and "spec ask \"q\"" (focused spec).
func splitAskArgs(args []string) (specID, question string, err error) {
	switch len(args) {
	case 0:
		id, err := resolveSpecIDArg(nil, "spec ask <id> --section <slug> \"<question>\"")
		return id, "", err
	case 1:
		if looksLikeSpecID(args[0]) {
			return normalizeSpecID(args[0]), "", nil
		}
		id, err := resolveSpecIDArg(nil, "spec ask <id> --section <slug> \"<question>\"")
		return id, args[0], err
	default:
		return normalizeSpecID(args[0]), args[1], nil
	}
}

// looksLikeSpecID reports whether s is a spec identifier (e.g. "SPEC-12").
func looksLikeSpecID(s string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "SPEC-")
}

func listThreads(p *printer, rc *config.ResolvedConfig, specID string) error {
	if rc.SpecsRepoDir == "" {
		return fmt.Errorf("specs repo not configured — ensure spec.config.yaml has specs_repo settings")
	}
	// sidecarDirFor is the same resolver withThreadStore uses (cmd/thread.go),
	// so a sidecar written on an archived spec is never looked up in the
	// wrong directory.
	dir, err := sidecarDirFor(rc.SpecsRepoDir, rc, specID)
	if err != nil {
		return err
	}
	store := thread.NewSidecarStore(dir)
	threads, err := store.List(specID)
	if err != nil {
		return err
	}

	if p.JSONEnabled() {
		return p.JSON(threads)
	}
	if len(threads) == 0 {
		p.Line("No discussion threads for %s.", specID)
		p.Line("Start one with: spec ask %s --section <slug> \"...\"", specID)
		return nil
	}

	p.Line("Discussion threads for %s:\n", specID)
	for _, t := range threads {
		marker := "●"
		state := "open"
		if !t.IsOpen() {
			marker, state = "✓", "resolved"
		}
		p.Line("%s %s  §%s  (%s)", marker, t.ID, t.Section, state)
		p.Line("    %s — %s", t.Author, t.Question)
		for _, r := range t.Replies {
			p.Line("      ↳ %s: %s", r.Author, r.Body)
		}
	}
	return nil
}
