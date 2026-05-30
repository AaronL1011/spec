package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/store"
)

// testAppWithSpecsDir builds an app whose specs repo points at a temp dir
// containing one spec file, so the file watcher has something to observe.
func testAppWithSpecsDir(t *testing.T) (App, string) {
	t.Helper()
	dir := t.TempDir()
	spec := filepath.Join(dir, "SPEC-007.md")
	body := "---\nid: SPEC-007\ntitle: File Watcher\nstatus: build\n---\n\n## 1. Problem Statement\n\npain\n"
	if err := os.WriteFile(spec, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	rc := testResolvedConfig()
	rc.SpecsRepoDir = dir
	app := newAppWithDB(rc, testRegistry(), "engineer", db)
	app.width = 100
	app.height = 30
	return app, dir
}

func TestOpenDetail_StartsWatcher(t *testing.T) {
	app, _ := testAppWithSpecsDir(t)
	cmd := app.openDetail("SPEC-007")
	defer app.stopWatch()

	if app.watcher == nil {
		t.Fatal("openDetail should start a watcher for a resolvable spec")
	}
	if cmd == nil {
		t.Error("openDetail should return a batch command (init + watch)")
	}
}

func TestCloseDetail_StopsWatcher(t *testing.T) {
	app, _ := testAppWithSpecsDir(t)
	app.openDetail("SPEC-007")
	if app.watcher == nil {
		t.Fatal("precondition: watcher should be running")
	}
	app.closeDetail()
	if app.watcher != nil {
		t.Error("closeDetail should stop and clear the watcher")
	}
}

func TestSwitchView_StopsWatcher(t *testing.T) {
	app, _ := testAppWithSpecsDir(t)
	app.openDetail("SPEC-007")
	if app.watcher == nil {
		t.Fatal("precondition: watcher should be running")
	}
	app.switchView(ViewDashboard)
	if app.watcher != nil {
		t.Error("switchView should stop the watcher when leaving the detail view")
	}
}

func TestStartWatch_NoSpecsRepoIsNoop(t *testing.T) {
	db, _ := store.OpenMemory()
	app := newAppWithDB(testResolvedConfig(), testRegistry(), "engineer", db) // no SpecsRepoDir
	app.width, app.height = 100, 30
	cmd := app.openDetail("SPEC-007")
	defer app.stopWatch()
	if app.watcher != nil {
		t.Error("no specs repo means no resolvable file and therefore no watcher")
	}
	_ = cmd
}

func TestFileChangedMsg_TriggersRefreshAndRearm(t *testing.T) {
	app, dir := testAppWithSpecsDir(t)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app = model.(App)

	m2, _ := app.Update(navigateToSpecMsg{SpecID: "SPEC-007"})
	app = m2.(App)
	defer app.stopWatch()

	if app.watcher == nil {
		t.Fatal("expected watcher after navigating to spec")
	}

	// Mutate the spec on disk and deliver a synthetic change message; the
	// handler should issue a refresh command and re-arm the watcher.
	spec := filepath.Join(dir, "SPEC-007.md")
	_ = os.WriteFile(spec, []byte("---\nid: SPEC-007\ntitle: File Watcher\nstatus: build\n---\n\n## 1. Problem Statement\n\nupdated pain\n"), 0o644)

	m3, cmd := app.Update(fileChangedMsg{Paths: []string{spec}})
	app = m3.(App)
	if cmd == nil {
		t.Fatal("fileChangedMsg should produce a command (refresh + re-arm)")
	}
	if !app.watchRefreshPending {
		t.Error("a delivered file change should mark a pending refresh for the cue")
	}
}

func TestFileChangedMsg_IgnoredWhenDetailClosed(t *testing.T) {
	app, _ := testAppWithSpecsDir(t)
	// No detail open, no watcher.
	_, cmd := app.Update(fileChangedMsg{Paths: []string{"x"}})
	if cmd != nil {
		t.Error("fileChangedMsg with no open detail should be a no-op")
	}
}

func TestWatcher_StopsWithinReasonableTime(t *testing.T) {
	app, _ := testAppWithSpecsDir(t)
	app.openDetail("SPEC-007")
	w := app.watcher
	done := make(chan struct{})
	go func() { app.stopWatch(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stopWatch did not return promptly (possible goroutine deadlock)")
	}
	// Channel should be closed after stop.
	if _, ok := <-w.C; ok {
		t.Error("watcher channel should be closed after stopWatch")
	}
}
