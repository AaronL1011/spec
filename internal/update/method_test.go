package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDetectMethod(t *testing.T) {
	// Clear Go env so goBinDirs is deterministic for the non-go cases.
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	t.Run("homebrew cellar path", func(t *testing.T) {
		// Build a Cellar-shaped path under a temp dir and point a bin symlink
		// at it to mirror a real Homebrew install.
		base := t.TempDir()
		cellarBin := filepath.Join(base, "Cellar", "spec", "1.2.3", "bin")
		if err := os.MkdirAll(cellarBin, 0o755); err != nil {
			t.Fatal(err)
		}
		realPath := filepath.Join(cellarBin, "spec")
		writeExecutable(t, realPath)
		if got := DetectMethod(realPath); got != MethodHomebrew {
			t.Errorf("DetectMethod = %q, want %q", got, MethodHomebrew)
		}
	})

	t.Run("go install path via GOBIN", func(t *testing.T) {
		gobin := t.TempDir()
		t.Setenv("GOBIN", gobin)
		bin := filepath.Join(gobin, "spec")
		writeExecutable(t, bin)
		if got := DetectMethod(bin); got != MethodGoInstall {
			t.Errorf("DetectMethod = %q, want %q", got, MethodGoInstall)
		}
	})

	t.Run("go install path via GOPATH bin", func(t *testing.T) {
		t.Setenv("GOBIN", "")
		gopath := t.TempDir()
		t.Setenv("GOPATH", gopath)
		bin := filepath.Join(gopath, "bin", "spec")
		writeExecutable(t, bin)
		if got := DetectMethod(bin); got != MethodGoInstall {
			t.Errorf("DetectMethod = %q, want %q", got, MethodGoInstall)
		}
	})

	t.Run("raw binary path", func(t *testing.T) {
		t.Setenv("GOBIN", "")
		t.Setenv("GOPATH", "")
		dir := t.TempDir()
		bin := filepath.Join(dir, "spec")
		writeExecutable(t, bin)
		if got := DetectMethod(bin); got != MethodBinary {
			t.Errorf("DetectMethod = %q, want %q", got, MethodBinary)
		}
	})
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
