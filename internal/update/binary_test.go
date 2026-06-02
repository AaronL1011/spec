package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// buildTarGz returns a gzip-compressed tar archive containing a single "spec"
// binary with the given contents.
func buildTarGz(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "spec", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serveRelease starts an httptest server returning the archive and checksums,
// and returns a Release pointed at it. Skips on Windows where the asset is a
// zip the tar fixture does not produce.
func serveRelease(t *testing.T, archive []byte, checksums string) Release {
	t.Helper()
	name := assetName("1.2.3")
	mux := http.NewServeMux()
	mux.HandleFunc("/archive", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Release{
		Tag: "v1.2.3",
		Assets: []Asset{
			{Name: name, URL: srv.URL + "/archive"},
			{Name: checksumsAsset, URL: srv.URL + "/checksums"},
		},
	}
}

func TestReplaceBinary_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tar.gz fixture does not match the windows zip asset")
	}
	archive := buildTarGz(t, []byte("new-spec-binary"))
	name := assetName("1.2.3")
	checksums := fmt.Sprintf("%s  %s\n", sha256Hex(archive), name)
	rel := serveRelease(t, archive, checksums)

	dir := t.TempDir()
	execPath := filepath.Join(dir, "spec")
	if err := os.WriteFile(execPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(context.Background(), rel, "1.2.3", execPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-spec-binary" {
		t.Errorf("binary contents = %q, want %q", got, "new-spec-binary")
	}
}

func TestReplaceBinary_ChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tar.gz fixture does not match the windows zip asset")
	}
	archive := buildTarGz(t, []byte("tampered"))
	name := assetName("1.2.3")
	// Record a deliberately wrong checksum.
	checksums := fmt.Sprintf("%s  %s\n", sha256Hex([]byte("different")), name)
	rel := serveRelease(t, archive, checksums)

	dir := t.TempDir()
	execPath := filepath.Join(dir, "spec")
	if err := os.WriteFile(execPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := replaceBinary(context.Background(), rel, "1.2.3", execPath)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "old" {
		t.Errorf("binary was modified on checksum failure: %q", got)
	}
}

func TestAssetName(t *testing.T) {
	want := fmt.Sprintf("spec_1.2.3_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		want += ".zip"
	} else {
		want += ".tar.gz"
	}
	if got := assetName("v1.2.3"); got != want {
		t.Errorf("assetName = %q, want %q", got, want)
	}
}
