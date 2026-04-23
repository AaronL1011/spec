package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aaronl1011/spec-cli/internal/config"
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
