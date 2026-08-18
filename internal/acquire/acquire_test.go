package acquire

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownloadCacheSkip verifies that an existing verified artifact is
// reused instead of re-downloaded.
func TestDownloadCacheSkip(t *testing.T) {
	body := []byte("artifact-bytes")
	sum := sha256.Sum256(body)
	wantSHA := hex.EncodeToString(sum[:])

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "artifact")
	ctx := context.Background()

	d := NewDownloader()
	d.Quiet = true
	if err := d.Download(ctx, srv.URL, dest, int64(len(body)), wantSHA); err != nil {
		t.Fatalf("first download failed: %v", err)
	}
	if hits != 1 {
		t.Fatalf("first download: hits = %d, want 1", hits)
	}

	// Second download must be served from cache: no network request.
	if err := d.Download(ctx, srv.URL, dest, int64(len(body)), wantSHA); err != nil {
		t.Fatalf("cached download failed: %v", err)
	}
	if hits != 1 {
		t.Fatalf("cached download: hits = %d, want 1 (must not re-download)", hits)
	}

	// A corrupt existing file must be re-downloaded and repaired.
	if err := os.WriteFile(dest, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.Download(ctx, srv.URL, dest, int64(len(body)), wantSHA); err != nil {
		t.Fatalf("repair download failed: %v", err)
	}
	if hits != 2 {
		t.Fatalf("repair download: hits = %d, want 2", hits)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("repaired artifact does not match")
	}
}

// TestCopyBounded verifies the extraction budget counts actual bytes.
func TestCopyBounded(t *testing.T) {
	var out bytes.Buffer
	src := strings.NewReader("01234567890123456789") // 20 bytes

	n, err := copyBounded(&out, src, 10)
	if err == nil {
		t.Fatalf("copyBounded(20 bytes, max 10) should fail, wrote %d", n)
	}
	if !strings.Contains(err.Error(), "exceeds extraction limit") {
		t.Fatalf("unexpected error: %v", err)
	}

	out.Reset()
	src = strings.NewReader("01234")
	n, err = copyBounded(&out, src, 10)
	if err != nil {
		t.Fatalf("copyBounded(5 bytes, max 10) failed: %v", err)
	}
	if n != 5 || out.String() != "01234" {
		t.Fatalf("copyBounded wrote %d bytes: %q", n, out.String())
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{
		"../../etc/passwd",
		"..",
		"a/../../evil",
		"/absolute/path",
		`..\..\evil`,
	} {
		if _, err := safeJoin(base, name); err == nil {
			t.Errorf("safeJoin(%q) should reject traversal", name)
		}
	}
	if p, err := safeJoin(base, "normal/file.txt"); err != nil {
		t.Errorf("safeJoin(normal) failed: %v", err)
	} else if filepath.Dir(p) != filepath.Join(base, "normal") {
		t.Errorf("safeJoin returned %q", p)
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Malicious entry with traversal path.
	if err := tw.WriteHeader(&tar.Header{Name: "../../evil.txt", Mode: 0o644, Size: 5}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("evil!"))
	_ = tw.Close()
	_ = gz.Close()

	path := filepath.Join(dest, "bad.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(path, "tar.gz", dest); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("evil file escaped the destination")
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	path := filepath.Join(dest, "bad.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("evil!"))
	_ = zw.Close()
	_ = f.Close()

	if err := Extract(path, "zip", dest); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("evil file escaped the destination")
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	// sha256("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := VerifySHA256(path, want); err != nil {
		t.Errorf("VerifySHA256 should pass: %v", err)
	}
	if err := VerifySHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Error("VerifySHA256 should fail on mismatch")
	}
	if err := VerifySHA256(path, "short"); err == nil {
		t.Error("VerifySHA256 should reject malformed digest")
	}
}

func TestHumanSize(t *testing.T) {
	if humanSize(0) != "0 B" {
		t.Errorf("humanSize(0) = %q", humanSize(0))
	}
	if humanSize(1024) != "1.0 KiB" {
		t.Errorf("humanSize(1024) = %q", humanSize(1024))
	}
}
