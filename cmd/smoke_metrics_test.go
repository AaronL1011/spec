package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitTransition rewrites a spec's frontmatter status in the sandboxed clone
// and commits it with the given message at the given ISO timestamp, pushing
// to the local bare origin so the history is authoritative.
func (e *smokeEnv) gitTransition(specID, fromStatus, toStatus, message, isoDate string) {
	e.t.Helper()
	path := filepath.Join(e.specsDirPath(), specID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("read spec: %v", err)
	}
	updated := strings.Replace(string(data), "status: "+fromStatus, "status: "+toStatus, 1)
	if updated == string(data) {
		e.t.Fatalf("status %q not found in %s", fromStatus, specID)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		e.t.Fatalf("write spec: %v", err)
	}
	e.gitCommitAll(message, isoDate)
}

// gitCommitAll commits and pushes everything in the clone at a fixed date.
func (e *smokeEnv) gitCommitAll(message, isoDate string) {
	e.t.Helper()
	root := e.cloneRoot()
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
			"GIT_AUTHOR_DATE="+isoDate, "GIT_COMMITTER_DATE="+isoDate)
		if out, err := cmd.CombinedOutput(); err != nil {
			e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", message)
	run("push", "-q", "origin", e.branch)
}

// seedFlowHistory drives SPEC-001 draft→…→done through real commits so the
// git history contains a complete journey.
func seedFlowHistory(e *smokeEnv) {
	e.gitTransition("SPEC-001", "draft", "engineering", "feat: advance SPEC-001 to engineering", "2026-01-05T10:00:00+00:00")
	e.gitTransition("SPEC-001", "engineering", "build", "feat: advance SPEC-001 to build", "2026-01-08T10:00:00+00:00")
	e.gitTransition("SPEC-001", "build", "done", "feat: advance SPEC-001 to done", "2026-01-12T10:00:00+00:00")
}

func TestSmoke_Metrics_FlowReportFromHistory(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Flow", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()
	seedFlowHistory(e)

	out, err := e.runSpec("metrics", "--since", "3650d")
	if err != nil {
		t.Fatalf("metrics: unexpected error: %v", err)
	}
	for _, want := range []string{"Flow —", "1 specs completed", "Lead time", "Time in stage", "transitions"} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q:\n%s", want, out)
		}
	}
}

func TestSmoke_Metrics_JSONShape(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Flow", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()
	seedFlowHistory(e)

	out, err := e.runSpec("metrics", "--since", "3650d", "--json")
	if err != nil {
		t.Fatalf("metrics --json: unexpected error: %v", err)
	}

	var report struct {
		Completed int `json:"completed"`
		LeadTime  struct {
			Samples int `json:"samples"`
			P50     struct {
				Seconds int64 `json:"seconds"`
			} `json:"p50"`
		} `json:"lead_time"`
		Coverage struct {
			Transitions int `json:"transitions"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("metrics --json output is not valid JSON: %v\n%s", err, out)
	}
	if report.Completed != 1 || report.LeadTime.Samples != 1 {
		t.Errorf("completed=%d lead samples=%d, want 1/1", report.Completed, report.LeadTime.Samples)
	}
	// Journey spans 2026-01-05 → 2026-01-12 = 7 days of lead time.
	if want := int64(7 * 24 * 3600); report.LeadTime.P50.Seconds != want {
		t.Errorf("lead p50 = %ds, want %d", report.LeadTime.P50.Seconds, want)
	}
	// 4 = scaffold (file add in the seed commit) + three advances.
	if report.Coverage.Transitions != 4 {
		t.Errorf("coverage transitions = %d, want 4", report.Coverage.Transitions)
	}
}

func TestSmoke_Metrics_SpecJourney(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Flow", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()
	seedFlowHistory(e)

	out, err := e.runSpec("metrics", "--spec", "SPEC-001")
	if err != nil {
		t.Fatalf("metrics --spec: unexpected error: %v", err)
	}
	for _, want := range []string{"SPEC-001 — journey", "engineering", "Lead time: 7d"} {
		if !strings.Contains(out, want) {
			t.Errorf("journey output missing %q:\n%s", want, out)
		}
	}
}

func TestSmoke_Metrics_SpecUnknown(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Flow", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("metrics", "--spec", "SPEC-999")
	if err == nil || !strings.Contains(err.Error(), "no history for SPEC-999") {
		t.Fatalf("metrics --spec unknown: err = %v, want no-history error", err)
	}
}

func TestSmoke_Metrics_StageDeepDive(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Flow", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()
	seedFlowHistory(e)

	out, err := e.runSpec("metrics", "--since", "3650d", "--stage", "engineering")
	if err != nil {
		t.Fatalf("metrics --stage: unexpected error: %v", err)
	}
	if !strings.Contains(out, "Stage engineering") {
		t.Errorf("stage output missing header:\n%s", out)
	}

	_, err = e.runSpec("metrics", "--stage", "not-a-stage")
	if err == nil || !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("metrics --stage invalid: err = %v, want unknown-stage error", err)
	}
}

func TestSmoke_Metrics_NoTeamConfigDegrades(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	// No team config, no specs repo.

	out, err := e.runSpec("metrics")
	if err != nil {
		t.Fatalf("metrics without config: unexpected error: %v", err)
	}
	if !strings.Contains(out, "metrics requires a specs repo") {
		t.Errorf("metrics output = %q, want setup hint", out)
	}
}
