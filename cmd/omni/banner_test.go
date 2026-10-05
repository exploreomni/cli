package main

import (
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
)

func bannerFields(info output.BannerInfo) map[string]string {
	fields := map[string]string{}
	for _, f := range info.Fields {
		fields[f.Label] = f.Value
	}
	return fields
}

func TestBannerInfo(t *testing.T) {
	cfg := &config.Config{
		DefaultProfile: "acme",
		Profiles: map[string]config.Profile{
			"acme":    {APIEndpoint: "https://acme.omniapp.co", AuthMethod: "api-key", APIKey: "secret-key"},
			"staging": {APIEndpoint: "https://staging.omniapp.co", AuthMethod: "oauth", AccessToken: "secret-token", RefreshToken: "secret-refresh"},
			"legacy":  {APIEndpoint: "https://legacy.omniapp.co", APIKey: "secret-key"},
			"blank":   {},
		},
	}
	const nextHint, useHint = "Next: omni models list", "Not the default profile: omni config use"

	for _, tc := range []struct {
		name, instance, auth, hint string
	}{
		{"acme", "acme.omniapp.co", "api-key", nextHint},
		{"staging", "staging.omniapp.co", "oauth", useHint},
		{"legacy", "legacy.omniapp.co", "api-key", useHint},
		{"blank", "not configured", "api-key", useHint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := bannerInfo("1.4.0", tc.name, cfg, "/work")
			fields := bannerFields(info)
			if fields["Profile"] != tc.name || fields["Instance"] != tc.instance || fields["Auth"] != tc.auth {
				t.Errorf("fields = %v, want profile %q, instance %q, auth %q", fields, tc.name, tc.instance, tc.auth)
			}
			if info.Hint != tc.hint {
				t.Errorf("hint = %q, want %q", info.Hint, tc.hint)
			}
			if info.Version != "v1.4.0" || info.Dir != "/work" {
				t.Errorf("version %q and dir %q should pass through", info.Version, info.Dir)
			}
			// The banner never shows a secret, whichever way the profile authenticates.
			for _, f := range info.Fields {
				if strings.Contains(f.Value, "secret") {
					t.Errorf("%s shows a secret: %q", f.Label, f.Value)
				}
			}
		})
	}
}

func TestBannerVersion(t *testing.T) {
	for in, want := range map[string]string{
		"1.4.0":        "v1.4.0",
		"v1.4.0":       "v1.4.0",
		"1.5.0-beta.1": "v1.5.0-beta.1",
		"dev":          "dev",
		"(devel)":      "(devel)",
	} {
		if got := bannerVersion(in); got != want {
			t.Errorf("bannerVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// The banner is for a person watching `config init` or `config login`
// finish. Agents and scripts must never find it in a command's output.
func TestBannerAllowed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		isTTY  bool
		format string
		env    map[string]string
		want   bool
	}{
		{"a person at a terminal", true, config.FormatHuman, nil, true},
		{"an ordinary terminal", true, config.FormatHuman, map[string]string{"TERM": "xterm-256color"}, true},
		{"piped or redirected", false, config.FormatHuman, nil, false},
		{"json output format", true, config.FormatJSON, nil, false},
		{"opted out", true, config.FormatHuman, map[string]string{"OMNI_NO_BANNER": "1"}, false},
		{"ci", true, config.FormatHuman, map[string]string{"CI": "true"}, false},
		{"dumb terminal", true, config.FormatHuman, map[string]string{"TERM": "dumb"}, false},
		{"claude code", true, config.FormatHuman, map[string]string{"CLAUDECODE": "1"}, false},
		{"cursor agent", true, config.FormatHuman, map[string]string{"CURSOR_AGENT": "1"}, false},
		{"codex", true, config.FormatHuman, map[string]string{"CODEX_SANDBOX": "seatbelt"}, false},
		{"gemini cli", true, config.FormatHuman, map[string]string{"GEMINI_CLI": "1"}, false},
		{"generic agent marker", true, config.FormatHuman, map[string]string{"AI_AGENT": "some-agent"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bannerAllowed(tc.isTTY, tc.format, getenv(tc.env)); got != tc.want {
				t.Errorf("bannerAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
