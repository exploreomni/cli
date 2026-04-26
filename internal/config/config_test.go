package config

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// clearEnv unsets all Omni env vars so tests start from a clean slate.
// t.Setenv snapshots the original value and restores it when the test ends,
// so other tests and the user's shell aren't affected.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OMNI_API_TOKEN",
		"OMNI_BASE_URL",
		"OMNI_CONFIG_PATH",
		"OMNI_CONFIG_DIR",
		"OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS",
		"XDG_CONFIG_HOME",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

// writeConfig saves a config file to a temp directory and points
// OMNI_CONFIG_PATH at it, so the test uses this file instead of ~/.config/...
func writeConfig(t *testing.T, cfg *Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OMNI_CONFIG_PATH", path)
	if err := Save(cfg); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

// --- Config resolution precedence ---
//
// The CLI resolves config from three sources with this priority:
//   1. Command-line flags (--token, --base-url) — highest priority
//   2. Environment variables (OMNI_API_TOKEN, OMNI_BASE_URL, etc.)
//   3. Config file (~/.config/omni-cli/config.json) — lowest priority
//
// These tests verify that higher-priority sources override lower ones.

// All three sources set different values. Flags should win.
func TestResolve_FlagsOverrideAll(t *testing.T) {
	clearEnv(t)

	// Write a config file with values
	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIKey:      "file-token",
				APIEndpoint: "https://file.omniapp.co",
			},
		},
	})

	// Set env vars with different values
	t.Setenv("OMNI_API_TOKEN", "env-token")
	t.Setenv("OMNI_BASE_URL", "https://env.omniapp.co")

	// Pass flags that should win
	rc, err := Resolve("", "flag-token", "https://flag.omniapp.co")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "flag-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "flag-token")
	}
	if rc.BaseURL != "https://flag.omniapp.co" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://flag.omniapp.co")
	}
}

// Config file has values, env vars have different values, no flags.
// Env vars should win over the file.
func TestResolve_EnvOverridesFile(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIKey:      "file-token",
				APIEndpoint: "https://file.omniapp.co",
			},
		},
	})

	t.Setenv("OMNI_API_TOKEN", "env-token")
	t.Setenv("OMNI_BASE_URL", "https://env.omniapp.co")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "env-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "env-token")
	}
	if rc.BaseURL != "https://env.omniapp.co" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://env.omniapp.co")
	}
}

// No flags, no env vars — config file values should be used as-is.
func TestResolve_FileValues(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]Profile{
			"prod": {
				APIKey:      "file-token",
				APIEndpoint: "https://file.omniapp.co",
			},
		},
	})

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "file-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "file-token")
	}
	if rc.BaseURL != "https://file.omniapp.co" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://file.omniapp.co")
	}
}

// When the user doesn't pass --profile, the config's DefaultProfile field
// determines which profile to use. This test has two profiles and verifies
// the default is selected automatically.
func TestResolve_DefaultProfile(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "second",
		Profiles: map[string]Profile{
			"first": {
				APIKey:      "first-token",
				APIEndpoint: "https://first.test.omniapp.co",
			},
			"second": {
				APIKey:      "second-token",
				APIEndpoint: "https://second.test.omniapp.co",
			},
		},
	})

	// No profileName arg — should use DefaultProfile ("second")
	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "second-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "second-token")
	}
	if rc.BaseURL != "https://second.test.omniapp.co" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://second.test.omniapp.co")
	}
}

// If no token is configured anywhere, Resolve should return a helpful error
// telling the user how to set one up.
func TestResolve_MissingToken(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	_, err := Resolve("", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no API token") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no API token")
	}
}

// Token is set but no base URL — should also error. Both are required.
func TestResolve_MissingBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	// Token set, but no base URL
	t.Setenv("OMNI_API_TOKEN", "some-token")

	_, err := Resolve("", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no API base URL") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no API base URL")
	}
}

// --- Security: HTTPS and domain allowlist ---
//
// The CLI refuses to send API tokens over non-HTTPS connections or to
// unrecognized domains, unless OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS is set.

func TestResolve_RejectsHTTP(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OMNI_API_TOKEN", "some-token")
	t.Setenv("OMNI_BASE_URL", "http://myorg.omniapp.co")

	_, err := Resolve("", "", "")
	if err == nil {
		t.Fatal("expected error for HTTP base URL, got nil")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("error = %q, want it to mention HTTPS", err.Error())
	}
}

