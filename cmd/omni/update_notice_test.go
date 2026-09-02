package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
)

type fakeAutomaticChecker struct {
	result   updatecheck.Result
	err      error
	claim    bool
	claimed  string
	checks   int
	claimAsk int
}

func (f *fakeAutomaticChecker) CheckAutomatic(context.Context, string) (updatecheck.Result, error) {
	f.checks++
	return f.result, f.err
}

func (f *fakeAutomaticChecker) ClaimNotification(r updatecheck.Result) bool {
	f.claimAsk++
	if !f.claim {
		return false
	}
	f.claimed = r.LatestVersion
	return true
}

func getenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// resolveCommand runs args through a root shaped like the real one and returns
// the command cobra resolved, which is what the update hook is handed.
func resolveCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "omni", SilenceUsage: true, SilenceErrors: true}
	addGlobalFlags(root)
	root.SetGlobalNormalizationFunc(openapi.NormalizeFlagName)

	models := &cobra.Command{Use: "models"}
	models.AddCommand(&cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(models)
	root.AddCommand(&cobra.Command{Use: "agent-help", RunE: func(*cobra.Command, []string) error { return nil }})
	addUpdateCommand(root, &fakeReleaseChecker{}, "v1.2.0")

	var resolved *cobra.Command
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		resolved = cmd
		return nil
	}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("executing %v: %v", args, err)
	}
	return resolved
}

func TestAutomaticUpdatesAllowed(t *testing.T) {
	tests := []struct {
		name    string
		version string
		args    []string
		human   bool
		env     map[string]string
		want    bool
	}{
		{"interactive release", "v1.2.0", []string{"models", "list"}, true, nil, true},
		{"development build", "dev", []string{"models", "list"}, true, nil, false},
		{"redirected", "v1.2.0", []string{"models", "list"}, false, nil, false},
		{"CI", "v1.2.0", []string{"models", "list"}, true, map[string]string{"CI": "true"}, false},
		{"disabled", "v1.2.0", []string{"models", "list"}, true, map[string]string{"OMNI_NO_UPDATE_NOTIFIER": "1"}, false},
		{"explicit check", "v1.2.0", []string{"update", "check", "--format", "human"}, true, nil, false},
		{"help command", "v1.2.0", []string{"help", "models"}, true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := resolveCommand(t, tt.args...)
			if got := automaticUpdatesAllowed(tt.version, cmd, tt.human, getenv(tt.env)); got != tt.want {
				t.Fatalf("automaticUpdatesAllowed() = %v, want %v", got, tt.want)
			}
		})
	}

	if automaticUpdatesAllowed("v1.2.0", nil, true, getenv(nil)) {
		t.Fatal("an unresolved command should never start a check")
	}
}

// TestInteractiveHumanOutputHonoursEveryFormatSpelling covers the flag forms a
// raw os.Args scan used to miss: -ojson and the normalized --FORMAT.
func TestInteractiveHumanOutputHonoursEveryFormatSpelling(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "auto")
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"models", "list"}, true},
		{[]string{"models", "list", "--format", "human"}, true},
		{[]string{"models", "list", "--format", "json"}, false},
		{[]string{"models", "list", "--format=json"}, false},
		{[]string{"models", "list", "-o", "json"}, false},
		{[]string{"models", "list", "-ojson"}, false},
		{[]string{"models", "list", "--FORMAT", "json"}, false},
		{[]string{"models", "list", "--Format=json"}, false},
	}
	for _, tt := range tests {
		cmd := resolveCommand(t, tt.args...)
		if got := interactiveHumanOutput(cmd, true); got != tt.want {
			t.Errorf("interactiveHumanOutput(%v) = %v, want %v", tt.args, got, tt.want)
		}
		if interactiveHumanOutput(cmd, false) {
			t.Errorf("interactiveHumanOutput(%v, notTTY) = true", tt.args)
		}
	}
}

// TestUpdateCommandNeverStartsAnAutomaticCheck pins the case where the format
// flag sits between the command words: `omni update --format human check`.
func TestUpdateCommandNeverStartsAnAutomaticCheck(t *testing.T) {
	t.Setenv("OMNI_OUTPUT_FORMAT", "auto")
	for _, args := range [][]string{
		{"update", "check"},
		{"update", "--format", "human", "check"},
		{"update", "check", "--format", "human"},
	} {
		cmd := resolveCommand(t, args...)
		if automaticUpdatesAllowed("v1.2.0", cmd, true, getenv(nil)) {
			t.Errorf("automaticUpdatesAllowed(%v) = true", args)
		}
	}
}

