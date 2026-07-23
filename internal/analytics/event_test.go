package analytics

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/git"
)

var testStages = []string{"triage", "draft", "tl-review", "engineering", "build", "done"}

func t0(minutes int) time.Time {
	return time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

// patchFor builds a minimal unified diff for a spec file status change.
func patchFor(path, from, to string, added bool) string {
	if added {
		return "diff --git a/" + path + " b/" + path + "\n" +
			"new file mode 100644\n" +
			"--- /dev/null\n" +
			"+++ b/" + path + "\n" +
			"@@ -0,0 +1,4 @@\n" +
			"+---\n" +
			"+status: " + to + "\n" +
			"+---\n" +
			"+body\n"
	}
	return "diff --git a/" + path + " b/" + path + "\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n" +
		"@@ -1,4 +1,4 @@\n" +
		" ---\n" +
		"-status: " + from + "\n" +
		"+status: " + to + "\n" +
		" ---\n"
}

func TestExtractEvents_LifecycleKinds(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		patch    string
		wantKind EventKind
		wantFrom string
		wantTo   string
		wantSrc  EventSource
	}{
		{
			name:     "scaffold new file",
			message:  "feat: scaffold SPEC-001 — Title",
			patch:    patchFor("specs/SPEC-001.md", "", "draft", true),
			wantKind: KindScaffolded, wantTo: "draft", wantSrc: SourceMessage,
		},
		{
			name:     "promote is a scaffold",
			message:  "feat: promote TRIAGE-004 to SPEC-002 — Title",
			patch:    patchFor("specs/SPEC-002.md", "", "triage", true),
			wantKind: KindScaffolded, wantTo: "triage", wantSrc: SourceMessage,
		},
		{
			name:     "advance",
			message:  "feat: advance SPEC-001 to build",
			patch:    patchFor("specs/SPEC-001.md", "engineering", "build", false),
			wantKind: KindAdvanced, wantFrom: "engineering", wantTo: "build", wantSrc: SourceMessage,
		},
		{
			name:     "revert",
			message:  "fix: revert SPEC-001 to draft — under-baked",
			patch:    patchFor("specs/SPEC-001.md", "tl-review", "draft", false),
			wantKind: KindReverted, wantFrom: "tl-review", wantTo: "draft", wantSrc: SourceMessage,
		},
		{
			name:     "eject",
			message:  "fix: eject SPEC-001 — waiting on legal",
			patch:    patchFor("specs/SPEC-001.md", "build", "blocked", false),
			wantKind: KindEjected, wantFrom: "build", wantTo: "blocked", wantSrc: SourceMessage,
		},
		{
			name:     "resume",
			message:  "fix: resume SPEC-001 to build",
			patch:    patchFor("specs/SPEC-001.md", "blocked", "build", false),
			wantKind: KindResumed, wantFrom: "blocked", wantTo: "build", wantSrc: SourceMessage,
		},
		{
			name:     "manual edit advancing status is attributed via frontmatter",
			message:  "update spec by hand",
			patch:    patchFor("specs/SPEC-001.md", "draft", "tl-review", false),
			wantKind: KindAdvanced, wantFrom: "draft", wantTo: "tl-review", wantSrc: SourceFrontmatter,
		},
		{
			name:     "manual edit moving status backwards is a reversion",
			message:  "chore: housekeeping",
			patch:    patchFor("specs/SPEC-001.md", "build", "draft", false),
			wantKind: KindReverted, wantFrom: "build", wantTo: "draft", wantSrc: SourceFrontmatter,
		},
		{
			name:     "unknown stages fall back to message kind",
			message:  "fix: revert SPEC-001 to legacy-stage — cleanup",
			patch:    patchFor("specs/SPEC-001.md", "old-stage", "legacy-stage", false),
			wantKind: KindReverted, wantFrom: "old-stage", wantTo: "legacy-stage", wantSrc: SourceMessage,
		},
		{
			name:     "archived spec path still attributes to the spec",
			message:  "chore: archive sweep",
			patch:    patchFor("specs/archive/SPEC-001.md", "done", "closed", false),
			wantKind: KindAdvanced, wantFrom: "done", wantTo: "closed", wantSrc: SourceFrontmatter,
		},
	}

	stages := append([]string{}, testStages...)
	stages = append(stages, "closed")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ExtractEvents([]git.LogEntry{{When: t0(0), Message: tt.message, Patch: tt.patch}}, stages)
			if len(res.Events) != 1 {
				t.Fatalf("got %d events, want 1", len(res.Events))
			}
			ev := res.Events[0]
			if ev.Kind != tt.wantKind || ev.FromStage != tt.wantFrom || ev.ToStage != tt.wantTo || ev.Source != tt.wantSrc {
				t.Errorf("event = %+v, want kind=%s from=%q to=%q src=%s", ev, tt.wantKind, tt.wantFrom, tt.wantTo, tt.wantSrc)
			}
			if res.Transitions != 1 || res.Unattributable != 0 {
				t.Errorf("counters = %+v", res)
			}
		})
	}
}