func TestResolve_RejectsUnknownDomain(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OMNI_API_TOKEN", "some-token")
	t.Setenv("OMNI_BASE_URL", "https://evil.com")

	_, err := Resolve("", "", "")
	if err == nil {
		t.Fatal("expected error for unrecognized domain, got nil")
	}
	if !strings.Contains(err.Error(), "not a recognized Omni domain") {
		t.Errorf("error = %q, want it to mention recognized domain", err.Error())
	}
}

func TestResolve_InsecureEnvBypassesChecks(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OMNI_API_TOKEN", "some-token")
	t.Setenv("OMNI_BASE_URL", "http://localhost:3000")
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("expected insecure env var to bypass checks, got: %v", err)
	}
	if rc.BaseURL != "http://localhost:3000" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "http://localhost:3000")
	}
}

// ValidateEndpoint is the single gate that prevents API tokens or OAuth
// credentials from leaving for a non-HTTPS or non-Omni host. These tests cover
// it directly so we're not relying on Resolve() to exercise all the edge cases.

func TestValidateEndpoint_RejectsHTTP(t *testing.T) {
	clearEnv(t)
	if err := ValidateEndpoint("http://myorg.omniapp.co"); err == nil {
		t.Fatal("expected error for http://, got nil")
	} else if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("error = %q, want it to mention HTTPS", err.Error())
	}
}

func TestValidateEndpoint_RejectsUnknownDomain(t *testing.T) {
	clearEnv(t)
	if err := ValidateEndpoint("https://evil.com"); err == nil {
		t.Fatal("expected error for non-Omni domain, got nil")
	} else if !strings.Contains(err.Error(), "not a recognized Omni domain") {
		t.Errorf("error = %q, want it to mention recognized domain", err.Error())
	}
}

func TestValidateEndpoint_AllowsOmniHTTPS(t *testing.T) {
	clearEnv(t)
	if err := ValidateEndpoint("https://myorg.omniapp.co"); err != nil {
		t.Errorf("unexpected error for valid Omni endpoint: %v", err)
	}
}

func TestValidateEndpoint_InsecureBypass(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")
	if err := ValidateEndpoint("http://localhost:3000"); err != nil {
		t.Errorf("expected insecure bypass to allow http://localhost, got: %v", err)
	}
	if err := ValidateEndpoint("https://evil.com"); err != nil {
		t.Errorf("expected insecure bypass to allow non-Omni domain, got: %v", err)
	}
}

// --- OAuth refresh safety ---

