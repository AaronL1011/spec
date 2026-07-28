package llm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNotInteractive reports that the review gate needs a terminal it does not
// have. Scripted callers pass --accept instead of being silently prompted at a
// pipe that will never answer.
var ErrNotInteractive = errors.New("draft review needs an interactive terminal")

// CLIPrompter renders the review gate at a terminal. It is one of two renderers
// of the same state machine; the TUI modal is the other, and the keys mirror
// deliberately so the loop is learned once and works over SSH.
type CLIPrompter struct {
	// Out receives the rendered draft. Defaults to os.Stdout.
	Out io.Writer
	// In supplies keystrokes. Defaults to os.Stdin.
	In io.Reader
	// Title labels the artifact under review ("Problem Statement").
	Title string
	// Editor overrides $EDITOR for the edit action.
	Editor string

	reader *bufio.Reader
}

// NewCLIPrompter builds a terminal prompter for a titled artifact.
func NewCLIPrompter(title, editor string) *CLIPrompter {
	return &CLIPrompter{Out: os.Stdout, In: os.Stdin, Title: title, Editor: editor}
}

func (c *CLIPrompter) out() io.Writer {
	if c.Out == nil {
		return os.Stdout
	}
	return c.Out
}

// writef renders gate chrome. Write errors are deliberately ignored: a broken
// stdout means the user cannot see the prompt at all, and there is no better
// recovery than continuing to read their answer — failing the review because a
// terminal write failed would lose the draft for no benefit.
func (c *CLIPrompter) writef(format string, args ...any) {
	_, _ = fmt.Fprintf(c.out(), format, args...)
}

func (c *CLIPrompter) in() *bufio.Reader {
	if c.reader == nil {
		src := c.In
		if src == nil {
			src = os.Stdin
		}
		c.reader = bufio.NewReader(src)
	}
	return c.reader
}

// Show renders one attempt and reads the reviewer's choice.
func (c *CLIPrompter) Show(a Attempt, index, total int, caps GateCapabilities) (Action, error) {
	c.writef("\n")
	header := fmt.Sprintf("─── DRAFT · %s ", c.Title)
	if total > 1 {
		header += fmt.Sprintf("· attempt %d/%d ", index+1, total)
	}
	c.writef("%s%s\n", header, strings.Repeat("─", maxInt(0, 60-len(header))))

	for _, line := range strings.Split(strings.TrimRight(a.Content, "\n"), "\n") {
		c.writef(" %s\n", line)
	}

	c.writef("%s\n", strings.Repeat("─", 60))
	if status := statusLine(a); status != "" {
		c.writef(" %s\n", status)
	}
	if a.Note != "" {
		c.writef(" steer: %s\n", a.Note)
	}

	c.writef(" %s ", footer(caps))

	answer, err := c.in().ReadString('\n')
	if err != nil && answer == "" {
		// A closed stdin at the gate means nobody can answer: fail loudly
		// rather than silently choosing an action on the user's behalf.
		return "", ErrNotInteractive
	}

	// Case is significant: r and R are different actions, mirroring the TUI
	// modal's lowercase/uppercase pairing. Only the long words are folded.
	raw := strings.TrimSpace(answer)
	switch raw {
	case "R":
		return ActionRetryNote, nil
	case "[":
		return ActionPrev, nil
	case "]":
		return ActionNext, nil
	}

	switch strings.ToLower(raw) {
	case "y", "yes", "a", "accept":
		return ActionAccept, nil
	case "e", "edit":
		return ActionEdit, nil
	case "r", "retry":
		return ActionRetry, nil
	case "retry-note":
		return ActionRetryNote, nil
	case "i", "interactive":
		return ActionEscalate, nil
	case "p", "prev":
		return ActionPrev, nil
	case "n", "next":
		return ActionNext, nil
	case "s", "skip", "":
		return ActionSkip, nil
	default:
		// An unrecognised key must not discard the draft. Re-showing is the
		// forgiving behaviour; the old gate defaulted to skip.
		c.Notify("unrecognised choice — press a, e, r, R, s" + escapeHint(caps))
		return ActionNext, nil
	}
}

// Note collects a one-line steer.
func (c *CLIPrompter) Note() (string, error) {
	c.writef(" steer (one line): ")
	note, err := c.in().ReadString('\n')
	if err != nil && note == "" {
		return "", ErrNotInteractive
	}
	return strings.TrimSpace(note), nil
}

// Notify prints a non-fatal condition without ending the review.
func (c *CLIPrompter) Notify(message string) {
	c.writef(" %s\n", message)
}

// footer renders the available actions. Uppercase R is distinct from r, so the
// hint spells both rather than implying one key.
func footer(caps GateCapabilities) string {
	parts := []string{"[a]ccept", "[e]dit", "[r]etry", "[R]etry+note"}
	if caps.CanNavigate {
		parts = append(parts, "[/] attempts")
	}
	if caps.CanEscalate {
		parts = append(parts, "[i]nteractive")
	}
	parts = append(parts, "[s]kip")
	return strings.Join(parts, " ")
}

func escapeHint(caps GateCapabilities) string {
	if caps.CanEscalate {
		return ", i"
	}
	return ""
}

// statusLine reports provider cost for the shown attempt: model, elapsed time,
// and tokens when the provider reported them. Local models can take tens of
// seconds, so showing the cost is what makes a slow draft legible rather than
// suspicious.
func statusLine(a Attempt) string {
	if a.Result == nil {
		return ""
	}
	var parts []string
	if a.Result.Model != "" {
		parts = append(parts, a.Result.Model)
	}
	if a.Result.Duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", a.Result.Duration.Seconds()))
	}
	if a.Result.Tokens.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens", a.Result.Tokens.Total))
	} else if a.Result.Tokens.Output > 0 {
		parts = append(parts, fmt.Sprintf("%d out tokens", a.Result.Tokens.Output))
	}
	if a.Edited {
		parts = append(parts, "edited")
	}
	return strings.Join(parts, " · ")
}

// IsInteractive reports whether stdin is a terminal. Callers use it to choose
// between the gate and an explicit non-interactive path.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
