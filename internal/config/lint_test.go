package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLintConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// findDiag returns the first diagnostic whose field contains sub, or nil.
func findDiag(diags []Diagnostic, sub string) *Diagnostic {
	for i := range diags {
		if stringContains(diags[i].Field, sub) {
			return &diags[i]
		}
	}
	return nil
}

func stringContains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOfStr(s, sub) >= 0
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestLint_UnknownGateTypeAndMissingField is AC-9: an unknown gate type and a
// missing required field are both reported with file, line, and a suggestion;
// the result has errors (non-zero exit).
func TestLint_UnknownGateTypeAndMissingField(t *testing.T) {
	// No top-level "version" (missing required field), and a misspelled gate.
	body := `team:
  name: Platform
pipeline:
  preset: product
  stages:
    - name: tl-review
      owner: tl
      gates:
        - sectoin_complete: problem_statement
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected errors for unknown gate + missing version")
	}

	gate := findDiag(res.Diagnostics, ".gate")
	if gate == nil {
		t.Fatalf("no gate diagnostic; got %+v", res.Diagnostics)
	}
	if gate.Line != 9 {
		t.Errorf("gate diagnostic line = %d, want 9", gate.Line)
	}
	if gate.Suggestion == "" || !stringContains(gate.Suggestion, "section_complete") {
		t.Errorf("gate suggestion = %q, want a did-you-mean section_complete", gate.Suggestion)
	}

	if findDiag(res.Diagnostics, "version") == nil {
		t.Errorf("expected a missing-version diagnostic; got %+v", res.Diagnostics)
	}
}

// TestLint_ValidConfigIsClean is AC-10: a valid preset-derived config produces
// no diagnostics.
func TestLint_ValidConfigIsClean(t *testing.T) {
	body := `version: "1"
team:
  name: Platform
pipeline:
  preset: product
  stages:
    - name: tl-review
      owner: tl
      gates:
        - section_not_empty: problem_statement
        - all:
            - prs_approved: true
            - review_approved: true
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if res.HasErrors() {
		t.Errorf("expected a clean config, got diagnostics: %+v", res.Diagnostics)
	}
}

func TestLint_UnknownPreset(t *testing.T) {
	body := `version: "1"
pipeline:
  preset: prodcut
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	d := findDiag(res.Diagnostics, "pipeline.preset")
	if d == nil {
		t.Fatalf("no preset diagnostic; got %+v", res.Diagnostics)
	}
	if !stringContains(d.Suggestion, "product") {
		t.Errorf("preset suggestion = %q, want did-you-mean product", d.Suggestion)
	}
}

func TestLint_UnknownDoScope(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: draft
      dashboard:
        do_scope: assignne
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	d := findDiag(res.Diagnostics, "do_scope")
	if d == nil {
		t.Fatalf("no do_scope diagnostic; got %+v", res.Diagnostics)
	}
	if !stringContains(d.Suggestion, "assignee") {
		t.Errorf("do_scope suggestion = %q, want did-you-mean assignee", d.Suggestion)
	}
}

func TestLint_UnknownSyncDirection(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: build
      on_enter:
        - sync: outbond
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	d := findDiag(res.Diagnostics, ".sync")
	if d == nil {
		t.Fatalf("no sync diagnostic; got %+v", res.Diagnostics)
	}
	if !stringContains(d.Suggestion, "outbound") {
		t.Errorf("sync suggestion = %q, want did-you-mean outbound", d.Suggestion)
	}
}

func TestLint_WebhookMissingURL(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: build
      on_exit:
        - webhook:
            method: POST
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	if findDiag(res.Diagnostics, "webhook.url") == nil {
		t.Errorf("expected a webhook.url diagnostic; got %+v", res.Diagnostics)
	}
}

func TestLint_StageMissingName(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - owner: tl
      gates:
        - section_not_empty: tldr
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	if findDiag(res.Diagnostics, "name") == nil {
		t.Errorf("expected a missing-name diagnostic; got %+v", res.Diagnostics)
	}
}

func TestLint_MalformedYAML(t *testing.T) {
	body := "version: \"1\"\npipeline:\n  stages:\n  - name: draft\n   bad-indent: x\n"
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint should return diagnostics, not a hard error: %v", err)
	}
	if !res.HasErrors() {
		t.Errorf("expected a YAML parse diagnostic; got %+v", res.Diagnostics)
	}
}

func TestLint_MissingFileErrors(t *testing.T) {
	_, err := LintTeamConfigFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestLint_NestedNotGate(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: build
      gates:
        - not:
            sectoin_complete: x
`
	path := writeLintConfig(t, body)
	res, _ := LintTeamConfigFile(path)
	if findDiag(res.Diagnostics, ".gate") == nil {
		t.Errorf("expected the typo inside not: to be caught; got %+v", res.Diagnostics)
	}
}

// TestKnownPresets_NoDrift guards the linter's preset list against the
// authoritative registry. It is a string-level check so config need not import
// pipeline (which would cycle).
func TestKnownPresets_NoDrift(t *testing.T) {
	want := map[string]bool{"minimal": true, "startup": true, "product": true, "platform": true, "kanban": true}
	got := KnownPresets()
	if len(got) != len(want) {
		t.Fatalf("KnownPresets = %v, want %d entries", got, len(want))
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected preset %q in KnownPresets", p)
		}
	}
}
