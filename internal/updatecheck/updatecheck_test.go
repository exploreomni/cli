package updatecheck

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func releaseResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestCheckFetchesAndCachesLatestRelease(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	requests := 0
	checker := &Checker{
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if got := req.Header.Get("User-Agent"); got != "omni-cli/v1.2.0" {
				t.Errorf("User-Agent = %q", got)
			}
			return releaseResponse(200, `{"tag_name":"v1.3.0","html_url":"https://example.test/v1.3.0","published_at":"2026-08-30T12:00:00Z"}`), nil
		})},
		Endpoint:  "https://example.test/latest",
		StatePath: filepath.Join(t.TempDir(), "update.json"),
		Now:       func() time.Time { return now },
	}

	got, err := checker.Check(context.Background(), "v1.2.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdateAvailable || got.LatestVersion != "v1.3.0" || got.Upgrade.Homebrew != "brew upgrade omni" {
		t.Fatalf("result = %+v", got)
	}

	now = now.Add(time.Hour)
	got, err = checker.Check(context.Background(), "v1.2.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want cached result after one request", requests)
	}
	if got.LatestVersion != "v1.3.0" {
		t.Fatalf("cached result = %+v", got)
	}

	if _, err := checker.Check(context.Background(), "v1.2.0", true); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d after forced check, want 2", requests)
	}
}

func TestNotificationThrottle(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	checker := &Checker{StatePath: filepath.Join(t.TempDir(), "update.json"), Now: func() time.Time { return now }}
	r := result("v1.2.0", Release{Version: "v1.3.0", URL: "https://example.test/release"})
	if !checker.NotificationDue(r) {
		t.Fatal("new release should be due before its first notification")
	}
	if err := checker.MarkNotified(r.LatestVersion); err != nil {
		t.Fatal(err)
	}
	if checker.NotificationDue(r) {
		t.Fatal("notification should be suppressed for 24 hours")
	}
	now = now.Add(25 * time.Hour)
	if !checker.NotificationDue(r) {
		t.Fatal("notification should be due again after 24 hours")
	}
}

func TestVersionComparison(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.2", 1},
		{"1.2.3", "v1.2.3", 0},
		{"v2.0.0", "v10.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-beta.2", "v1.0.0-beta.11", -1},
		{"v1.0.0+build.2", "v1.0.0+build.1", 0},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	if IsReleaseVersion("dev") || IsReleaseVersion("v1.2") || !IsReleaseVersion("v1.2.3") {
		t.Fatal("release version validation returned unexpected results")
	}
}

func TestCheckRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name string
		resp *http.Response
	}{
		{"http status", releaseResponse(500, `{}`)},
		{"malformed json", releaseResponse(200, `{`)},
		{"invalid release", releaseResponse(200, `{"tag_name":"latest"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{
				Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return tt.resp, nil
				})},
				Endpoint:  "https://example.test/latest",
				StatePath: filepath.Join(t.TempDir(), "update.json"),
			}
			if _, err := checker.Check(context.Background(), "v1.2.0", true); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
