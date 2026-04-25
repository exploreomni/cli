package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"golang.org/x/oauth2"
)

// withConfig points OMNI_CONFIG_PATH at a temp file containing cfg, so the
// command under test reads/writes that file instead of the user's real config.
func withConfig(t *testing.T, cfg *config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OMNI_CONFIG_PATH", path)
	if cfg != nil {
		if err := config.Save(cfg); err != nil {
			t.Fatalf("seeding config: %v", err)
		}
	}
	return path
}

// captureStdout swaps os.Stdout for a pipe and returns whatever fn writes to it.
// Used to assert against the human-readable output of cobra commands without
// having to refactor them to take an io.Writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	_ = w.Close()
	return <-done
}

// applyOAuthToken is a one-liner but it's the single point that translates an
// oauth2.Token into Profile fields; a regression here would silently break
// every successful login.
func TestApplyOAuthToken_CopiesAllFields(t *testing.T) {
	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	p := &config.Profile{APIEndpoint: "https://myorg.omniapp.co"}

	applyOAuthToken(p, &oauth2.Token{
		AccessToken:  "access-xyz",
		RefreshToken: "refresh-xyz",
		Expiry:       expiry,
	})

	if p.AuthMethod != "oauth" {
		t.Errorf("AuthMethod = %q, want %q", p.AuthMethod, "oauth")
	}
	if p.AccessToken != "access-xyz" {
		t.Errorf("AccessToken = %q, want %q", p.AccessToken, "access-xyz")
	}
	if p.RefreshToken != "refresh-xyz" {
		t.Errorf("RefreshToken = %q, want %q", p.RefreshToken, "refresh-xyz")
	}
	if got, want := p.TokenExpiresAt, expiry.Format(time.RFC3339); got != want {
		t.Errorf("TokenExpiresAt = %q, want %q", got, want)
	}
	// APIEndpoint should be untouched — the helper only writes auth fields.
	if p.APIEndpoint != "https://myorg.omniapp.co" {
		t.Errorf("APIEndpoint mutated: got %q", p.APIEndpoint)
	}
}

// --- config logout ---

func TestConfigLogout_ClearsTokensForNamedProfile(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {
				APIEndpoint:    "https://myorg.omniapp.co",
				AuthMethod:     "oauth",
				AccessToken:    "a",
				RefreshToken:   "r",
				TokenExpiresAt: "2099-01-01T00:00:00Z",
			},
			"staging": {
				APIEndpoint:  "https://staging.omniapp.co",
				AuthMethod:   "oauth",
				AccessToken:  "should-not-touch",
				RefreshToken: "should-not-touch",
			},
		},
	})

	cmd := configLogoutCmd()
	out := captureStdout(t, func() {
		if err := cmd.RunE(cmd, []string{"prod"}); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, `Logged out of profile "prod"`) {
		t.Errorf("stdout = %q, want it to confirm logout", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prod := cfg.Profiles["prod"]
	if prod.AccessToken != "" || prod.RefreshToken != "" || prod.TokenExpiresAt != "" {
		t.Errorf("prod tokens not cleared: %+v", prod)
	}
	// Other profiles must be left alone — logout is per-profile.
	if cfg.Profiles["staging"].AccessToken != "should-not-touch" {
		t.Error("logout leaked into staging profile")
	}
}

// No arg should fall back to the default profile.
func TestConfigLogout_DefaultProfile(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {
				AuthMethod:   "oauth",
				AccessToken:  "a",
				RefreshToken: "r",
			},
		},
	})

	cmd := configLogoutCmd()
	_ = captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	cfg, _ := config.Load()
	if cfg.Profiles["prod"].AccessToken != "" {
		t.Error("default profile's tokens were not cleared")
	}
}

func TestConfigLogout_UnknownProfile(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {AuthMethod: "oauth", AccessToken: "a"},
		},
	})

	cmd := configLogoutCmd()
	err := cmd.RunE(cmd, []string{"nope"})
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	if !strings.Contains(err.Error(), `"nope" not found`) {
		t.Errorf("error = %q, want it to mention the missing profile name", err.Error())
	}
}

// No arg AND no default profile → must surface a clear error rather than
// silently doing nothing or panicking on an empty map key.
func TestConfigLogout_NoArgNoDefault(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{},
	})

	cmd := configLogoutCmd()
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when no profile arg and no default, got nil")
	}
	if !strings.Contains(err.Error(), "no profile specified") {
		t.Errorf("error = %q, want it to mention missing profile", err.Error())
	}
}

// Logout when there's no config file at all should produce a friendly
// "run config init" error, not a JSON parse error or panic.
func TestConfigLogout_NoConfigFile(t *testing.T) {
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))

	cmd := configLogoutCmd()
	err := cmd.RunE(cmd, []string{"prod"})
	if err == nil {
		t.Fatal("expected error when no config exists, got nil")
	}
	if !strings.Contains(err.Error(), "config init") {
		t.Errorf("error = %q, want it to point user at `config init`", err.Error())
	}
}

// --- config show redaction ---
//
// `config show` prints the config as JSON to stdout. It MUST redact API keys,
// access tokens, and refresh tokens — `omni config show > config.txt; pbcopy`
// is a common debugging pattern and we don't want to leak credentials.

func TestConfigShow_RedactsSecrets(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {
				APIEndpoint:    "https://myorg.omniapp.co",
				AuthMethod:     "oauth",
				AccessToken:    "super-secret-access-token",
				RefreshToken:   "super-secret-refresh-token",
				TokenExpiresAt: "2099-01-01T00:00:00Z",
			},
			"legacy": {
				APIEndpoint: "https://legacy.omniapp.co",
				AuthMethod:  "api-key",
				APIKey:      "abcd1234efgh5678ijkl",
			},
			"tinykey": {
				APIEndpoint: "https://tiny.omniapp.co",
				AuthMethod:  "api-key",
				APIKey:      "xy7k", // <12 chars → wholly redacted to "****"
			},
		},
	})

	cmd := configShowCmd()
	out := captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	for _, secret := range []string{
		"super-secret-access-token",
		"super-secret-refresh-token",
		"abcd1234efgh5678ijkl",
		"xy7k",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("`config show` leaked secret %q in output:\n%s", secret, out)
		}
	}

	// Long API keys get a fingerprint preview (first/last 4 chars) — useful for
	// distinguishing which key is in play without exposing the full value.
	if !strings.Contains(out, "abcd...ijkl") {
		t.Errorf(`expected long-key fingerprint "abcd...ijkl", got:\n%s`, out)
	}
	// Short keys are wholly redacted to "****" because there's nothing safe to show.
	if !strings.Contains(out, "****") {
		t.Errorf(`expected redacted "****" placeholder, got:\n%s`, out)
	}
	// Non-secret fields should still be visible — show is supposed to be useful.
	if !strings.Contains(out, "https://myorg.omniapp.co") {
		t.Errorf("expected APIEndpoint to remain visible, got:\n%s", out)
	}
}

func TestConfigShow_NoConfig(t *testing.T) {
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.json"))

	cmd := configShowCmd()
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when no config exists, got nil")
	}
	if !strings.Contains(err.Error(), "config init") {
		t.Errorf("error = %q, want it to suggest `config init`", err.Error())
	}
}
