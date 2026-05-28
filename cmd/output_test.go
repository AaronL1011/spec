package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func newTestPrinter(jsonOut, quiet bool) (*printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &printer{out: &out, errOut: &errOut, json: jsonOut, quiet: quiet}, &out, &errOut
}

func TestPrinter_LineHumanMode(t *testing.T) {
	p, out, _ := newTestPrinter(false, false)
	p.Line("hello %s", "world")
	if got := out.String(); got != "hello world\n" {
		t.Fatalf("out = %q, want %q", got, "hello world\n")
	}
}

func TestPrinter_LineSuppressedByQuietAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name        string
		json, quiet bool
	}{
		{"quiet", false, true},
		{"json", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, out, _ := newTestPrinter(tc.json, tc.quiet)
			p.Line("should not appear")
			if out.Len() != 0 {
				t.Fatalf("expected no stdout, got %q", out.String())
			}
		})
	}
}

func TestPrinter_JSONOnlyWhenEnabled(t *testing.T) {
	p, out, _ := newTestPrinter(true, false)
	if err := p.JSON(map[string]string{"k": "v"}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(out.String(), `"k": "v"`) {
		t.Fatalf("expected JSON in stdout, got %q", out.String())
	}

	p2, out2, _ := newTestPrinter(false, false)
	if err := p2.JSON(map[string]string{"k": "v"}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if out2.Len() != 0 {
		t.Fatalf("JSON must be silent when --json is off, got %q", out2.String())
	}
}

func TestPrinter_WarnGoesToStderr(t *testing.T) {
	p, out, errOut := newTestPrinter(true, false)
	p.Warn("careful: %d", 7)
	if out.Len() != 0 {
		t.Fatalf("warnings must not hit stdout, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "warning: careful: 7") {
		t.Fatalf("stderr = %q", errOut.String())
	}

	// --quiet suppresses warnings.
	pq, _, errq := newTestPrinter(false, true)
	pq.Warn("hidden")
	if errq.Len() != 0 {
		t.Fatalf("quiet must suppress warnings, got %q", errq.String())
	}
}
