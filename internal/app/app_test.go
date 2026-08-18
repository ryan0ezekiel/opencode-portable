package app

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Options
	}{
		{
			"no args",
			nil,
			Options{Forward: []string{}},
		},
		{
			"all launcher flags",
			[]string{"--diagnose", "--dry-run", "--update", "--all-platforms", "--verbose"},
			Options{Diagnose: true, DryRun: true, Update: true, UpdateAll: true, Verbose: true, Forward: []string{}},
		},
		{
			"help forwarded",
			[]string{"--help"},
			Options{Forward: []string{"--help"}},
		},
		{
			"run command forwarded",
			[]string{"run", "explain this"},
			Options{Forward: []string{"run", "explain this"}},
		},
		{
			"everything after -- forwarded",
			[]string{"--dry-run", "--", "--dry-run", "run"},
			Options{DryRun: true, Forward: []string{"--dry-run", "run"}},
		},
		{
			"mixed",
			[]string{"--verbose", "run", "--model", "x", "--diagnose"},
			Options{Verbose: true, Diagnose: true, Forward: []string{"run", "--model", "x"}},
		},
	}
	for _, c := range cases {
		got := ParseArgs(c.args)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ParseArgs(%v) = %+v, want %+v", c.name, c.args, got, c.want)
		}
	}
}

func TestPortableConfigValidate(t *testing.T) {
	valid := []string{"prefer_host_fallback_usb", "usb_only", "host_only"}
	for _, v := range valid {
		if err := (portableConfig{ToolPolicy: v, LogLevel: "info"}).validate(); err != nil {
			t.Errorf("policy %q should be valid: %v", v, err)
		}
	}
	if err := (portableConfig{ToolPolicy: "bogus", LogLevel: "info"}).validate(); err == nil {
		t.Error("bogus policy should be rejected")
	}
	if err := (portableConfig{ToolPolicy: "usb_only", LogLevel: "bogus"}).validate(); err == nil {
		t.Error("bogus log level should be rejected")
	}
	// An empty file resolves to the default config, which is valid.
	c := defaultPortableConfig()
	c.ToolPolicy = "" // simulate a config that set nothing
	if err := c.validate(); err == nil {
		t.Error("empty policy should be rejected by validate (callers fill defaults first)")
	}
}

func TestDefaultPortableConfig(t *testing.T) {
	c := defaultPortableConfig()
	if c.ToolPolicy != "prefer_host_fallback_usb" {
		t.Errorf("default policy = %q", c.ToolPolicy)
	}
	if c.LogLevel != "info" {
		t.Errorf("default log level = %q", c.LogLevel)
	}
}

func TestParseLogLevel(t *testing.T) {
	if parseLogLevel("debug") != 0 {
		t.Error("debug should parse")
	}
	if parseLogLevel("bogus") != 1 {
		t.Error("bogus should fall back to info")
	}
}