// If the saved profile's apiEndpoint isn't an allowlisted HTTPS Omni domain,
// Resolve() must NOT send the refresh token there, even if env vars or flags
// point rc.BaseURL at a legitimate host. This protects against a poisoned
// profile exfiltrating the refresh token on every CLI invocation.
func TestResolve_SkipsRefreshForNonAllowlistedEndpoint(t *testing.T) {
	clearEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// Return a real-looking token — if the CLI ignores validation and
		// hits us anyway, we want the refresh to visibly "succeed" so the
		// failure mode is clear rather than appearing as an oauth2 error.
		_, _ = w.Write([]byte(`{"access_token":"leaked","refresh_token":"leaked","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIEndpoint:    srv.URL, // attacker-controlled, non-Omni, http://
				AuthMethod:     "oauth",
				AccessToken:    "old-access",
				RefreshToken:   "old-refresh",
				TokenExpiresAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339), // expired
			},
		},
	})

	// Mask the bad profile endpoint with a legitimate base URL so that the
	// final validation passes and we get to observe what happened during refresh.
	t.Setenv("OMNI_BASE_URL", "https://myorg.omniapp.co")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hits.Load() != 0 {
		t.Errorf("Resolve made %d request(s) to non-allowlisted endpoint; refresh token was leaked", hits.Load())
	}
	// Falls through with the stale access token — exactly the documented behavior.
	if rc.Token != "old-access" {
		t.Errorf("rc.Token = %q, want the stale %q (refresh should have been skipped)", rc.Token, "old-access")
	}
}

// Positive counterpart: when the endpoint passes validation (via the insecure
// override), the refresh actually happens and rc.Token reflects the new token.
// This guards against over-correcting and breaking legitimate refreshes.
func TestResolve_RefreshesWhenEndpointValid(t *testing.T) {
	clearEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIEndpoint:    srv.URL,
				AuthMethod:     "oauth",
				AccessToken:    "old-access",
				RefreshToken:   "old-refresh",
				TokenExpiresAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
		},
	})
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hits.Load() != 1 {
		t.Errorf("expected exactly 1 refresh request, got %d", hits.Load())
	}
	if rc.Token != "new-access" {
		t.Errorf("rc.Token = %q, want %q (refresh should have succeeded)", rc.Token, "new-access")
	}

	// Sanity check: the refreshed token should have been persisted back to disk.
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load after refresh: %v", err)
	}
	if got := loaded.Profiles["test"].AccessToken; got != "new-access" {
		t.Errorf("persisted AccessToken = %q, want %q", got, "new-access")
	}
}

// An OAuth profile with no refresh token shouldn't trigger any network call —
// the CLI just hands the stored access token to the caller. The most common
// way this happens: the org's auth server didn't issue a refresh_token.
func TestResolve_OAuthProfileWithoutRefreshTokenSkipsNetwork(t *testing.T) {
	clearEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIEndpoint:    srv.URL,
				AuthMethod:     "oauth",
				AccessToken:    "stored-access",
				TokenExpiresAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339), // expired but no refresh token
			},
		},
	})
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hits.Load() != 0 {
		t.Errorf("Resolve made %d request(s); should be zero when refresh token is absent", hits.Load())
	}
	if rc.Token != "stored-access" {
		t.Errorf("rc.Token = %q, want %q", rc.Token, "stored-access")
	}
}

// If the refresh request fails (server down, 5xx, network error), Resolve
// should NOT return an error — it should fall through with the stale access
// token and let the eventual API call return 401, giving the user a clearer
// "your session expired, run `omni config login`" signal than a refresh
// transport error would.
func TestResolve_OAuthRefreshFailureFallsThrough(t *testing.T) {
	clearEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIEndpoint:    srv.URL,
				AuthMethod:     "oauth",
				AccessToken:    "stale-access",
				RefreshToken:   "stale-refresh",
				TokenExpiresAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
		},
	})
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error on refresh failure; expected fall-through: %v", err)
	}
	if rc.Token != "stale-access" {
		t.Errorf("rc.Token = %q, want %q (the stale token)", rc.Token, "stale-access")
	}
}

// A non-expired OAuth token shouldn't be refreshed — the oauth2 library only
// hits the network when the token is near/past expiry. This test guards against
// a future change accidentally inverting that behavior and refreshing on every
// CLI invocation.
func TestResolve_OAuthFreshTokenSkipsRefresh(t *testing.T) {
	clearEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIEndpoint:    srv.URL,
				AuthMethod:     "oauth",
				AccessToken:    "fresh-access",
				RefreshToken:   "fresh-refresh",
				TokenExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
		},
	})
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")

	rc, err := Resolve("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hits.Load() != 0 {
		t.Errorf("Resolve made %d request(s); a non-expired token must not trigger refresh", hits.Load())
	}
	if rc.Token != "fresh-access" {
		t.Errorf("rc.Token = %q, want %q", rc.Token, "fresh-access")
	}
}

// Ensure the test-server URL we generated above is actually not on the
// allowlist — otherwise TestResolve_SkipsRefreshForNonAllowlistedEndpoint
// would pass vacuously.
func TestHttptestURLIsNotAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	if isAllowedHost(srv.URL) {
		t.Errorf("httptest URL %q (host %q) unexpectedly matched allowlist", srv.URL, u.Host)
	}
}

func TestIsAllowedHost(t *testing.T) {
	allowed := []string{
		"https://omniapp.co",
		"https://myorg.omniapp.co",
		"https://deep.sub.omniapp.co",
		"https://exploreomni.dev",
		"https://myorg.exploreomni.dev",
		"https://thundersalmon.com",
		"https://myorg.thundersalmon.com",
		"https://embed-omniapp.co",
		"https://myorg.embed-omniapp.co",
		"https://embed-exploreomni.dev",
		"https://myorg.embed-exploreomni.dev",
		"https://embed-thundersalmon.com",
		"https://myorg.embed-thundersalmon.com",
	}
	for _, u := range allowed {
		if !isAllowedHost(u) {
			t.Errorf("isAllowedHost(%q) = false, want true", u)
		}
	}

	blocked := []string{
		"https://evil.com",
		"https://omniapp.co.evil.com",
		"https://notomniapp.co",
		"https://localhost",
		"https://169.254.169.254",
		"https://evil.com/path?host=omniapp.co",
	}
	for _, u := range blocked {
		if isAllowedHost(u) {
			t.Errorf("isAllowedHost(%q) = true, want false", u)
		}
	}
}

// --- Config path ---
//
// ConfigPath() determines where the config file lives on disk.
// It defaults to ~/.config/omni-cli/config.json but can be overridden
// via OMNI_CONFIG_PATH for testing or non-standard setups.

func TestConfigPath_EnvOverride(t *testing.T) {
	clearEnv(t)
	want := "/tmp/custom/config.json"
	t.Setenv("OMNI_CONFIG_PATH", want)

	got := ConfigPath()
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPath_Default(t *testing.T) {
	clearEnv(t)

	got := ConfigPath()
	if !strings.HasSuffix(got, filepath.Join("omni-cli", "config.json")) {
		t.Errorf("ConfigPath() = %q, want suffix %q", got, filepath.Join("omni-cli", "config.json"))
	}
	// On non-Windows, default should be under ~/.config
	if runtime.GOOS != "windows" {
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".config", "omni-cli", "config.json")
		if got != want {
			t.Errorf("ConfigPath() = %q, want %q", got, want)
		}
	}
}

func TestConfigDir_OMNI_CONFIG_DIR(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_DIR", "/tmp/custom-omni")

	got := ConfigPath()
	want := filepath.Join("/tmp/custom-omni", "config.json")
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigDir_XDG_CONFIG_HOME(t *testing.T) {
	clearEnv(t)
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

	got := ConfigPath()
	want := filepath.Join("/tmp/xdg", "omni-cli", "config.json")
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigDir_OMNI_CONFIG_DIR_OverridesXDG(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_DIR", "/tmp/omni-wins")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-loses")

	got := ConfigPath()
	want := filepath.Join("/tmp/omni-wins", "config.json")
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q (OMNI_CONFIG_DIR should beat XDG_CONFIG_HOME)", got, want)
	}
}

// --- Load/Save ---
//
// These test the JSON serialization of the config file. Save writes it,
// Load reads it back. A round-trip test ensures no data is lost.

// Write a config with multiple profiles, read it back, verify every field matches.
func TestLoadSaveRoundTrip(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OMNI_CONFIG_PATH", path)

	original := &Config{
		Version:        1,
		DefaultProfile: "myprofile",
		Profiles: map[string]Profile{
			"myprofile": {
				APIEndpoint: "https://api.test.omniapp.co",
				AuthMethod:  "apiKey",
				APIKey:      "secret-key",
			},
			"other": {
				APIEndpoint: "https://other.test.omniapp.co",
				APIKey:      "other-key",
			},
		},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version = %d, want %d", loaded.Version, original.Version)
	}
	if loaded.DefaultProfile != original.DefaultProfile {
		t.Errorf("DefaultProfile = %q, want %q", loaded.DefaultProfile, original.DefaultProfile)
	}
	if len(loaded.Profiles) != len(original.Profiles) {
		t.Fatalf("len(Profiles) = %d, want %d", len(loaded.Profiles), len(original.Profiles))
	}

	for name, orig := range original.Profiles {
		got, ok := loaded.Profiles[name]
		if !ok {
			t.Errorf("profile %q missing after round-trip", name)
			continue
		}
		if got.APIEndpoint != orig.APIEndpoint {
			t.Errorf("profile %q APIEndpoint = %q, want %q", name, got.APIEndpoint, orig.APIEndpoint)
		}
		if got.AuthMethod != orig.AuthMethod {
			t.Errorf("profile %q AuthMethod = %q, want %q", name, got.AuthMethod, orig.AuthMethod)
		}
		if got.APIKey != orig.APIKey {
			t.Errorf("profile %q APIKey = %q, want %q", name, got.APIKey, orig.APIKey)
		}
	}
}

// Loading from a path that doesn't exist should return an error (not panic).
func TestLoad_MissingFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent", "config.json"))

	_, err := Load()
	if err == nil {
		t.Fatal("expected error loading nonexistent file, got nil")
	}
}
