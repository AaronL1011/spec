package dashboard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

func TestPendingCount_NilConfig(t *testing.T) {
	count := PendingCount(nil, "engineer")
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestPendingCount_EmptyDir(t *testing.T) {
	rc := &config.ResolvedConfig{SpecsRepoDir: t.TempDir()}
	count := PendingCount(rc, "engineer")
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestPendingCount_MatchesRole(t *testing.T) {
	dir := t.TempDir()

	// engineer-owned stage
	_ = os.WriteFile(filepath.Join(dir, "SPEC-001.md"), []byte(
		"---\nid: SPEC-001\ntitle: Test\nstatus: build\n---\n",
	), 0o644)

	// pm-owned stage
	_ = os.WriteFile(filepath.Join(dir, "SPEC-002.md"), []byte(
		"---\nid: SPEC-002\ntitle: Other\nstatus: draft\n---\n",
	), 0o644)

	rc := &config.ResolvedConfig{
		SpecsRepoDir: dir,
		Team:         defaultTeamConfig(),
	}

	if count := PendingCount(rc, "engineer"); count != 1 {
		t.Errorf("engineer: expected 1, got %d", count)
	}
	if count := PendingCount(rc, "pm"); count != 1 {
		t.Errorf("pm: expected 1, got %d", count)
	}
}

func TestPendingCount_EmptyRole(t *testing.T) {
	rc := &config.ResolvedConfig{SpecsRepoDir: t.TempDir()}
	count := PendingCount(rc, "")
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{30 * time.Minute, "30m ago"},
		{3 * time.Hour, "3h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		got := timeAgo(time.Now().Add(-tt.d))
		if got != tt.want {
			t.Errorf("timeAgo(-%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncStr(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is very long text", 10, "this is..."},
		{"exact", 5, "exact"},
	}
	for _, tt := range tests {
		got := truncStr(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncStr(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func writeSpec(t *testing.T, dir, id, status string, assignees []string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\ntitle: %s\nstatus: %s\nauthor: Ana\n", id, id, status)
	if len(assignees) > 0 {
		b.WriteString("assignees:\n")
		for _, a := range assignees {
			fmt.Fprintf(&b, "  - %q\n", a)
		}
	}
	if status == "blocked" {
		b.WriteString("blocked_from: build\n")
	}
	b.WriteString("---\n")
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scopedTeamConfig() *config.TeamConfig {
	tc := &config.TeamConfig{
		Pipeline: config.PipelineConfig{
			Stages: []config.StageConfig{
				{Name: "engineering", Owner: config.Owners{"engineer"},
					Dashboard: config.StageDashboardConfig{DoScope: "assignee"}},
				{Name: "build", Owner: config.Owners{"engineer"}},
			},
		},
	}
	tc.Dashboard.Blocked = config.BlockedConfig{Scope: "owning_role"}
	return tc
}

func TestAggregate_AssigneeScopeAndBlocked(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "SPEC-100", "engineering", nil)              // unclaimed
	writeSpec(t, dir, "SPEC-101", "engineering", []string{"@ana"}) // claimed by Ana
	writeSpec(t, dir, "SPEC-102", "blocked", nil)                  // blocked from build (engineer-owned)

	ana := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Ana", "@ana", "engineer")}
	ben := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Ben", "@ben", "engineer")}
	pam := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Pam", "@pam", "pm")}

	anaData, _ := Aggregate(context.Background(), ana, nil, "engineer")
	benData, _ := Aggregate(context.Background(), ben, nil, "engineer")
	pamData, _ := Aggregate(context.Background(), pam, nil, "pm")

	// Ana sees the unclaimed queue spec + her own claimed spec.
	if ids := doIDs(anaData); !equalSet(ids, []string{"SPEC-100", "SPEC-101"}) {
		t.Errorf("Ana DO = %v, want [SPEC-100 SPEC-101]", ids)
	}
	// Ben sees the unclaimed queue spec but NOT Ana's claimed spec.
	if ids := doIDs(benData); !equalSet(ids, []string{"SPEC-100"}) {
		t.Errorf("Ben DO = %v, want [SPEC-100]", ids)
	}
	// PM is not an engineer-stage owner, so sees nothing in DO.
	if ids := doIDs(pamData); len(ids) != 0 {
		t.Errorf("Pam DO = %v, want []", ids)
	}
	// Blocked-from-build is engineer-owned: engineers see it, PM does not.
	if len(anaData.Blocked) != 1 {
		t.Errorf("Ana Blocked = %d, want 1", len(anaData.Blocked))
	}
	if len(pamData.Blocked) != 0 {
		t.Errorf("Pam Blocked = %d, want 0", len(pamData.Blocked))
	}
}

func userCfg(name, handle, role string) *config.UserConfig {
	uc := &config.UserConfig{}
	uc.User.Name = name
	uc.User.Handle = handle
	uc.User.OwnerRole = role
	return uc
}

func doIDs(d *DashboardData) []string {
	var ids []string
	for _, item := range d.Do {
		ids = append(ids, item.SpecID)
	}
	return ids
}

func equalSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// defaultTeamConfig returns a minimal team config with the default pipeline.
func defaultTeamConfig() *config.TeamConfig {
	return &config.TeamConfig{
		Pipeline: config.PipelineConfig{
			Stages: []config.StageConfig{
				{Name: "triage", OwnerRole: "pm"},
				{Name: "draft", OwnerRole: "pm"},
				{Name: "tl-review", OwnerRole: "tl"},
				{Name: "design", OwnerRole: "designer"},
				{Name: "qa-expectations", OwnerRole: "qa"},
				{Name: "engineering", OwnerRole: "engineer"},
				{Name: "build", OwnerRole: "engineer"},
				{Name: "pr-review", OwnerRole: "engineer"},
				{Name: "qa-validation", OwnerRole: "qa"},
				{Name: "done", OwnerRole: "tl"},
			},
		},
	}
}
