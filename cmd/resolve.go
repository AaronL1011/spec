package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/thread"
	"github.com/spf13/cobra"
)

var resolveCmd = &cobra.Command{
	Use:   "resolve [id] <thread-id>",
	Short: "Resolve (close) a discussion thread on a spec",
	Long: `Mark a discussion thread resolved. The conversation stays readable but
stops drawing attention. The spec ID is inferred from the focused spec when
omitted:
  spec resolve T-7f3a
  spec resolve SPEC-012 T-7f3a`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runResolve,
}

func init() {
	rootCmd.AddCommand(resolveCmd)
}

func runResolve(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	specID, threadID, _, err := splitThreadActionArgs(args, "spec resolve <id> <thread-id>", false)
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	var resolved thread.Thread
	err = withThreadStore(rc, specID, func(store *thread.SidecarStore) (string, error) {
		t, err := store.Resolve(specID, threadID, threadAuthor(rc))
		if err != nil {
			return "", err
		}
		resolved = t
		return fmt.Sprintf("docs: %s — resolve %s", specID, threadID), nil
	})
	if err != nil {
		return err
	}

	logThreadActivity(rc, specID, fmt.Sprintf("resolved %s", threadID), threadID)
	recipients := excludeIdentity(resolved.Participants(), threadAuthor(rc))
	notifyThreadParticipants(p, rc, specID, recipients,
		fmt.Sprintf("%s resolved §%s: %s", threadAuthor(rc), resolved.Section, resolved.Question))

	if p.JSONEnabled() {
		return p.JSON(resolved)
	}
	p.Line("✓ Resolved %s on %s", threadID, specID)
	return nil
}

// splitThreadActionArgs resolves (specID, threadID, trailing) from positional
// args for answer/resolve. A thread ID is recognised by its "T-" prefix, so a
// leading spec ID is optional. When wantTrailing is true, any args after the
// thread ID are joined as the trailing value (the reply body).
func splitThreadActionArgs(args []string, usage string, wantTrailing bool) (specID, threadID, trailing string, err error) {
	if len(args) == 0 {
		return "", "", "", fmt.Errorf("missing thread id — usage: %s", usage)
	}

	rest := args
	// Leading spec ID is present only when the first arg is not a thread ID.
	if !isThreadID(args[0]) {
		specID = normalizeSpecID(args[0])
		rest = args[1:]
	} else {
		id, e := resolveSpecIDArg(nil, usage)
		if e != nil {
			return "", "", "", e
		}
		specID = id
	}

	if len(rest) == 0 || !isThreadID(rest[0]) {
		return "", "", "", fmt.Errorf("missing thread id (e.g. T-7f3a) — usage: %s", usage)
	}
	threadID = rest[0]
	if wantTrailing && len(rest) > 1 {
		trailing = rest[1]
	}
	return specID, threadID, trailing, nil
}

// isThreadID reports whether s looks like a thread identifier (e.g. "T-7f3a").
func isThreadID(s string) bool {
	return len(s) > 2 && (s[:2] == "T-" || s[:2] == "t-")
}
