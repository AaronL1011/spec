package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/thread"
	"github.com/spf13/cobra"
)

var answerCmd = &cobra.Command{
	Use:   "answer [id] <thread-id> \"<reply>\"",
	Short: "Reply to a discussion thread on a spec",
	Long: `Append a reply to an open discussion thread.

The spec ID is inferred from the focused spec when omitted:
  spec answer T-7f3a "because we run multiple instances"
  spec answer SPEC-012 T-7f3a "..."`,
	Args: cobra.RangeArgs(1, 3),
	RunE: runAnswer,
}

func init() {
	answerCmd.Flags().StringSlice("to", nil, "handle to notify (repeatable) — inline @handle in the reply works automatically")
	rootCmd.AddCommand(answerCmd)
}

func runAnswer(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	to, _ := cmd.Flags().GetStringSlice("to")

	specID, threadID, body, err := splitThreadActionArgs(args, "spec answer <id> <thread-id> \"<reply>\"", true)
	if err != nil {
		return err
	}
	if body == "" {
		return fmt.Errorf("no reply provided — usage: spec answer <thread-id> \"<reply>\"")
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	var updated thread.Thread
	err = withThreadStore(rc, specID, func(store *thread.SidecarStore) (string, error) {
		t, err := store.Reply(specID, threadID, threadAuthor(rc), body, to)
		if err != nil {
			return "", err
		}
		updated = t
		return fmt.Sprintf("docs: %s — reply on %s: %s", specID, threadID, body), nil
	})
	if err != nil {
		return err
	}

	logThreadActivity(rc, specID, fmt.Sprintf("replied to %s", threadID), threadID)
	// A reply notifies the asker and prior repliers, not just whoever was
	// @-mentioned in this reply — the whole point of a reply is that the
	// people already in the conversation see it.
	recipients := excludeIdentity(updated.Participants(), threadAuthor(rc))
	notifyThreadParticipants(p, rc, specID, recipients,
		fmt.Sprintf("%s replied on §%s: %s", threadAuthor(rc), updated.Section, body))

	if p.JSONEnabled() {
		return p.JSON(updated)
	}
	p.Line("✓ Replied to %s on %s", threadID, specID)
	return nil
}
