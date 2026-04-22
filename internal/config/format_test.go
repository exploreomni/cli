package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOutputFormat_FlagBeatsEverything(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "human")
	withTempConfig(t, `{"version":1,"defaultOutputFormat":"human","profiles":{}}`)
	if got := ResolveOutputFormat("json", true); got != "json" {
		t.Errorf("flag should win, got %q", got)
	}
}

func TestResolveOutputFormat_EnvBeatsConfig(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "json")
	withTempConfig(t, `{"version":1,"defaultOutputFormat":"human","profiles":{}}`)
	if got := ResolveOutputFormat("", true); got != "json" {
		t.Errorf("env should beat config, got %q", got)
	}
}

func TestResolveOutputFormat_ConfigBeatsAuto(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "")
	withTempConfig(t, `{"version":1,"defaultOutputFormat":"human","profiles":{}}`)
	if got := ResolveOutputFormat("", false); got != "human" {
		t.Errorf("config should beat auto, got %q", got)
	}
}

func TestResolveOutputFormat_AutoTTY(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "")
	withTempConfig(t, `{"version":1,"profiles":{}}`)
	if got := ResolveOutputFormat("", true); got != "human" {
		t.Errorf("auto on TTY should be human, got %q", got)
	}
	if got := ResolveOutputFormat("", false); got != "json" {
		t.Errorf("auto off TTY should be json, got %q", got)
	}
}

func TestResolveOutputFormat_AutoFlagRespectsTTY(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "")
	withTempConfig(t, `{"version":1,"profiles":{}}`)
	if got := ResolveOutputFormat("auto", true); got != "human" {
		t.Errorf("flag=auto on TTY should be human, got %q", got)
	}
	if got := ResolveOutputFormat("auto", false); got != "json" {
		t.Errorf("flag=auto off TTY should be json, got %q", got)
	}
}

// withTempConfig points OMNI_CONFIG_PATH at a temp file containing the given JSON.
func withTempConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write temp config: %v", err)
		}
	}
	t.Setenv("OMNI_CONFIG_PATH", path)
}
