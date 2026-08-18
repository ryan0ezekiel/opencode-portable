// Package acquire downloads, verifies and installs OpenCode runtimes and
// tools onto the USB volume.
//
// Security model:
//   - downloads come only from URLs listed in the manifest (themselves
//     generated from the official release);
//   - every artifact is verified against a sha256 digest before use;
//   - archives are extracted with path-traversal protection and a total
//     size bound;
//   - installation is atomic: a new runtime is staged and renamed into
//     place, never partially overwriting an existing one.
package acquire

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
	"strings"
	"time"
)

// MaxExtractBytes bounds the total uncompressed size of an extracted
// artifact (defense against oversized/corrupt archives). The limit is
// enforced on the actual number of bytes written, not on archive metadata
// (declared sizes are not authoritative).
const MaxExtractBytes = 1 << 30 // 1 GiB

// MaxDownloadTime bounds a single artifact download. Downloads can be large
// (tens of MB) and connections can stall; this is a generous fallback that
// guarantees the launcher never hangs forever on a dead connection.
const MaxDownloadTime = 20 * time.Minute

// ErrChecksumMismatch reports a verification failure.
type ErrChecksumMismatch struct {
	Path string
	Want string
	Got  string
}

func (e *ErrChecksumMismatch) Error() string {
	return fmt.Sprintf("checksum mismatch for %s: expected sha256 %s, got %s", e.Path, e.Want, e.Got)
}

// ErrSizeMismatch reports a size verification failure.
type ErrSizeMismatch struct {
	Path string
	Want int64
	Got  int64
}

func (e *ErrSizeMismatch) Error() string {
	return fmt.Sprintf("size mismatch for %s: expected %d bytes, got %d", e.Path, e.Want, e.Got)
}

// Downloader fetches artifacts with optional progress reporting.
type Downloader struct {
	// Quiet suppresses the progress bar.
	Quiet bool
	// Client is the HTTP client used (defaults to a sane one).
	Client *http.Client
	// Logf receives informational messages ("" disables).
	Logf func(format string, args ...any)
}

// NewDownloader creates a Downloader with defaults.
func NewDownloader() *Downloader {
	return &Downloader{
		Client: &http.Client{
			Timeout: MaxDownloadTime, // fallback bound; the caller's ctx still wins
		},
	}
}

// Download fetches url to destPath. If expectedSize > 0 the download is
// checked against it; if expectedSHA256 is non-empty the file is verified.
//
// When a file already exists at destPath with the expected size and digest,
// the download is skipped: artifacts are immutable and digest-addressed, so
// a verified copy is as good as a fresh one (and much faster on repeated
// runs).
func (d *Downloader) Download(ctx context.Context, url, destPath string, expectedSize int64, expectedSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	// Reuse an existing verified artifact instead of re-downloading.
	if expectedSHA256 != "" {
		if fi, err := os.Stat(destPath); err == nil {
			if expectedSize <= 0 || fi.Size() == expectedSize {
				if err := VerifySHA256(destPath, expectedSHA256); err == nil {
					if d.Logf != nil {
						d.Logf("already downloaded %s (verified)", filepath.Base(destPath))
					}
					return nil
				}
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "OpenCodePortable/1.0 (+portable launcher)")

	client := d.Client
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	part := destPath + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	total := expectedSize
	if total <= 0 {
		total = resp.ContentLength
	}
	if total < 0 {
		total = 0
	}

	hash := sha256.New()
	var written int64
	buf := make([]byte, 128*1024)
	lastReport := time.Now()

	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return werr
			}
			hash.Write(buf[:n])
			written += int64(n)
			if !d.Quiet && total > 0 && time.Since(lastReport) > 200*time.Millisecond {
				lastReport = time.Now()
				pct := float64(written) * 100 / float64(total)
				fmt.Fprintf(os.Stderr, "\r\033[K  downloading %s: %6.1f%% (%s / %s)", filepath.Base(destPath), pct, humanSize(written), humanSize(total))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return fmt.Errorf("download interrupted: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if !d.Quiet {
		fmt.Fprintf(os.Stderr, "\r\033[K")
	}

	if expectedSize > 0 && written != expectedSize {
		os.Remove(part)
		return &ErrSizeMismatch{Path: filepath.Base(destPath), Want: expectedSize, Got: written}
	}
	if expectedSHA256 != "" {
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(got, expectedSHA256) {
			os.Remove(part)
			return &ErrChecksumMismatch{Path: filepath.Base(destPath), Want: expectedSHA256, Got: got}
		}
	}
	return os.Rename(part, destPath)
}

// CleanupStaleParts removes incomplete download remnants (*.part) older
// than olderThan. A live download cannot be older than MaxDownloadTime by
// definition, so age-based cleanup never races an active download; it only
// reclaims files left behind by interrupted runs.
func CleanupStaleParts(dir string, olderThan time.Duration) int {
	matches, err := filepath.Glob(filepath.Join(dir, "*.part"))
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().Before(cutoff) {
			if os.Remove(m) == nil {
				removed++
			}
		}
	}
	return removed
}

// VerifySHA256 checks a file against an expected digest.
func VerifySHA256(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return &ErrChecksumMismatch{Path: path, Want: expected, Got: got}
	}
	return nil
}

