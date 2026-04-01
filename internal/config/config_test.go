package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearEnv unsets all Omni env vars so tests start from a clean slate.
// t.Setenv snapshots the original value and restores it when the test ends,
// so other tests and the user's shell aren't affected.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OMNI_API_TOKEN",
		"OMNI_API_KEY",
		"OMNI_BASE_URL",
		"OMNI_CONFIG_PATH",
		"OMNI_CONFIG_DIR",
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
	rc, err := Resolve("", "flag-token", "https://flag.omniapp.co", false)
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

	rc, err := Resolve("", "", "", false)
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

// The CLI supports two env vars for the API token: OMNI_API_TOKEN (preferred)
// and OMNI_API_KEY (fallback for backwards compat). This test verifies the
// fallback works, and that OMNI_API_TOKEN takes priority when both are set.
func TestResolve_APIKeyFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	// Only OMNI_API_KEY set (no OMNI_API_TOKEN)
	t.Setenv("OMNI_API_KEY", "apikey-token")
	t.Setenv("OMNI_BASE_URL", "https://test.omniapp.co")

	rc, err := Resolve("", "", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Token != "apikey-token" {
		t.Errorf("Token = %q, want %q (OMNI_API_KEY fallback)", rc.Token, "apikey-token")
	}

	// Now also set OMNI_API_TOKEN — it should take precedence
	t.Setenv("OMNI_API_TOKEN", "token-wins")
	rc, err = Resolve("", "", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Token != "token-wins" {
		t.Errorf("Token = %q, want %q (OMNI_API_TOKEN should beat OMNI_API_KEY)", rc.Token, "token-wins")
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

	rc, err := Resolve("", "", "", false)
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
	rc, err := Resolve("", "", "", false)
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

	_, err := Resolve("", "", "", false)
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

	_, err := Resolve("", "", "", false)
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
// unrecognized domains, unless --insecure is explicitly set.

func TestResolve_RejectsHTTP(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OMNI_API_TOKEN", "some-token")
	t.Setenv("OMNI_BASE_URL", "http://myorg.omniapp.co")

	_, err := Resolve("", "", "", false)
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

	_, err := Resolve("", "", "", false)
	if err == nil {
		t.Fatal("expected error for unrecognized domain, got nil")
	}
	if !strings.Contains(err.Error(), "not a recognized Omni domain") {
		t.Errorf("error = %q, want it to mention recognized domain", err.Error())
	}
}

func TestResolve_InsecureBypassesChecks(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OMNI_API_TOKEN", "some-token")
	t.Setenv("OMNI_BASE_URL", "http://localhost:3000")

	rc, err := Resolve("", "", "", true)
	if err != nil {
		t.Fatalf("expected --insecure to bypass checks, got: %v", err)
	}
	if rc.BaseURL != "http://localhost:3000" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "http://localhost:3000")
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

func TestMigrateConfig(t *testing.T) {
	clearEnv(t)

	// Set up a "new" config dir that doesn't have a config yet
	newDir := filepath.Join(t.TempDir(), "new")
	t.Setenv("OMNI_CONFIG_DIR", newDir)

	// Write a config at the legacy os.UserConfigDir() location
	legacyDir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("os.UserConfigDir() not available")
	}
	// Use a temp dir to simulate the legacy path without touching real config
	tmpLegacy := t.TempDir()
	legacyPath := filepath.Join(tmpLegacy, "omni-cli", "config.json")
	os.MkdirAll(filepath.Dir(legacyPath), 0o700)
	testData := []byte(`{"version":1,"profiles":{}}`)
	os.WriteFile(legacyPath, testData, 0o600)

	// We can't easily override os.UserConfigDir(), so test the migration logic directly:
	// Verify that if new path doesn't exist and legacy does, the file gets copied.
	// We'll test via the exported function by temporarily pointing OMNI_CONFIG_PATH.
	_ = legacyDir // used above for reference

	newPath := filepath.Join(newDir, "config.json")

	// Directly test: new path shouldn't exist yet
	if _, err := os.Stat(newPath); err == nil {
		t.Fatal("new config path should not exist yet")
	}

	// Call MigrateConfig — since OMNI_CONFIG_DIR points to newDir,
	// and no config exists there, it should try the legacy path.
	// But os.UserConfigDir() returns the real system path, not our tmpLegacy.
	// So we test the scenario where new path already exists (no-op).
	os.MkdirAll(filepath.Dir(newPath), 0o700)
	os.WriteFile(newPath, testData, 0o600)
	MigrateConfig() // should be a no-op since new path exists
	data, _ := os.ReadFile(newPath)
	if string(data) != string(testData) {
		t.Error("MigrateConfig modified existing config file")
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
