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

	// Non-spec noise must be skipped; a spec-named file that will not parse
	// must be carried as a corrupt ref, not skipped.
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
	if len(refs) != 5 {
		t.Fatalf("Scan returned %d refs, want 5: %+v", len(refs), refs)
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
	corrupt, ok := g.Get("SPEC-666")
	if !ok || !corrupt.Corrupt {
		t.Errorf("unparseable spec = %+v, want carried with Corrupt true", corrupt)
	}
	if corrupt.Title != "" || corrupt.Status != "" {
		t.Errorf("corrupt ref must not carry salvaged title or status: %+v", corrupt)
	}
}

// writeCorruptSpec drops a spec file whose frontmatter is present but will
// not parse as YAML, optionally naming a parent on a salvageable line.
func writeCorruptSpec(t *testing.T, dir, id, parent string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	fm := "---\nid: " + id + "\ntitle: [unclosed\nstatus: build\n"
	if parent != "" {
		fm += "parent: " + parent + "\n"
	}
	fm += "---\n\n# mangled\n"
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(fm), 0o644); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
}

func TestScan_CorruptSpecSalvagesParentEdge(t *testing.T) {
	// A corrupt slice must still count against its initiative's rollup, or
	// children_complete passes over a file nobody can read.
	root := t.TempDir()
	writeSpec(t, root, "SPEC-004", "build", "")
	writeSpec(t, root, "SPEC-009", "done", "SPEC-004")
	writeCorruptSpec(t, root, "SPEC-010", "SPEC-004")

	g, err := Load(root, "archive")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := g.Rollup("SPEC-004", testPipeline())
	if r.Total != 2 || r.Complete != 1 || r.Open != 1 {
		t.Errorf("rollup = %+v, want the corrupt slice counted as open (2 total, 1 complete)", r)
	}
	if r.IsComplete() {
		t.Error("an initiative with an unreadable slice must never read as complete")
	}
}

func TestScan_ParentProseNeverMintsAnEdge(t *testing.T) {
	// "parent:" in the body of a file with no frontmatter must not be
	// salvaged: only lines inside the frontmatter block count.
	root := t.TempDir()
	writeSpec(t, root, "SPEC-004", "build", "")
	body := "# notes\n\nparent: SPEC-004\n"
	if err := os.WriteFile(filepath.Join(root, "SPEC-010.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := Load(root, "archive")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.HasChildren("SPEC-004") {
		t.Error("prose outside frontmatter minted a parent edge")
	}
	ref, _ := g.Get("SPEC-010")
	if !ref.Corrupt || ref.Parent != "" {
		t.Errorf("ref = %+v, want corrupt with no salvaged parent", ref)
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
// detail pane and a rollup can describe different documents. The corrupt
// case is the sharp edge: a corrupt root copy must shadow a healthy archive
// copy in both, exactly as the CLI's path resolver would open it.
func TestFind_MatchesScanPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		writeRoot   func(t *testing.T, root string)
		wantCorrupt bool
	}{
		{
			name:      "healthy root copy wins",
			writeRoot: func(t *testing.T, root string) { writeSpec(t, root, "SPEC-004", "build", "") },
		},
		{
			name:        "corrupt root copy still wins",
			writeRoot:   func(t *testing.T, root string) { writeCorruptSpec(t, root, "SPEC-004", "") },
			wantCorrupt: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.writeRoot(t, root)
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
			if found.Path != scanned.Path || found.Archived != scanned.Archived || found.Corrupt != scanned.Corrupt {
				t.Errorf("Find resolved %+v but Scan resolved %+v", found, scanned)
			}
			if found.Corrupt != tt.wantCorrupt {
				t.Errorf("Corrupt = %v, want %v", found.Corrupt, tt.wantCorrupt)
			}
		})
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