func TestAutomaticUpdateNotice(t *testing.T) {
	checker := &fakeAutomaticChecker{claim: true}
	result := updatecheck.Result{
		UpdateAvailable: true,
		LatestVersion:   "v1.3.0",
		ReleaseURL:      "https://example.test/v1.3.0",
		Upgrade:         updatecheck.UpgradeInstructions{Homebrew: "brew upgrade omni", Other: "install command"},
	}
	ch := make(chan updateOutcome, 1)
	ch <- updateOutcome{result: result}
	u := automaticUpdate{
		cancel: func() {}, result: ch, checker: checker, enabled: true,
		version: "v1.2.0", now: time.Now,
	}
	var stderr bytes.Buffer
	u.finish(true, &stderr)
	if got := stderr.String(); !strings.Contains(got, "v1.2.0 -> v1.3.0") || !strings.Contains(got, "run: install command") {
		t.Fatalf("stderr = %q", got)
	}
	if checker.claimed != "v1.3.0" {
		t.Fatalf("claimed = %q", checker.claimed)
	}
}

// A notice is printed only by the process that won the claim.
func TestAutomaticUpdateNoticeRequiresAClaim(t *testing.T) {
	checker := &fakeAutomaticChecker{claim: false}
	ch := make(chan updateOutcome, 1)
	ch <- updateOutcome{result: updatecheck.Result{UpdateAvailable: true, LatestVersion: "v1.3.0"}}
	u := automaticUpdate{cancel: func() {}, result: ch, checker: checker, enabled: true, version: "v1.2.0", now: time.Now}
	var stderr bytes.Buffer
	u.finish(true, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAutomaticUpdateIsSilentOnFailure(t *testing.T) {
	checker := &fakeAutomaticChecker{claim: true}
	cases := []struct {
		name    string
		outcome updateOutcome
		success bool
	}{
		{"failed command", updateOutcome{result: updatecheck.Result{UpdateAvailable: true, LatestVersion: "v1.3.0"}}, false},
		{"check error", updateOutcome{err: errors.New("offline")}, true},
		{"already current", updateOutcome{result: updatecheck.Result{UpdateAvailable: false}}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan updateOutcome, 1)
			ch <- tt.outcome
			u := automaticUpdate{cancel: func() {}, result: ch, checker: checker, enabled: true, version: "v1.2.0", now: time.Now}
			var stderr bytes.Buffer
			u.finish(tt.success, &stderr)
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if checker.claimed != "" {
				t.Fatalf("claimed = %q, want no claim", checker.claimed)
			}
		})
	}
}

func TestRecentHomebrewReleaseIsSuppressed(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	checker := &fakeAutomaticChecker{claim: true}
	ch := make(chan updateOutcome, 1)
	ch <- updateOutcome{result: updatecheck.Result{
		UpdateAvailable: true, LatestVersion: "v1.3.0",
		PublishedAt: now.Add(-time.Hour),
	}}
	u := automaticUpdate{
		cancel: func() {}, result: ch, checker: checker, enabled: true,
		version: "v1.2.0", homebrew: true, now: func() time.Time { return now },
	}
	var stderr bytes.Buffer
	u.finish(true, &stderr)
	if stderr.Len() != 0 || checker.claimed != "" {
		t.Fatalf("recent Homebrew release was announced: %q", stderr.String())
	}
}

func TestUpgradeHint(t *testing.T) {
	unix := updatecheck.UpgradeInstructions{Homebrew: "brew upgrade omni", Other: "curl ... | sh"}
	if got := upgradeHint(unix, true); got != "run: brew upgrade omni" {
		t.Errorf("homebrew hint = %q", got)
	}
	if got := upgradeHint(unix, false); got != "run: curl ... | sh" {
		t.Errorf("unix hint = %q", got)
	}
	// Windows has no Homebrew and no install.sh, so the advice is prose.
	windows := updatecheck.UpgradeInstructions{Other: "download the latest release from https://example.test/latest"}
	if got := upgradeHint(windows, false); got != windows.Other {
		t.Errorf("windows hint = %q", got)
	}
	if got := upgradeHint(windows, true); got != windows.Other {
		t.Errorf("windows homebrew hint = %q", got)
	}
}
