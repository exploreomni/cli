package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/spf13/cobra"
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

// --- config init (non-interactive flags) ---
//
// Note: there is deliberately no --api-key flag. Accepting a secret on the
// command line leaks it into shell history and process listings (and from
// there into anything that scrapes those, including agentic tooling). The key
// is always read from a hidden prompt instead; only the non-secret --name,
// --endpoint, and --auth values can be supplied non-interactively.

// --auth oauth must validate the endpoint BEFORE launching the browser flow,
// so a bad endpoint fails fast instead of opening a browser against it.
func TestConfigInit_OAuthRejectsInvalidEndpoint(t *testing.T) {
	withConfig(t, nil)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "")

	cmd := configInitCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"--name", "prod",
		"--endpoint", "http://insecure.omniapp.co",
		"--auth", "oauth",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected endpoint validation error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("error = %q, want HTTPS validation failure", err.Error())
	}
}

func TestConfigInit_InvalidAuthFlag(t *testing.T) {
	withConfig(t, nil)

	cmd := configInitCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"--name", "prod",
		"--endpoint", "https://myorg.omniapp.co",
		"--auth", "magic",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --auth value, got nil")
	}
	if !strings.Contains(err.Error(), `invalid --auth "magic"`) {
		t.Errorf("error = %q, want it to name the invalid value", err.Error())
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

// --- config use (the #45 quoting UX) ---

// The reported failure: `omni config use Playground Org-scoped` (unquoted) was
// split by the shell into two args. Instead of cobra's opaque
// "accepts 1 arg(s), received 2", we now hint at quoting and echo the likely
// intended name so the user can copy-paste the fix.
func TestConfigUse_MultiArgHint(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{"Playground Org-scoped": {APIEndpoint: "https://x.omniapp.co"}},
	})

	cmd := configUseCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"Playground", "Org-scoped"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an args error for unquoted multi-word name, got nil")
	}
	if !strings.Contains(err.Error(), "quote it") {
		t.Errorf("error = %q, want a quoting hint", err.Error())
	}
	// The hint echoes the reconstructed name, quoted, so it's directly usable.
	if !strings.Contains(err.Error(), `"Playground Org-scoped"`) {
		t.Errorf("error = %q, want it to show the quoted name", err.Error())
	}
}

// The counterpart: a correctly quoted spaced name arrives as one arg and
// switches profiles. Confirms we didn't break the legitimate path.
func TestConfigUse_QuotedSpacedNameWorks(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{"Playground Org-scoped": {APIEndpoint: "https://x.omniapp.co"}},
	})

	cmd := configUseCmd()
	cmd.SetArgs([]string{"Playground Org-scoped"})
	out := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, `Switched to profile "Playground Org-scoped"`) {
		t.Errorf("stdout = %q, want it to switch to the spaced profile", out)
	}

	cfg, _ := config.Load()
	if cfg.DefaultProfile != "Playground Org-scoped" {
		t.Errorf("DefaultProfile = %q, want the spaced name", cfg.DefaultProfile)
	}
}

// --- config list ---

func TestConfigList_MarksDefault(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod":    {APIEndpoint: "https://prod.omniapp.co", AuthMethod: "oauth"},
			"staging": {APIEndpoint: "https://staging.omniapp.co", AuthMethod: "api-key"},
		},
	})

	cmd := configListCmd()
	out := captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, "* prod") {
		t.Errorf("expected default profile to be marked with *, got:\n%s", out)
	}
	if !strings.Contains(out, "  staging") {
		t.Errorf("expected non-default profile to be unmarked, got:\n%s", out)
	}
}

// --- config rename ---

// The motivating bug (#45): a profile saved with spaces is unusable. Rename
// must let the user fix it without sudo-editing the config file.
func TestConfigRename_FixesLegacySpacedName(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "Playground Org-scoped",
		Profiles: map[string]config.Profile{
			"Playground Org-scoped": {
				APIEndpoint: "https://playground.omniapp.co",
				AuthMethod:  "api-key",
				APIKey:      "secret",
			},
		},
	})

	cmd := configRenameCmd()
	out := captureStdout(t, func() {
		if err := cmd.RunE(cmd, []string{"Playground Org-scoped", "playground"}); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, "Renamed") {
		t.Errorf("stdout = %q, want it to confirm rename", out)
	}

	cfg, _ := config.Load()
	if _, exists := cfg.Profiles["Playground Org-scoped"]; exists {
		t.Error("old profile name still present after rename")
	}
	p, ok := cfg.Profiles["playground"]
	if !ok {
		t.Fatal("new profile name missing after rename")
	}
	if p.APIKey != "secret" {
		t.Errorf("profile contents lost in rename: %+v", p)
	}
	if cfg.DefaultProfile != "playground" {
		t.Errorf("DefaultProfile = %q, want %q (default should follow the rename)", cfg.DefaultProfile, "playground")
	}
}

