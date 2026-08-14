package hierarchy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpec drops a minimal spec file with the given frontmatter fields.
func writeSpec(t *testing.T, dir, id, status, parent string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	fm := "---\nid: " + id + "\ntitle: " + id + " title\nstatus: " + status + "\n"
	if parent != "" {
		fm += "parent: " + parent + "\n"
	}
	fm += "---\n\n# " + id + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(fm), 0o644); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "SPEC-004", "build", "")
	writeSpec(t, root, "SPEC-009", "build", "SPEC-004")
	writeSpec(t, filepath.Join(root, "triage"), "SPEC-020", "draft", "")
	writeSpec(t, filepath.Join(root, "archive"), "SPEC-001", "closed", "")

	// Noise that must be skipped rather than fail the scan.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("not a spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SPEC-666.md"), []byte("no frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := Scan(root, "archive")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(refs) != 4 {
		t.Fatalf("Scan returned %d refs, want 4: %+v", len(refs), refs)
	}

	g := Build(refs)
	if !g.HasChildren("SPEC-004") {
		t.Error("SPEC-004 should have a slice")
	}
	archived, ok := g.Get("SPEC-001")
	if !ok || !archived.Archived {
		t.Errorf("archived spec = %+v, want Archived true", archived)
	}
	triaged, ok := g.Get("SPEC-020")
	if !ok || triaged.Archived {
		t.Errorf("triage spec = %+v, want indexed and not archived", triaged)
	}
}

func TestScan_ArchivedInitiativeIsNotDangling(t *testing.T) {
	// The reason Scan searches archive/: a closed initiative must still
	// resolve, or every slice beneath it reports a dangling parent.
	root := t.TempDir()
	writeSpec(t, filepath.Join(root, "archive"), "SPEC-004", "closed", "")
	writeSpec(t, root, "SPEC-009", "build", "SPEC-004")

	g, err := Load(root, "archive")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	findings := Check(g, "SPEC-009", testPipeline())
	if HasErrors(findings) {
		t.Errorf("archived parent must not be a dangling-parent error: %+v", findings)
	}
	if len(findings) != 1 || findings[0].Rule != RuleParentArchived {
		t.Errorf("findings = %+v, want a single %s warning", findings, RuleParentArchived)
	}
}

func TestScan_MissingSubdirsAreNotErrors(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "SPEC-001", "draft", "")
	refs, err := Scan(root, "archive")
	if err != nil {
		t.Fatalf("Scan with no triage/ or archive/: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("got %d refs, want 1", len(refs))
	}
}

func TestScan_UnconfiguredDirIsActionable(t *testing.T) {
	_, err := Scan("", "archive")
	if err == nil {
		t.Fatal("expected an error for an unconfigured specs dir")
	}
	if got := err.Error(); !strings.Contains(got, "spec.config.yaml") {
		t.Errorf("error %q should name the config to fix", got)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "SPEC-004", "build", "")
	writeSpec(t, filepath.Join(root, "triage"), "SPEC-020", "draft", "")
	writeSpec(t, filepath.Join(root, "archive"), "SPEC-001", "closed", "")

	tests := []struct {
		name         string
		id           string
		wantFound    bool
		wantArchived bool
	}{
		{name: "root", id: "SPEC-004", wantFound: true},
		{name: "triage", id: "SPEC-020", wantFound: true},
		{name: "archive", id: "SPEC-001", wantFound: true, wantArchived: true},
		{name: "missing", id: "SPEC-999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, ok := Find(root, "archive", tt.id)
			if ok != tt.wantFound {
				t.Fatalf("Find(%s) found = %v, want %v", tt.id, ok, tt.wantFound)
			}
			if !ok {
				return
			}
			if ref.ID != tt.id {
				t.Errorf("ID = %q, want %q", ref.ID, tt.id)
			}
			if ref.Archived != tt.wantArchived {
				t.Errorf("Archived = %v, want %v", ref.Archived, tt.wantArchived)
			}
			if ref.Title == "" || ref.Path == "" {
				t.Errorf("ref should carry title and path: %+v", ref)
			}
		})
	}
}

// Find and Scan must agree about which copy of a duplicated ID wins, or a
// detail pane and a rollup can describe different documents.
func TestFind_MatchesScanPrecedence(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "SPEC-004", "build", "")
	writeSpec(t, filepath.Join(root, "archive"), "SPEC-004", "closed", "")

	found, ok := Find(root, "archive", "SPEC-004")
	if !ok {
		t.Fatal("SPEC-004 not found")
	}
	g, err := Load(root, "archive")
	if err != nil {
		t.Fatal(err)
	}
	scanned, _ := g.Get("SPEC-004")
	if found.Path != scanned.Path || found.Archived != scanned.Archived {
		t.Errorf("Find resolved %+v but Scan resolved %+v", found, scanned)
	}
}

func TestFind_EmptyInputs(t *testing.T) {
	if _, ok := Find("", "archive", "SPEC-001"); ok {
		t.Error("an unconfigured specs dir resolves nothing")
	}
	if _, ok := Find(t.TempDir(), "archive", ""); ok {
		t.Error("an empty id resolves nothing")
	}
}