// Extract unpacks an archive (zip or tar.gz) into destDir with path
// traversal protection. "raw" archives are copied as-is.
func Extract(archivePath, kind, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	switch kind {
	case "zip":
		return extractZip(archivePath, destDir)
	case "tar.gz":
		return extractTarGz(archivePath, destDir)
	case "raw":
		return copyRaw(archivePath, filepath.Join(destDir, filepath.Base(archivePath)))
	default:
		return fmt.Errorf("unknown archive type %q", kind)
	}
}

func copyRaw(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func safeJoin(base, name string) (string, error) {
	// Normalize backslash separators so that Windows-style paths inside
	// archives cannot escape the destination on any platform.
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean(name)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("archive contains invalid empty path")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("archive contains absolute path %q", name)
	}
	target := filepath.Join(base, clean)
	if !strings.HasPrefix(target, filepath.Clean(base)+string(filepath.Separator)) {
		return "", fmt.Errorf("archive path %q escapes destination", name)
	}
	return target, nil
}

func extractZip(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("invalid zip archive: %w", err)
	}
	defer zr.Close()

	var written int64
	for _, f := range zr.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		// Declared sizes are metadata, not authority; the per-entry copy
		// below enforces the true budget on actual bytes written.
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := f.Mode()
		if mode&0o111 != 0 {
			mode |= 0o755
		} else {
			mode |= 0o644
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		n, cerr := copyBounded(out, rc, MaxExtractBytes-written)
		written += n
		rc.Close()
		werr := out.Close()
		if cerr != nil {
			return cerr
		}
		if werr != nil {
			return werr
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var written int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("corrupt tar archive: %w", err)
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			if mode&0o111 != 0 {
				mode |= 0o755
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			n, cerr := copyBounded(out, tr, MaxExtractBytes-written)
			written += n
			werr := out.Close()
			if cerr != nil {
				return cerr
			}
			if werr != nil {
				return werr
			}
		case tar.TypeSymlink:
			// Symlinks are not needed for the artifacts we install; skip
			// them rather than risk following a malicious link.
			continue
		default:
			continue
		}
	}
	return nil
}

// copyBounded copies from src to dst but never more than max bytes; it
// fails when the source still has data beyond max. This is the authoritative
// extraction budget: it counts actual bytes written, not declared sizes.
func copyBounded(dst io.Writer, src io.Reader, max int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, max+1))
	if n > max {
		return n, fmt.Errorf("archive exceeds extraction limit")
	}
	return n, err
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