// Spaced names are allowed — the user just has to quote them when passing them
// as args. Renaming TO a spaced name must succeed (and works with rename's two
// positional args, which are unambiguous unlike a single-name command).
func TestConfigRename_AllowsSpacedName(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{"prod": {APIEndpoint: "https://x.omniapp.co"}},
	})

	cmd := configRenameCmd()
	if err := cmd.RunE(cmd, []string{"prod", "Prod (US)"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfg, _ := config.Load()
	if _, ok := cfg.Profiles["Prod (US)"]; !ok {
		t.Errorf("spaced profile name not created; profiles: %v", cfg.Profiles)
	}
}

func TestConfigRename_RefusesToOverwrite(t *testing.T) {
	withConfig(t, &config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"prod":    {APIEndpoint: "https://prod.omniapp.co"},
			"staging": {APIEndpoint: "https://staging.omniapp.co"},
		},
	})

	cmd := configRenameCmd()
	err := cmd.RunE(cmd, []string{"prod", "staging"})
	if err == nil {
		t.Fatal("expected error when renaming over an existing profile, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention name conflict", err.Error())
	}
}

func TestConfigRename_UnknownProfile(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{"prod": {APIEndpoint: "https://prod.omniapp.co"}},
	})

	cmd := configRenameCmd()
	err := cmd.RunE(cmd, []string{"nope", "newname"})
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	if !strings.Contains(err.Error(), `"nope" not found`) {
		t.Errorf("error = %q, want it to mention the missing profile", err.Error())
	}
}

// --- config delete ---

func TestConfigDelete_RemovesProfileWithYesFlag(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod":    {APIEndpoint: "https://prod.omniapp.co"},
			"staging": {APIEndpoint: "https://staging.omniapp.co"},
		},
	})

	cmd := configDeleteCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("setting --yes flag: %v", err)
	}
	out := captureStdout(t, func() {
		if err := cmd.RunE(cmd, []string{"prod"}); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, `Deleted profile "prod"`) {
		t.Errorf("stdout = %q, want it to confirm deletion", out)
	}

	cfg, _ := config.Load()
	if _, exists := cfg.Profiles["prod"]; exists {
		t.Error("prod profile still present after delete")
	}
	if _, exists := cfg.Profiles["staging"]; !exists {
		t.Error("staging profile incorrectly deleted")
	}
	if cfg.DefaultProfile != "" {
		t.Errorf("DefaultProfile = %q, want empty (deleted profile was the default)", cfg.DefaultProfile)
	}
}

func TestConfigDelete_UnknownProfile(t *testing.T) {
	withConfig(t, &config.Config{
		Version:  1,
		Profiles: map[string]config.Profile{"prod": {APIEndpoint: "https://prod.omniapp.co"}},
	})

	cmd := configDeleteCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("setting --yes flag: %v", err)
	}
	err := cmd.RunE(cmd, []string{"nope"})
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	if !strings.Contains(err.Error(), `"nope" not found`) {
		t.Errorf("error = %q, want it to mention the missing profile", err.Error())
	}
}

// Regression for #83: the global -p/--profile flag must be honoured when no
// positional profile is given, instead of silently acting on the default.
func TestConfigLogout_ProfileFlagOverridesDefault(t *testing.T) {
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "playground",
		Profiles: map[string]config.Profile{
			"playground": {AuthMethod: "oauth", AccessToken: "keep", RefreshToken: "keep"},
			"prod":       {AuthMethod: "oauth", AccessToken: "a", RefreshToken: "r"},
		},
	})

	cmd := configLogoutCmd()
	cmd.Flags().StringP("profile", "p", "", "")
	if err := cmd.Flags().Set("profile", "prod"); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	cfg, _ := config.Load()
	if cfg.Profiles["prod"].AccessToken != "" {
		t.Error("-p prod: prod tokens were not cleared")
	}
	if cfg.Profiles["playground"].AccessToken != "keep" {
		t.Error("-p prod: default profile was modified")
	}
}

func TestTargetProfileName(t *testing.T) {
	cfg := &config.Config{DefaultProfile: "playground"}
	newCmd := func(flag string) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().StringP("profile", "p", "", "")
		if flag != "" {
			_ = c.Flags().Set("profile", flag)
		}
		return c
	}
	tests := []struct {
		name string
		args []string
		flag string
		want string
	}{
		{"positional wins over flag", []string{"prod"}, "staging", "prod"},
		{"flag wins over default", nil, "prod", "prod"},
		{"default when neither", nil, "", "playground"},
	}
	for _, tc := range tests {
		got, err := targetProfileName(newCmd(tc.flag), tc.args, cfg)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if _, err := targetProfileName(newCmd(""), nil, &config.Config{}); err == nil {
		t.Error("expected error when no profile and no default")
	}
}
