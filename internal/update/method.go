package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Method identifies how the running binary was installed, which determines the
// mechanism `spec update` delegates to.
type Method string

const (
	// MethodHomebrew indicates a Homebrew-managed install (brew upgrade).
	MethodHomebrew Method = "homebrew"
	// MethodGoInstall indicates a `go install`-managed install.
	MethodGoInstall Method = "go-install"
	// MethodBinary indicates a raw release binary with no package manager;
	// `spec update` self-replaces it from the GitHub release.
	MethodBinary Method = "binary"
)

// DetectMethod classifies how the executable at execPath was installed. The
// path is resolved through symlinks first so a Homebrew shim in bin/ is traced
// back to its Cellar location.
func DetectMethod(execPath string) Method {
	resolved := execPath
	if r, err := filepath.EvalSymlinks(execPath); err == nil {
		resolved = r
	}
	resolved = filepath.Clean(resolved)

	if isHomebrewPath(resolved) {
		return MethodHomebrew
	}
	if isGoInstallPath(filepath.Dir(resolved)) {
		return MethodGoInstall
	}
	return MethodBinary
}

// isHomebrewPath reports whether a resolved binary path lives inside a Homebrew
// Cellar. This covers macOS (/opt/homebrew, /usr/local) and Linuxbrew layouts,
// all of which place the real binary under ".../Cellar/<formula>/...".
func isHomebrewPath(resolved string) bool {
	return strings.Contains(resolved, string(filepath.Separator)+"Cellar"+string(filepath.Separator))
}

// isGoInstallPath reports whether dir is a Go install bin directory: GOBIN,
// $GOPATH/bin, or the default $HOME/go/bin. Candidates are symlink-resolved
// before comparison because DetectMethod resolves the binary path (on macOS,
// /var vs /private/var, or a symlinked $HOME) — comparing a resolved dir
// against unresolved candidates would misclassify a go-install binary as a raw
// binary and make `spec update` self-replace it instead of delegating to
// `go install`.
func isGoInstallPath(dir string) bool {
	dir = resolvePath(dir)
	for _, candidate := range goBinDirs() {
		if candidate != "" && resolvePath(candidate) == dir {
			return true
		}
	}
	return false
}

// resolvePath cleans a path and resolves symlinks when possible, falling back
// to the cleaned path when resolution fails (e.g., the path does not exist).
func resolvePath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// goBinDirs returns the candidate Go install directories, consulting the
// environment first and falling back to `go env` when the toolchain is present.
func goBinDirs() []string {
	dirs := []string{
		os.Getenv("GOBIN"),
		goEnv("GOBIN"),
	}
	for _, gopath := range []string{os.Getenv("GOPATH"), goEnv("GOPATH")} {
		for _, p := range filepath.SplitList(gopath) {
			if p != "" {
				dirs = append(dirs, filepath.Join(p, "bin"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

// goEnv reads a single `go env` value, returning "" when the Go toolchain is
// unavailable. Best-effort: the absence of `go` is expected on many machines.
func goEnv(key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
