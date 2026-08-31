package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/updatecheck"
)

type fakeAutomaticChecker struct {
	result          updatecheck.Result
	err             error
	notificationDue bool
	marked          string
}

func (f *fakeAutomaticChecker) Check(context.Context, string, bool) (updatecheck.Result, error) {
	return f.result, f.err
}
func (f *fakeAutomaticChecker) NotificationDue(updatecheck.Result) bool { return f.notificationDue }
func (f *fakeAutomaticChecker) MarkNotified(version string) error {
	f.marked = version
	return nil
}

func getenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
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
		{"bare help", "v1.2.0", nil, true, nil, false},
		{"redirected", "v1.2.0", []string{"models", "list"}, false, nil, false},
		{"JSON output", "v1.2.0", []string{"models", "list", "--format", "json"}, false, nil, false},
		{"CI", "v1.2.0", []string{"models", "list"}, true, map[string]string{"CI": "true"}, false},
		{"disabled", "v1.2.0", []string{"models", "list"}, true, map[string]string{"OMNI_NO_UPDATE_NOTIFIER": "1"}, false},
		{"help", "v1.2.0", []string{"models", "--help"}, true, nil, false},
		{"version", "v1.2.0", []string{"--version"}, true, nil, false},
		{"explicit check", "v1.2.0", []string{"--format", "human", "update", "check"}, true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := automaticUpdatesAllowed(tt.version, tt.args, tt.human, getenv(tt.env)); got != tt.want {
				t.Fatalf("automaticUpdatesAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestedOutputFormat(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"models", "list", "--format", "json"}, "json"},
		{[]string{"--format=human", "models", "list"}, "human"},
		{[]string{"models", "list", "-o", "auto"}, "auto"},
	} {
		if got := requestedOutputFormat(tt.args); got != tt.want {
			t.Errorf("requestedOutputFormat(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestAutomaticUpdateNotice(t *testing.T) {
	checker := &fakeAutomaticChecker{notificationDue: true}
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
	if got := stderr.String(); !strings.Contains(got, "v1.2.0 -> v1.3.0") || !strings.Contains(got, "install command") {
		t.Fatalf("stderr = %q", got)
	}
	if checker.marked != "v1.3.0" {
		t.Fatalf("marked = %q", checker.marked)
	}
}

func TestAutomaticUpdateIsSilentOnFailure(t *testing.T) {
	checker := &fakeAutomaticChecker{notificationDue: true}
	for _, outcome := range []updateOutcome{{err: errors.New("offline")}, {result: updatecheck.Result{UpdateAvailable: true}}} {
		ch := make(chan updateOutcome, 1)
		ch <- outcome
		u := automaticUpdate{cancel: func() {}, result: ch, checker: checker, enabled: true, version: "v1.2.0", now: time.Now}
		var stderr bytes.Buffer
		u.finish(false, &stderr)
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q", stderr.String())
		}
	}
}

func TestRecentHomebrewReleaseIsSuppressed(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	checker := &fakeAutomaticChecker{notificationDue: true}
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
	if stderr.Len() != 0 || checker.marked != "" {
		t.Fatalf("recent Homebrew release was announced: %q", stderr.String())
	}
}
