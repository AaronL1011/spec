// Package watch observes a fixed set of files for changes and emits debounced,
// coalesced notifications.
//
// It is purpose-built for the spec reader: it watches the two files backing the
// open spec — the spec markdown and its thread.yml sidecar — and signals when
// either changes so the TUI can re-read and re-render without the user quitting
// and reopening.
//
// The watcher watches the *containing directory* rather than the file inodes:
// editors and atomic-rename saves replace a file by writing a temp file and
// renaming over the target, which would orphan an inode-level watch. Directory
// watching plus a name filter survives that pattern.
//
// When the platform's filesystem notification backend cannot be registered
// (unsupported filesystem, descriptor exhaustion), the watcher transparently
// falls back to mtime polling at a fixed cadence. Callers see the same
// ChangeEvent stream either way.
package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// pollInterval is the cadence used by the polling fallback when native
// filesystem notifications are unavailable.
const pollInterval = 1 * time.Second

// ChangeEvent is emitted when one or more watched files change within a
// debounce window. Paths lists the absolute paths that changed.
type ChangeEvent struct {
	Paths []string
}

// Watcher observes a fixed set of files and emits debounced change events on C.
// It is single-target: Retarget swaps the watched set without recreating the
// underlying goroutine.
type Watcher struct {
	// C delivers debounced, coalesced change notifications. It is closed when
	// the watcher is closed.
	C <-chan ChangeEvent

	out      chan ChangeEvent
	debounce time.Duration

	mu      sync.Mutex
	dir     string           // directory currently watched
	targets map[string]bool  // base names of interest within dir
	mtimes  map[string]int64 // last-seen mtime per absolute path (polling)

	fsw    *fsnotify.Watcher // nil when in polling mode
	cancel context.CancelFunc
	done   chan struct{}
}

// New starts watching the files named in paths. All paths are expected to live
// in the same directory; the directory of paths[0] is watched and events are
// filtered to the base names of all paths.
//
// New never returns a nil Watcher on a non-nil error: callers that want the
// polling fallback get a working watcher even when fsnotify registration fails,
// so a returned error is purely advisory.
func New(ctx context.Context, paths []string, debounce time.Duration) (*Watcher, error) {
	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}
	out := make(chan ChangeEvent)
	cctx, cancel := context.WithCancel(ctx)
	w := &Watcher{
		C:        out,
		out:      out,
		debounce: debounce,
		targets:  make(map[string]bool),
		mtimes:   make(map[string]int64),
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	w.setTargets(paths)

	fsw, err := fsnotify.NewWatcher()
	if err == nil {
		if addErr := fsw.Add(w.dir); addErr != nil {
			_ = fsw.Close()
			fsw = nil
			err = addErr
		} else {
			w.fsw = fsw
		}
	}

	if w.fsw != nil {
		go w.runFSNotify(cctx)
		return w, nil
	}
	// Fall back to polling. The returned error tells the caller native
	// notifications were unavailable, but the watcher is fully functional.
	w.snapshotMtimes()
	go w.runPolling(cctx)
	return w, err
}

// setTargets records the watched directory and the set of base names of
// interest. paths is assumed non-empty.
func (w *Watcher) setTargets(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.targets = make(map[string]bool, len(paths))
	if len(paths) == 0 {
		w.dir = ""
		return
	}
	w.dir = filepath.Dir(paths[0])
	for _, p := range paths {
		w.targets[filepath.Base(p)] = true
	}
}

// Retarget switches the watched file set. All paths must share a directory.
// In fsnotify mode it re-points the directory watch; in polling mode it resets
// the mtime baseline.
func (w *Watcher) Retarget(paths []string) error {
	w.mu.Lock()
	oldDir := w.dir
	w.mu.Unlock()

	w.setTargets(paths)

	w.mu.Lock()
	newDir := w.dir
	fsw := w.fsw
	w.mu.Unlock()

	if fsw != nil && newDir != oldDir {
		if oldDir != "" {
			_ = fsw.Remove(oldDir)
		}
		if newDir != "" {
			if err := fsw.Add(newDir); err != nil {
				return err
			}
		}
	}
	if fsw == nil {
		w.snapshotMtimes()
	}
	return nil
}

// Close stops the watcher and closes C. It is safe to call more than once.
func (w *Watcher) Close() error {
	w.cancel()
	<-w.done
	if w.fsw != nil {
		return w.fsw.Close()
	}
	return nil
}

// targetPaths returns the absolute paths currently of interest.
func (w *Watcher) targetPaths() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	paths := make([]string, 0, len(w.targets))
	for base := range w.targets {
		paths = append(paths, filepath.Join(w.dir, base))
	}
	return paths
}

// matches reports whether an absolute path is one of the watched targets.
func (w *Watcher) matches(absPath string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return filepath.Dir(absPath) == w.dir && w.targets[filepath.Base(absPath)]
}

// runFSNotify drains fsnotify events, filters to the watched targets, and
// debounces bursts into single ChangeEvents.
func (w *Watcher) runFSNotify(ctx context.Context) {
	defer close(w.done)
	defer close(w.out)

	var timer *time.Timer
	var timerC <-chan time.Time
	pending := make(map[string]bool)

	arm := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
		} else {
			timer.Reset(w.debounce)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if !w.matches(ev.Name) {
				continue
			}
			pending[ev.Name] = true
			arm()
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			_ = err // errors are advisory; a missed event is recovered on the next change
		case <-timerC:
			timerC = nil
			if len(pending) == 0 {
				continue
			}
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = make(map[string]bool)
			select {
			case w.out <- ChangeEvent{Paths: paths}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// snapshotMtimes records the current mtime of every target path so the polling
// loop only reports subsequent changes.
func (w *Watcher) snapshotMtimes() {
	w.mu.Lock()
	w.mtimes = make(map[string]int64)
	w.mu.Unlock()
	for _, p := range w.targetPaths() {
		if info, err := os.Stat(p); err == nil {
			w.mu.Lock()
			w.mtimes[p] = info.ModTime().UnixNano()
			w.mu.Unlock()
		}
	}
}

// runPolling is the fallback loop: it stats the target paths on a fixed
// interval and emits a ChangeEvent for any whose mtime advanced or which
// appeared/disappeared since the last poll.
func (w *Watcher) runPolling(ctx context.Context) {
	defer close(w.done)
	defer close(w.out)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var changed []string
			for _, p := range w.targetPaths() {
				var cur int64 = -1
				if info, err := os.Stat(p); err == nil {
					cur = info.ModTime().UnixNano()
				}
				w.mu.Lock()
				prev, seen := w.mtimes[p]
				if cur != prev || !seen {
					w.mtimes[p] = cur
					if seen || cur != -1 {
						changed = append(changed, p)
					}
				}
				w.mu.Unlock()
			}
			if len(changed) == 0 {
				continue
			}
			select {
			case w.out <- ChangeEvent{Paths: changed}:
			case <-ctx.Done():
				return
			}
		}
	}
}
