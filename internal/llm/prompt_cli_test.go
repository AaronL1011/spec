package llm

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

func promptWith(input string) (*CLIPrompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &CLIPrompter{Out: out, In: strings.NewReader(input), Title: "Problem Statement"}, out
}

// The CLI keys mirror the TUI modal exactly, so the loop is learned once and
// works over SSH. Case matters: r and R are different actions.
func TestCLIPrompter_KeyMapping(t *testing.T) {
	tests := []struct {
		in   string
		want Action
	}{
		{"a\n", ActionAccept},
		{"y\n", ActionAccept},
		{"accept\n", ActionAccept},
		{"e\n", ActionEdit},
		{"r\n", ActionRetry},
		{"R\n", ActionRetryNote},
		{"i\n", ActionEscalate},
		{"[\n", ActionPrev},
		{"]\n", ActionNext},
		{"s\n", ActionSkip},
		{"\n", ActionSkip},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.in), func(t *testing.T) {
			p, _ := promptWith(tt.in)
			got, err := p.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{CanEscalate: true})
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			if got != tt.want {
				t.Errorf("input %q = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Lowercase folding must not collapse R into r: that would silently turn
// retry-with-note into a plain retry and drop the user's steer.
func TestCLIPrompter_UppercaseRIsDistinct(t *testing.T) {
	p, _ := promptWith("R\n")
	got, err := p.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got != ActionRetryNote {
		t.Fatalf("R = %v, want retry-with-note", got)
	}

	p2, _ := promptWith("r\n")
	got2, err := p2.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got2 != ActionRetry {
		t.Fatalf("r = %v, want plain retry", got2)
	}
}

// An unrecognised key must not discard the draft. The previous gate defaulted to
// skip, which threw away work for a typo.
func TestCLIPrompter_UnknownKeyDoesNotSkip(t *testing.T) {
	p, out := promptWith("zzz\n")
	got, err := p.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got == ActionSkip {
		t.Error("an unrecognised key must not throw the draft away")
	}
	if !strings.Contains(out.String(), "unrecognised") {
		t.Errorf("the user should be told the key was not understood, got:\n%s", out)
	}
}

// A closed stdin means nobody can answer, so the gate must fail loudly rather
// than picking an action on the user's behalf.
func TestCLIPrompter_ClosedStdinIsNotInteractive(t *testing.T) {
	p, _ := promptWith("")
	_, err := p.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{})
	if err == nil {
		t.Fatal("expected ErrNotInteractive at a closed stdin")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("err = %v, want an interactivity error", err)
	}
}

// Escalation must not be advertised when the agent cannot do it.
func TestCLIPrompter_FooterHidesUnavailableActions(t *testing.T) {
	p, out := promptWith("s\n")
	if _, err := p.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{CanEscalate: false}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if strings.Contains(out.String(), "interactive") {
		t.Errorf("footer should omit escalation without a session plane:\n%s", out)
	}

	p2, out2 := promptWith("s\n")
	if _, err := p2.Show(Attempt{Content: "body"}, 0, 1, GateCapabilities{CanEscalate: true}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !strings.Contains(out2.String(), "[i]nteractive") {
		t.Errorf("footer should offer escalation when supported:\n%s", out2)
	}
}

// Local models take tens of seconds, so cost must be visible: a slow draft
// should read as slow, not suspicious.
func TestCLIPrompter_StatusLineShowsProviderCost(t *testing.T) {
	p, out := promptWith("s\n")
	attempt := Attempt{
		Content: "body",
		Result: &Result{
			Model:    "llama3.1",
			Duration: 4200 * time.Millisecond,
			Tokens:   adapter.TokenUsage{Total: 1100},
		},
	}
	if _, err := p.Show(attempt, 0, 1, GateCapabilities{}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	s := out.String()
	for _, want := range []string{"llama3.1", "4.2s", "1100 tokens"} {
		if !strings.Contains(s, want) {
			t.Errorf("status line missing %q:\n%s", want, s)
		}
	}
}

func TestCLIPrompter_ShowsAttemptPosition(t *testing.T) {
	p, out := promptWith("s\n")
	if _, err := p.Show(Attempt{Content: "body"}, 1, 3, GateCapabilities{CanNavigate: true}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !strings.Contains(out.String(), "attempt 2/3") {
		t.Errorf("expected an attempt counter:\n%s", out)
	}
}

func TestCLIPrompter_ShowsSteerNote(t *testing.T) {
	p, out := promptWith("s\n")
	attempt := Attempt{Content: "body", Note: "lead with revenue"}
	if _, err := p.Show(attempt, 0, 1, GateCapabilities{}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !strings.Contains(out.String(), "lead with revenue") {
		t.Errorf("the steer that produced an attempt should be visible:\n%s", out)
	}
}

func TestCLIPrompter_NoteReadsOneLine(t *testing.T) {
	p, _ := promptWith("  be terser  \n")
	note, err := p.Note()
	if err != nil {
		t.Fatalf("Note: %v", err)
	}
	if note != "be terser" {
		t.Errorf("note = %q, want it trimmed", note)
	}
}
