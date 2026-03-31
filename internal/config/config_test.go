package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv unsets all config-related env vars via t.Setenv (auto-restored).
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OMNI_API_TOKEN",
		"OMNI_API_KEY",
		"OMNI_ORG_ID",
		"OMNI_BASE_URL",
		"OMNI_CONFIG_PATH",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

// writeConfig writes a config file to a temp dir and sets OMNI_CONFIG_PATH.
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

func TestResolve_FlagsOverrideAll(t *testing.T) {
	clearEnv(t)

	// Write a config file with values
	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIKey:         "file-token",
				APIEndpoint:    "https://file.example.com",
				OrganizationID: "file-org",
			},
		},
	})

	// Set env vars with different values
	t.Setenv("OMNI_API_TOKEN", "env-token")
	t.Setenv("OMNI_BASE_URL", "https://env.example.com")
	t.Setenv("OMNI_ORG_ID", "env-org")

	// Pass flags that should win
	rc, err := Resolve("", "flag-token", "flag-org", "https://flag.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "flag-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "flag-token")
	}
	if rc.BaseURL != "https://flag.example.com" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://flag.example.com")
	}
	if rc.OrgID != "flag-org" {
		t.Errorf("OrgID = %q, want %q", rc.OrgID, "flag-org")
	}
}

func TestResolve_EnvOverridesFile(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {
				APIKey:         "file-token",
				APIEndpoint:    "https://file.example.com",
				OrganizationID: "file-org",
			},
		},
	})

	t.Setenv("OMNI_API_TOKEN", "env-token")
	t.Setenv("OMNI_BASE_URL", "https://env.example.com")
	t.Setenv("OMNI_ORG_ID", "env-org")

	rc, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "env-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "env-token")
	}
	if rc.BaseURL != "https://env.example.com" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://env.example.com")
	}
	if rc.OrgID != "env-org" {
		t.Errorf("OrgID = %q, want %q", rc.OrgID, "env-org")
	}
}

func TestResolve_APIKeyFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	// Only OMNI_API_KEY set (no OMNI_API_TOKEN)
	t.Setenv("OMNI_API_KEY", "apikey-token")
	t.Setenv("OMNI_BASE_URL", "https://example.com")

	rc, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Token != "apikey-token" {
		t.Errorf("Token = %q, want %q (OMNI_API_KEY fallback)", rc.Token, "apikey-token")
	}

	// Now also set OMNI_API_TOKEN — it should take precedence
	t.Setenv("OMNI_API_TOKEN", "token-wins")
	rc, err = Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Token != "token-wins" {
		t.Errorf("Token = %q, want %q (OMNI_API_TOKEN should beat OMNI_API_KEY)", rc.Token, "token-wins")
	}
}

func TestResolve_FileValues(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]Profile{
			"prod": {
				APIKey:         "file-token",
				APIEndpoint:    "https://file.example.com",
				OrganizationID: "file-org",
			},
		},
	})

	rc, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "file-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "file-token")
	}
	if rc.BaseURL != "https://file.example.com" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://file.example.com")
	}
	if rc.OrgID != "file-org" {
		t.Errorf("OrgID = %q, want %q", rc.OrgID, "file-org")
	}
}

func TestResolve_DefaultProfile(t *testing.T) {
	clearEnv(t)

	writeConfig(t, &Config{
		Version:        1,
		DefaultProfile: "second",
		Profiles: map[string]Profile{
			"first": {
				APIKey:         "first-token",
				APIEndpoint:    "https://first.example.com",
				OrganizationID: "first-org",
			},
			"second": {
				APIKey:         "second-token",
				APIEndpoint:    "https://second.example.com",
				OrganizationID: "second-org",
			},
		},
	})

	// No profileName arg — should use DefaultProfile ("second")
	rc, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Token != "second-token" {
		t.Errorf("Token = %q, want %q", rc.Token, "second-token")
	}
	if rc.BaseURL != "https://second.example.com" {
		t.Errorf("BaseURL = %q, want %q", rc.BaseURL, "https://second.example.com")
	}
	if rc.OrgID != "second-org" {
		t.Errorf("OrgID = %q, want %q", rc.OrgID, "second-org")
	}
}

func TestResolve_MissingToken(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	_, err := Resolve("", "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no API token") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no API token")
	}
}

func TestResolve_MissingBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	// Token set, but no base URL
	t.Setenv("OMNI_API_TOKEN", "some-token")

	_, err := Resolve("", "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no API base URL") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no API base URL")
	}
}

// --- Config path ---

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
}

// --- Load/Save ---

func TestLoadSaveRoundTrip(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OMNI_CONFIG_PATH", path)

	original := &Config{
		Version:        1,
		DefaultProfile: "myprofile",
		Profiles: map[string]Profile{
			"myprofile": {
				OrganizationID:      "org-123",
				OrganizationShortID: "org",
				APIEndpoint:         "https://api.example.com",
				AuthMethod:          "apiKey",
				APIKey:              "secret-key",
			},
			"other": {
				OrganizationID: "org-456",
				APIEndpoint:    "https://other.example.com",
				APIKey:         "other-key",
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
		if got.OrganizationID != orig.OrganizationID {
			t.Errorf("profile %q OrganizationID = %q, want %q", name, got.OrganizationID, orig.OrganizationID)
		}
		if got.OrganizationShortID != orig.OrganizationShortID {
			t.Errorf("profile %q OrganizationShortID = %q, want %q", name, got.OrganizationShortID, orig.OrganizationShortID)
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

func TestLoad_MissingFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("OMNI_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent", "config.json"))

	_, err := Load()
	if err == nil {
		t.Fatal("expected error loading nonexistent file, got nil")
	}
}
