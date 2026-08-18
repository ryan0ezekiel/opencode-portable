package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedDefaultManifestValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "app", "default.json"))
	if err != nil {
		t.Fatalf("cannot read embedded manifest: %v", err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", m.SchemaVersion)
	}
	if m.OpenCodeVersion == "" || m.SourceRepo == "" {
		t.Error("opencode_version/source missing")
	}
	// Every supported platform must have at least one runtime variant.
	for _, pk := range SupportedPlatforms() {
		i := strings.IndexByte(pk, '/')
		os, arch := pk[:i], pk[i+1:]
		if len(m.RuntimesFor(os, arch)) == 0 {
			t.Errorf("platform %s has no runtimes", pk)
		}
	}
	// Digests must be non-empty and well-formed.
	for _, v := range m.RuntimesFor("linux", "amd64") {
		if len(v.SHA256) != 64 {
			t.Errorf("variant %s: bad sha256 %q", v.Name, v.SHA256)
		}
		if v.URL == "" || v.Artifact == "" || v.Binary == "" {
			t.Errorf("variant %s: incomplete fields", v.Name)
		}
	}
	for _, tool := range m.ToolsFor("windows", "amd64") {
		if tool.Artifact == "" || tool.SHA256 == "" || tool.Binary == "" {
			t.Errorf("tool %s: incomplete fields", tool.Name)
		}
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	if _, err := Parse([]byte("{ broken")); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestParseRejectsMissingPlatforms(t *testing.T) {
	_, err := Parse([]byte(`{"schema_version":1,"opencode_version":"v1.0.0","source":"x","runtimes":{},"tools":{}}`))
	if err == nil {
		t.Fatal("expected error for empty runtimes")
	}
}

func TestParseRejectsNonHexDigest(t *testing.T) {
	// 64 chars but not hex: previously accepted by the length check alone,
	// then failing confusingly at every download verification.
	bad := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	rt := `{"variants":[{"name":"native","artifact":"a.tar.gz","url":"https://e/a.tar.gz","sha256":"` + bad + `","size":1,"archive":"tar.gz","binary":"opencode"}]}`
	doc := `{"schema_version":1,"opencode_version":"v1.0.0","source":"x","runtimes":{"linux/amd64":` + rt + `},"tools":{}}`
	if _, err := Parse([]byte(doc)); err == nil {
		t.Fatal("expected error for non-hex sha256")
	}
}

func TestParseRejectsNegativeSize(t *testing.T) {
	rt := `{"variants":[{"name":"native","artifact":"a.tar.gz","url":"https://e/a.tar.gz","sha256":"` + tSHA + `","size":-1,"archive":"tar.gz","binary":"opencode"}]}`
	doc := `{"schema_version":1,"opencode_version":"v1.0.0","source":"x","runtimes":{"linux/amd64":` + rt + `},"tools":{}}`
	if _, err := Parse([]byte(doc)); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	// Build a minimal manifest covering every supported platform.
	rt := `{"variants":[{"name":"native","artifact":"a.tar.gz","url":"https://e/a.tar.gz","sha256":"` + tSHA + `","size":1,"archive":"tar.gz","binary":"opencode"}]}`
	var b strings.Builder
	b.WriteString(`{"schema_version":1,"opencode_version":"v1.0.0","source":"x","runtimes":{`)
	for i, pk := range SupportedPlatforms() {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + pk + `":` + rt)
	}
	b.WriteString(`},"tools":{}}`)
	m, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	if err := m.Save(p); err != nil {
		t.Fatal(err)
	}
	m2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if m2.OpenCodeVersion != m.OpenCodeVersion {
		t.Errorf("version mismatch after round trip")
	}
}

// tSHA is a 64-char hex string for tests.
const tSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
