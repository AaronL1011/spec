package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSmoke_Version asserts the version command writes through the cobra
// stream and never errors.
func TestSmoke_Version(t *testing.T) {
	e := newSmokeEnv(t)
	out, err := e.runSpec("version")
	if err != nil {
		t.Fatalf("version: unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "spec ") {
		t.Errorf("version output = %q, want prefix %q", out, "spec ")
	}
}

// TestSmoke_RootNoUserConfig asserts the cold-start branch: no identity yet,
// so the root prints onboarding hints and exits 0 (no error).
func TestSmoke_RootNoUserConfig(t *testing.T) {
	e := newSmokeEnv(t)
	out, err := e.runSpec()
	if err != nil {
		t.Fatalf("root: unexpected error: %v", err)
	}
	if !strings.Contains(out, "config init --user") {
		t.Errorf("root output = %q, want it to mention 'config init --user'", out)
	}
}

// TestSmoke_RootRoleNoTeam asserts the role-but-no-team branch prints the
// team-setup hint and exits 0.
func TestSmoke_RootRoleNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	out, err := e.runSpec()
	if err != nil {
		t.Fatalf("root: unexpected error: %v", err)
	}
	if !strings.Contains(out, "config init") {
		t.Errorf("root output = %q, want it to mention 'config init'", out)
	}
	if !strings.Contains(strings.ToLower(out), "role: engineer") {
		t.Errorf("root output = %q, want it to show the role", out)
	}
}

// TestSmoke_DashboardJSON asserts the --json dashboard shape against a sandbox
// with one DO-stage spec owned by the viewer's role.
func TestSmoke_DashboardJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "First", status: "build", author: "Dev"}, "## TL;DR\nbody\n")

	out, err := e.runSpec("--static", "--json")
	if err != nil {
		t.Fatalf("dashboard --json: unexpected error: %v", err)
	}

	var data struct {
		Do       []map[string]any `json:"do"`
		Review   []map[string]any `json:"review"`
		Incoming []map[string]any `json:"incoming"`
		Blocked  []map[string]any `json:"blocked"`
	}
	if err := json.Unmarshal([]byte(jsonChunk(out)), &data); err != nil {
		t.Fatalf("dashboard JSON did not unmarshal: %v\nraw: %s", err, out)
	}
	if len(data.Do) == 0 {
		t.Errorf("expected at least one DO item, got none: %s", out)
	}
}

// TestSmoke_ListNoTeam asserts the next-action error contract when no team
// config is present.
func TestSmoke_ListNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	_, err := e.runSpec("list")
	if err == nil {
		t.Fatal("list without team config: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("list error = %q, want it to point to 'spec config init'", err)
	}
}

// TestSmoke_ListJSON asserts the --json list shape (array of spec summaries)
// against a sandboxed git-backed specs repo.
func TestSmoke_ListJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "First", status: "build", author: "Dev"}, "## TL;DR\nbody\n")
	e.writeSpec(specFixture{id: "SPEC-002", title: "Second", status: "draft", author: "Other"}, "## TL;DR\nbody\n")
	e.initSpecsGit()

	out, err := e.runSpec("list", "--all", "--json")
	if err != nil {
		t.Fatalf("list --all --json: unexpected error: %v", err)
	}
	var specs []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(jsonChunk(out)), &specs); err != nil {
		t.Fatalf("list JSON did not unmarshal: %v\nraw: %s", err, out)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs in --all JSON, got %d: %s", len(specs), out)
	}
	for _, s := range specs {
		if s.ID == "" || s.Title == "" || s.Status == "" {
			t.Errorf("spec summary missing fields: %+v", s)
		}
	}
}

// TestSmoke_ListMineJSON asserts the --mine filter wiring narrows to the
// viewer's owned specs.
func TestSmoke_ListMineJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Mine", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-002", title: "Theirs", status: "draft", author: "Other"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("list", "--mine", "--json")
	if err != nil {
		t.Fatalf("list --mine --json: unexpected error: %v", err)
	}
	var specs []struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal([]byte(jsonChunk(out)), &specs); err != nil {
		t.Fatalf("list --mine JSON did not unmarshal: %v\nraw: %s", err, out)
	}
	if len(specs) != 1 || specs[0].ID != "SPEC-001" {
		t.Fatalf("expected only SPEC-001 owned by Dev, got %+v", specs)
	}
}

