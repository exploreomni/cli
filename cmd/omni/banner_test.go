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
			"My Org":  {APIEndpoint: "https://my.omniapp.co", AuthMethod: "api-key", APIKey: "secret-key"},
		},
	}

	for _, tc := range []struct {
		name, instance, auth, hint string
	}{
		{"acme", "acme.omniapp.co", "api-key", "Next: omni models list"},
		{"staging", "staging.omniapp.co", "oauth", "Set as default: omni config use staging"},
		{"legacy", "legacy.omniapp.co", "api-key", "Set as default: omni config use legacy"},
		{"blank", "not configured", "api-key", "Set as default: omni config use blank"},
		{"My Org", "my.omniapp.co", "api-key", "Set as default: omni config use 'My Org'"},
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

// An endpoint is saved as typed. Only its host may reach the banner: not a
// user:password, not a query string that might carry a token, not a path.
func TestEndpointHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://acme.omniapp.co":                   "acme.omniapp.co",
		"https://acme.omniapp.co/":                  "acme.omniapp.co",
		"https://acme.omniapp.co:8443/api/v1?x=1#f": "acme.omniapp.co:8443",
		"https://user:secret@acme.omniapp.co":       "acme.omniapp.co",
		"https://acme.omniapp.co/?api_token=secret": "acme.omniapp.co",
		"http://localhost:3000":                     "localhost:3000",
		"":                                          "not configured",
		"acme.omniapp.co":                           "unrecognized endpoint",
		"https://":                                  "unrecognized endpoint",
		"://secret@nowhere":                         "unrecognized endpoint",
		"https://user:secret@%zz":                   "unrecognized endpoint",
	} {
		got := endpointHost(in)
		if got != want {
			t.Errorf("endpointHost(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("endpointHost(%q) leaked a credential: %q", in, got)
		}
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

// Whether a person is at the terminal is interactiveHumanOutput's call,
// shared with the update notice. On top of that, some environments don't
// want the banner even then, and agents must never find it in a command's
// output.
func TestBannerOptedOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing set", nil, false},
		{"an ordinary terminal", map[string]string{"TERM": "xterm-256color"}, false},
		{"opted out", map[string]string{"OMNI_NO_BANNER": "1"}, true},
		{"ci", map[string]string{"CI": "true"}, true},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, true},
		{"claude code", map[string]string{"CLAUDECODE": "1"}, true},
		{"cursor agent", map[string]string{"CURSOR_AGENT": "1"}, true},
		{"codex", map[string]string{"CODEX_SANDBOX": "seatbelt"}, true},
		{"gemini cli", map[string]string{"GEMINI_CLI": "1"}, true},
		{"generic agent marker", map[string]string{"AI_AGENT": "some-agent"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bannerOptedOut(getenv(tc.env)); got != tc.want {
				t.Errorf("bannerOptedOut = %v, want %v", got, tc.want)
			}
		})
	}
}