func TestExtractEvents_IgnoresNonEvents(t *testing.T) {
	tests := []struct {
		name  string
		patch string
	}{
		{"body-only edit", "diff --git a/specs/SPEC-001.md b/specs/SPEC-001.md\n--- a/specs/SPEC-001.md\n+++ b/specs/SPEC-001.md\n@@ -50,3 +50,4 @@\n context\n+new prose\n"},
		{"status line in body code block beyond frontmatter window", "diff --git a/specs/SPEC-001.md b/specs/SPEC-001.md\n--- a/specs/SPEC-001.md\n+++ b/specs/SPEC-001.md\n@@ -80,3 +80,4 @@\n ```yaml\n-status: draft\n+status: build\n ```\n"},
		{"threads sidecar", patchFor("specs/SPEC-001.threads.yaml", "draft", "build", false)},
		{"triage file", patchFor("specs/TRIAGE-004.md", "open", "closed", false)},
		{"unchanged status rewrite", patchFor("specs/SPEC-001.md", "build", "build", false)},
		{"pure rename", "diff --git a/specs/SPEC-001.md b/specs/archive/SPEC-001.md\nsimilarity index 100%\nrename from specs/SPEC-001.md\nrename to specs/archive/SPEC-001.md\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ExtractEvents([]git.LogEntry{{When: t0(0), Message: "chore: x", Patch: tt.patch}}, testStages)
			if len(res.Events) != 0 {
				t.Errorf("got %d events, want 0: %+v", len(res.Events), res.Events)
			}
			if res.Unattributable != 0 {
				t.Errorf("Unattributable = %d, want 0", res.Unattributable)
			}
		})
	}
}

func TestExtractEvents_UnattributableStatusRestructure(t *testing.T) {
	// A +status: with no -status: on a pre-existing file cannot be attributed.
	patch := "diff --git a/specs/SPEC-001.md b/specs/SPEC-001.md\n" +
		"--- a/specs/SPEC-001.md\n+++ b/specs/SPEC-001.md\n" +
		"@@ -1,3 +1,4 @@\n ---\n+status: build\n ---\n"
	res := ExtractEvents([]git.LogEntry{{When: t0(0), Message: "chore: restructure", Patch: patch}}, testStages)
	if len(res.Events) != 0 || res.Unattributable != 1 {
		t.Errorf("res = %+v, want 0 events and 1 unattributable", res)
	}
}

func TestExtractEvents_MultipleSpecsInOneCommit(t *testing.T) {
	patch := patchFor("specs/SPEC-001.md", "draft", "tl-review", false) +
		patchFor("specs/SPEC-002.md", "engineering", "build", false)
	res := ExtractEvents([]git.LogEntry{{When: t0(0), Message: "feat: update specs (SPEC-001, SPEC-002)", Patch: patch}}, testStages)
	if len(res.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(res.Events))
	}
	if res.Events[0].SpecID != "SPEC-001" || res.Events[1].SpecID != "SPEC-002" {
		t.Errorf("spec IDs = %s, %s", res.Events[0].SpecID, res.Events[1].SpecID)
	}
	for _, ev := range res.Events {
		if ev.Kind != KindAdvanced || ev.Source != SourceFrontmatter {
			t.Errorf("event = %+v, want advanced/frontmatter", ev)
		}
	}
}
