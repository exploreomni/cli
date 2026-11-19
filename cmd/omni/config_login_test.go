package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// oauthFixture reads a server response body from the oauth package's testdata.
func oauthFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "oauth", "testdata", name+".json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return b
}

// deviceOrgServer is a fake org host that offers the device grant and
// approves on the first poll.
func deviceOrgServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{
			"/.well-known/oauth-authorization-server": "discovery_with_device",
			"/oauth/device_authorization":             "device_authorization",
			"/oauth/token":                            "device_token_success",
		}[r.URL.Path]
		if name == "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(oauthFixture(t, name))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// refusingServer fails the test on any request: --browser must not touch
// discovery or the device endpoints.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubBrowserLogin replaces the browser login for the test; fn runs in its
// place and returns the token to save.
func stubBrowserLogin(t *testing.T, fn func(endpoint string) (*oauth2.Token, error)) {
	t.Helper()
	prev := browserLogin
	browserLogin = fn
	t.Cleanup(func() { browserLogin = prev })
}

// rootWithConfig builds the parts of the real root a config command needs:
// the global flags (--format, --profile) and the config group.
func rootWithConfig(args ...string) *cobra.Command {
	root := &cobra.Command{Use: "omni", SilenceUsage: true, SilenceErrors: true}
	addGlobalFlags(root)
	addConfigCommands(root)
	root.SetArgs(args)
	return root
}

func loginTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")
	t.Setenv("OMNI_OUTPUT_FORMAT", "")
	t.Setenv("OMNI_NO_BANNER", "1")
	prev := devicePollInterval
	devicePollInterval = time.Second
	t.Cleanup(func() { devicePollInterval = prev })
}

// `config login --format json` leaves exactly one JSON line on stdout, the
// device code, and saves the tokens the device grant returned.
func TestConfigLogin_FormatJSONWritesOneDeviceLine(t *testing.T) {
	loginTestEnv(t)
	srv := deviceOrgServer(t)
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		t.Error("browser login ran; the server offers the device grant")
		return nil, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {APIEndpoint: srv.URL, AuthMethod: "api-key", APIKey: "old-key"},
		},
	})

	var err error
	out := captureStdout(t, func() {
		err = rootWithConfig("config", "login", "prod", "--format", "json").Execute()
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout = %q, want exactly one line", out)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(out), &line); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", out, err)
	}
	for _, k := range []string{"verification_uri", "verification_uri_complete", "user_code", "expires_in"} {
		if _, ok := line[k]; !ok {
			t.Errorf("JSON line %v lacks %q", line, k)
		}
	}
	if len(line) != 4 {
		t.Errorf("JSON line %v, want exactly the four device fields", line)
	}
	if line["user_code"] != "BDKF-QRTW" {
		t.Errorf("user_code = %v, want BDKF-QRTW", line["user_code"])
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}
	p := cfg.Profiles["prod"]
	if p.AuthMethod != "oauth" || p.AccessToken != "device-access-token" || p.RefreshToken != "device-refresh-token" {
		t.Errorf("saved profile = %+v, want the device grant's OAuth tokens", p)
	}
}

func TestConfigLogin_BrowserFlagForcesBrowser(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	var gotEndpoint string
	stubBrowserLogin(t, func(endpoint string) (*oauth2.Token, error) {
		gotEndpoint = endpoint
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh", Expiry: time.Now().Add(time.Hour)}, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles:       map[string]config.Profile{"prod": {APIEndpoint: srv.URL, AuthMethod: "oauth"}},
	})

	var err error
	out := captureStdout(t, func() {
		err = rootWithConfig("config", "login", "--browser").Execute()
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if gotEndpoint != srv.URL {
		t.Errorf("browser login endpoint = %q, want %q", gotEndpoint, srv.URL)
	}
	if !strings.Contains(out, `Login successful! Profile "prod" updated.`) {
		t.Errorf("stdout = %q, want the success message", out)
	}
	cfg, _ := config.Load()
	if got := cfg.Profiles["prod"].AccessToken; got != "browser-access" {
		t.Errorf("saved access token = %q, want browser-access", got)
	}
}

// The tokens land on the config as it is on disk once the login finishes:
// a write another process made while the person was approving survives.
func TestConfigLogin_KeepsConcurrentConfigChanges(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod":  {APIEndpoint: srv.URL, AuthMethod: "oauth"},
			"other": {APIEndpoint: "https://other.omniapp.co", AuthMethod: "oauth", RefreshToken: "r1"},
		},
	})
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		// A refresh of another profile, mid-login.
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("loading config: %v", err)
		}
		other := cfg.Profiles["other"]
		other.RefreshToken = "r2"
		cfg.Profiles["other"] = other
		if err := config.Save(cfg); err != nil {
			t.Fatalf("saving config: %v", err)
		}
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
	})

	captureStdout(t, func() {
		if err := rootWithConfig("config", "login", "prod", "--browser").Execute(); err != nil {
			t.Errorf("login: %v", err)
		}
	})

	cfg, _ := config.Load()
	if got := cfg.Profiles["other"].RefreshToken; got != "r2" {
		t.Errorf("other profile's refresh token = %q, want r2 (login wrote back the config it read before logging in)", got)
	}
	if got := cfg.Profiles["prod"].AccessToken; got != "browser-access" {
		t.Errorf("prod access token = %q, want browser-access", got)
	}
}

func TestConfigInit_OAuthBrowserSavesProfile(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "existing",
		Profiles:       map[string]config.Profile{"existing": {APIEndpoint: "https://x.omniapp.co", AuthMethod: "api-key", APIKey: "k"}},
	})

	captureStdout(t, func() {
		if err := rootWithConfig("config", "init", "--name", "prod", "--endpoint", srv.URL, "--auth", "oauth", "--browser").Execute(); err != nil {
			t.Errorf("init: %v", err)
		}
	})

	cfg, _ := config.Load()
	if got := cfg.Profiles["prod"]; got.AuthMethod != "oauth" || got.AccessToken != "browser-access" || got.APIEndpoint != srv.URL {
		t.Errorf("prod profile = %+v, want the OAuth tokens for %s", got, srv.URL)
	}
	if _, ok := cfg.Profiles["existing"]; !ok || cfg.DefaultProfile != "existing" {
		t.Errorf("config = %+v, want the existing profile and default kept", cfg)
	}
}