// TestSmoke_StatusJSON asserts the status JSON contract for a known spec.
func TestSmoke_StatusJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-007", title: "Statusable", status: "build", author: "Dev"},
		"## TL;DR\nsummary\n\n## 1. Problem Statement\ndetails\n")
	e.initSpecsGit()

	out, err := e.runSpec("status", "SPEC-007", "--json")
	if err != nil {
		t.Fatalf("status --json: unexpected error: %v", err)
	}
	var rep statusReport
	if err := json.Unmarshal([]byte(jsonChunk(out)), &rep); err != nil {
		t.Fatalf("status JSON did not unmarshal: %v\nraw: %s", err, out)
	}
	if rep.ID != "SPEC-007" || rep.Status != "build" {
		t.Errorf("status report = %+v, want id SPEC-007 status build", rep)
	}
	if len(rep.Sections) == 0 {
		t.Errorf("status report has no sections: %+v", rep)
	}
}

// TestSmoke_StatusNotFound asserts the not-found next-action error contract.
func TestSmoke_StatusNotFound(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Only", status: "build", author: "Dev"}, "## TL;DR\nx\n")

	_, err := e.runSpec("status", "SPEC-404")
	if err == nil {
		t.Fatal("status for missing spec: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("status error = %q, want it to mention 'not found'", err)
	}
}

// TestSmoke_StatusNoArgNoFocus asserts the missing-id next-action contract.
func TestSmoke_StatusNoArgNoFocus(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	_, err := e.runSpec("status")
	if err == nil {
		t.Fatal("status with no id and no focus: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "spec focus") {
		t.Errorf("status error = %q, want it to suggest 'spec focus'", err)
	}
}

// TestSmoke_FocusNoArg asserts the focus missing-id next-action contract.
func TestSmoke_FocusNoArg(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	_, err := e.runSpec("focus")
	if err == nil {
		t.Fatal("focus with no id: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "spec focus") {
		t.Errorf("focus error = %q, want it to suggest 'spec focus'", err)
	}
}

// TestSmoke_FocusClear asserts the focus --clear happy path against the
// sandboxed DB.
func TestSmoke_FocusClear(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	out, err := e.runSpec("focus", "--clear")
	if err != nil {
		t.Fatalf("focus --clear: unexpected error: %v", err)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("focus --clear output = %q, want 'cleared'", out)
	}
}

// TestSmoke_Whoami asserts whoami reports resolved identity.
func TestSmoke_Whoami(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("whoami")
	if err != nil {
		t.Fatalf("whoami: unexpected error: %v", err)
	}
	if !strings.Contains(out, "engineer") || !strings.Contains(out, "Dev") {
		t.Errorf("whoami output = %q, want role and name", out)
	}
}

// TestSmoke_Validate asserts the gate dry-run prints without advancing.
func TestSmoke_Validate(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "V", status: "draft", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nstuff\n")
	e.initSpecsGit()

	out, err := e.runSpec("validate", "SPEC-001")
	if err != nil {
		t.Fatalf("validate: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("validate produced no output")
	}
}

// TestSmoke_PipelineShow asserts the default pipeline renders without config.
func TestSmoke_PipelineShow(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("pipeline")
	if err != nil {
		t.Fatalf("pipeline: unexpected error: %v", err)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("pipeline output = %q, want it to list stages", out)
	}
}

// TestSmoke_History asserts history degrades gracefully with no archive.
func TestSmoke_History(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("history")
	if err != nil {
		t.Fatalf("history: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("history produced no output")
	}
}

// jsonChunk extracts the JSON document from mixed output by trimming to the
// first '{' or '[' — warnings or awareness lines may precede it.
func jsonChunk(s string) string {
	i := strings.IndexAny(s, "{[")
	if i < 0 {
		return s
	}
	return s[i:]
}
