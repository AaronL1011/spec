package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// writeRawTeamConfig overwrites spec.config.yaml in the working directory with
// arbitrary content, for exercising the linter against crafted inputs.
func (e *smokeEnv) writeRawTeamConfig(content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.workDir, "spec.config.yaml"), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write raw team config: %v", err)
	}
}

// TestSmoke_ConfigLintValid is AC-10: a valid config prints the OK line and
// exits zero.
func TestSmoke_ConfigLintValid(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig() // default config has no pipeline overrides — valid

	out, err := e.runSpec("config", "lint")
	if err != nil {
		t.Fatalf("config lint (valid): unexpected error: %v", err)
	}
	if !strings.Contains(out, "config valid") {
		t.Errorf("config lint output = %q, want a valid marker", out)
	}
}

// TestSmoke_ConfigLintInvalid is AC-9: an unknown gate type and a missing
// required field are reported with line numbers; exit is non-zero.
func TestSmoke_ConfigLintInvalid(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	// No version (missing required field) and a misspelled gate type.
	e.writeRawTeamConfig(`team:
  name: Test
specs_repo:
  provider: github
  owner: testorg
  repo: specs-repo
  branch: main
  token: test-token-not-a-secret
pipeline:
  stages:
    - name: tl-review
      owner: tl
      gates:
        - sectoin_complete: problem_statement
`)

	out, err := e.runSpec("config", "lint")
	if err == nil {
		t.Fatal("config lint (invalid): expected a non-zero exit")
	}
	if !strings.Contains(out, "unknown gate type") {
		t.Errorf("output = %q, want an unknown-gate-type diagnostic", out)
	}
	if !strings.Contains(out, "section_complete") {
		t.Errorf("output = %q, want a did-you-mean suggestion", out)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("output = %q, want a missing-version diagnostic", out)
	}
}

// TestSmoke_ConfigLintJSON asserts --json emits a structured diagnostics array
// (AC-9) and still exits non-zero on errors.
func TestSmoke_ConfigLintJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeRawTeamConfig(`team:
  name: Test
pipeline:
  preset: prodcut
`)

	out, err := e.runSpec("config", "lint", "--json")
	if err == nil {
		t.Fatal("config lint --json (invalid): expected a non-zero exit")
	}

	// The JSON object is the first thing on stdout; isolate it.
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON object in output: %q", out)
	}
	var result config.LintResult
	dec := json.NewDecoder(strings.NewReader(out[start:]))
	if err := dec.Decode(&result); err != nil {
		t.Fatalf("decode lint JSON: %v\n%s", err, out)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("expected diagnostics in JSON output")
	}
	if !result.HasErrors() {
		t.Errorf("expected at least one error-severity diagnostic, got %+v", result.Diagnostics)
	}
}

// TestSmoke_ConfigLintNoTeam asserts the next-action error contract when no
// team config exists.
func TestSmoke_ConfigLintNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	_, err := e.runSpec("config", "lint")
	if err == nil {
		t.Fatal("config lint with no team: expected an error")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("error = %q, want a 'spec config init' next action", err)
	}
}
