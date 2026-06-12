package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// checkCache is the persisted record of the last passive update check. It lives
// at ~/.spec/update-check.json so the TUI can show the notice instantly on the
// next launch and only re-query GitHub once the TTL has elapsed.
type checkCache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

// defaultCachePath returns the on-disk location of the update-check cache,
// mirroring the store's ~/.spec home with a working-directory fallback.
func defaultCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".spec", "update-check.json")
	}
	return filepath.Join(home, ".spec", "update-check.json")
}

// readCheckCache loads the cache, returning a zero value when it is missing or
// corrupt. A zero value reads as "stale", which simply triggers a fresh check —
// so a bad cache file is self-healing rather than fatal.
func readCheckCache(path string) checkCache {
	var c checkCache
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	// Best-effort: a corrupt cache decodes to the zero value and is re-checked.
	_ = json.Unmarshal(data, &c)
	return c
}

// writeCheckCache persists the cache best-effort. Any failure is non-fatal —
// the next launch simply performs a live check — so errors are swallowed
// rather than surfaced into an ambient notice.
func writeCheckCache(path string, c checkCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return
	}
}
