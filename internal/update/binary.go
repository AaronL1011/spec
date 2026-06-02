package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadTimeout bounds release-archive downloads (per AGENTS.md network defaults).
const downloadTimeout = 30 * time.Second

// maxArchiveBytes caps an extracted archive to guard against decompression
// bombs. Release archives are a few MB; 200 MB is generous headroom.
const maxArchiveBytes = 200 << 20

// binaryName is the executable file packed inside release archives.
const binaryName = "spec"

// checksumsAsset is the release asset listing per-archive SHA-256 sums.
const checksumsAsset = "checksums.txt"

// assetName returns the release archive name for the given version and the
// current platform, matching the .goreleaser.yaml name_template
// ("spec_<version>_<os>_<arch>.<ext>"). The version is supplied without its
// leading "v" because goreleaser strips it.
func assetName(version string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	v := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("%s_%s_%s_%s.%s", binaryName, v, runtime.GOOS, runtime.GOARCH, ext)
}

// replaceBinary downloads the release archive for rel, verifies its checksum,
// extracts the spec binary, and atomically swaps it over execPath.
func replaceBinary(ctx context.Context, rel Release, version, execPath string) error {
	name := assetName(version)
	archive, ok := rel.asset(name)
	if !ok {
		return fmt.Errorf("release %s has no asset %q for this platform", rel.Tag, name)
	}
	sums, ok := rel.asset(checksumsAsset)
	if !ok {
		return fmt.Errorf("release %s has no %s — cannot verify download", rel.Tag, checksumsAsset)
	}

	client := &http.Client{Timeout: downloadTimeout}
	tmpDir, err := os.MkdirTemp("", "spec-update-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, name)
	if err := download(ctx, client, archive.URL, archivePath); err != nil {
		return err
	}
	if err := verifyChecksum(ctx, client, sums.URL, archivePath, name); err != nil {
		return err
	}

	binPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return err
	}
	return swapBinary(binPath, execPath)
}

// download streams url to dest with the given client.
func download(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w — check your network connection", filepath.Base(dest), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: server returned %s", filepath.Base(dest), resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dest, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxArchiveBytes)); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

// verifyChecksum fetches the checksums file and compares the SHA-256 of the
// downloaded archive against the recorded sum for assetName. A mismatch is
// fatal — an unverified binary is never installed.
func verifyChecksum(ctx context.Context, client *http.Client, sumsURL, archivePath, assetName string) error {
	sumsPath := filepath.Join(filepath.Dir(archivePath), checksumsAsset)
	if err := download(ctx, client, sumsURL, sumsPath); err != nil {
		return err
	}
	want, err := checksumFor(sumsPath, assetName)
	if err != nil {
		return err
	}
	got, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s — aborting update", assetName, want, got)
	}
	return nil
}

// checksumFor reads a "sha256<space*>name" checksums file and returns the sum
// recorded for the named asset.
func checksumFor(sumsPath, assetName string) (string, error) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", fmt.Errorf("reading checksums: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum recorded for %s", assetName)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary pulls the spec executable out of archivePath into destDir and
// returns the path to the extracted binary.
func extractBinary(archivePath, destDir string) (string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, destDir)
	}
	return extractFromTarGz(archivePath, destDir)
}

func extractFromTarGz(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("reading gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar archive: %w", err)
		}
		if filepath.Base(hdr.Name) == binaryName && hdr.Typeflag == tar.TypeReg {
			return writeExtracted(tr, destDir)
		}
	}
	return "", fmt.Errorf("archive does not contain a %q binary", binaryName)
}

func extractFromZip(archivePath, destDir string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening zip archive: %w", err)
	}
	defer func() { _ = zr.Close() }()

	target := binaryName + ".exe"
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) == target {
			rc, err := zf.Open()
			if err != nil {
				return "", fmt.Errorf("opening %s in archive: %w", zf.Name, err)
			}
			defer func() { _ = rc.Close() }()
			return writeExtracted(rc, destDir)
		}
	}
	return "", fmt.Errorf("archive does not contain a %q binary", target)
}

// writeExtracted copies the extracted binary stream into destDir and marks it
// executable, returning its path.
func writeExtracted(r io.Reader, destDir string) (string, error) {
	out := filepath.Join(destDir, binaryName+".new")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("creating extracted binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, io.LimitReader(r, maxArchiveBytes)); err != nil {
		return "", fmt.Errorf("extracting binary: %w", err)
	}
	return out, nil
}

// swapBinary atomically replaces the executable at execPath with newBin. The
// new binary is staged in the target directory first so the final rename is on
// the same filesystem (and therefore atomic). On Windows the running executable
// is moved aside before the swap because it cannot be overwritten in place.
func swapBinary(newBin, execPath string) error {
	dir := filepath.Dir(execPath)
	staged := filepath.Join(dir, binaryName+".new")
	if err := copyFile(newBin, staged, 0o755); err != nil {
		return wrapPermission(err, execPath)
	}

	if runtime.GOOS == "windows" {
		old := execPath + ".old"
		_ = os.Remove(old)
		if err := os.Rename(execPath, old); err != nil {
			_ = os.Remove(staged)
			return wrapPermission(err, execPath)
		}
	}
	if err := os.Rename(staged, execPath); err != nil {
		_ = os.Remove(staged)
		return wrapPermission(err, execPath)
	}
	return nil
}

// copyFile copies src to dst with the given permission bits. Staging in the
// destination directory keeps the subsequent rename on one filesystem.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// wrapPermission augments permission-denied errors with the manual next step so
// the user is not left guessing when the binary lives in a root-owned dir.
func wrapPermission(err error, execPath string) error {
	if os.IsPermission(err) {
		return fmt.Errorf("cannot write %s: %w — re-run with elevated permissions (e.g. sudo) or reinstall manually", execPath, err)
	}
	return fmt.Errorf("replacing %s: %w", execPath, err)
}
