package environ

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRedirectsXDG(t *testing.T) {
	p := New("/usb")
	base := []string{
		"HOME=/home/user",
		"PATH=/usr/bin:/bin",
		"XDG_CONFIG_HOME=/home/user/.config",
		"XDG_DATA_HOME=/home/user/.local/share",
		"XDG_CACHE_HOME=/home/user/.cache",
		"LANG=en_US.UTF-8",
	}
	env := p.Build(base, nil)
	got := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			got[kv[:i]] = kv[i+1:]
		}
	}
	if got["XDG_CONFIG_HOME"] != "/usb/config" {
		t.Errorf("XDG_CONFIG_HOME = %q", got["XDG_CONFIG_HOME"])
	}
	if got["XDG_DATA_HOME"] != "/usb/data" {
		t.Errorf("XDG_DATA_HOME = %q", got["XDG_DATA_HOME"])
	}
	if got["XDG_CACHE_HOME"] != "/usb/cache" {
		t.Errorf("XDG_CACHE_HOME = %q", got["XDG_CACHE_HOME"])
	}
	if got["OPENCODE_DISABLE_AUTOUPDATE"] != "1" {
		t.Errorf("OPENCODE_DISABLE_AUTOUPDATE = %q", got["OPENCODE_DISABLE_AUTOUPDATE"])
	}
	if got["LANG"] != "en_US.UTF-8" {
		t.Errorf("unrelated var must pass through untouched, got %q", got["LANG"])
	}
}

func TestBuildToolDirsPrepended(t *testing.T) {
	p := New("/usb")
	env := p.Build([]string{"PATH=/usr/bin"}, []string{"/usb/tools/x"})
	got := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			got[kv[:i]] = kv[i+1:]
		}
	}
	if got["PATH"] != "/usb/tools/x"+string(os.PathListSeparator)+"/usr/bin" {
		t.Errorf("PATH = %q", got["PATH"])
	}
}

func TestDedupPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	in := "/a" + sep + "/b" + sep + "/a" + sep + "/c" + sep + "/b"
	got := dedupPath(in, sep)
	want := "/a" + sep + "/b" + sep + "/c"
	if got != want {
		t.Errorf("dedupPath = %q, want %q", got, want)
	}
	// Empty entries are removed.
	in2 := sep + "/a" + sep + sep + "/b" + sep
	if got := dedupPath(in2, sep); got != "/a"+sep+"/b" {
		t.Errorf("dedupPath(empty entries) = %q", got)
	}
}

func TestDescribe(t *testing.T) {
	p := New(filepath.Join("usb", "root"))
	env := p.Build([]string{"PATH=/usr/bin"}, nil)
	desc := p.Describe()
	found := false
	for _, d := range desc {
		if strings.Contains(d, "Config:") {
			found = true
		}
	}
	if !found {
		t.Errorf("Describe missing Config entry: %v", desc)
	}
	if len(env) == 0 {
		t.Error("Build returned empty env")
	}
}
