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

func TestChosenOutputFormat_Normalizes(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", " Human ")
	withTempConfig(t, `{"version":1,"profiles":{}}`)
	if got := ChosenOutputFormat(" JSON "); got != FormatJSON {
		t.Errorf("flag: got %q, want %q", got, FormatJSON)
	}
	if got := ChosenOutputFormat(""); got != FormatHuman {
		t.Errorf("env: got %q, want %q", got, FormatHuman)
	}
}

func TestCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name, flag, env, config, wantErr string
	}{
		{name: "unset", flag: "", env: ""},
		{name: "valid flag", flag: "json"},
		{name: "flag any case", flag: " HUMAN "},
		{name: "valid env", env: "Auto"},
		{name: "bad flag", flag: "html", wantErr: `invalid output format "html" (json, human, auto)`},
		{name: "blank flag", flag: " ", wantErr: `invalid output format " " (json, human, auto)`},
		{name: "bad env", env: "html", wantErr: `invalid OMNI_OUTPUT_FORMAT "html" (json, human, auto)`},
		{name: "valid flag beats bad env", flag: "json", env: "html"},
		{name: "bad flag beats valid env", flag: "html", env: "json", wantErr: `invalid output format "html" (json, human, auto)`},
		// The config file is left to `config set-format`, which fixes it.
		{name: "bad config ignored", config: "html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OMNI_OUTPUT_FORMAT", tt.env)
			withTempConfig(t, `{"version":1,"defaultOutputFormat":"`+tt.config+`","profiles":{}}`)
			err := CheckOutputFormat(tt.flag)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
				t.Errorf("got error %v, want %q", err, tt.wantErr)
			}
		})
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
