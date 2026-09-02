package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// clock is a test clock that is safe to read and advance from several
// goroutines, which the concurrency tests below need.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

const releaseBody = `{"tag_name":"v1.3.0","html_url":"https://example.test/v1.3.0","published_at":"2026-08-30T12:00:00Z"}`

func newTestChecker(t *testing.T, statePath string, clk *clock, rt roundTripFunc) *Checker {
	t.Helper()
	return &Checker{
		Client:    &http.Client{Transport: rt},
		Endpoint:  "https://example.test/latest",
		StatePath: statePath,
		Now:       clk.Now,
	}
}

func TestCheckAlwaysFetchesAndCaches(t *testing.T) {
	clk := newClock()
	var requests int
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(req *http.Request) (*http.Response, error) {
		requests++
		if got := req.Header.Get("User-Agent"); got != "omni-cli/v1.2.0" {
			t.Errorf("User-Agent = %q", got)
		}
		return releaseResponse(200, releaseBody), nil
	})

	got, err := checker.Check(context.Background(), "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdateAvailable || got.LatestVersion != "v1.3.0" {
		t.Fatalf("result = %+v", got)
	}

	// An explicit check is never served from the cache.
	if _, err := checker.Check(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	// It does record the result, so the automatic path stays throttled.
	clk.advance(time.Hour)
	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want the automatic check to reuse the cached release", requests)
	}
}

func TestCheckSucceedsWhenTheCacheIsUnwritable(t *testing.T) {
	clk := newClock()
	// A path under a file (rather than a directory) can never be created.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	checker := newTestChecker(t, filepath.Join(blocked, "update.json"), clk, func(*http.Request) (*http.Response, error) {
		return releaseResponse(200, releaseBody), nil
	})
	got, err := checker.Check(context.Background(), "v1.2.0")
	if err != nil || got.LatestVersion != "v1.3.0" {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
}

func TestAutomaticCheckIsThrottled(t *testing.T) {
	clk := newClock()
	var requests int
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(*http.Request) (*http.Response, error) {
		requests++
		return releaseResponse(200, releaseBody), nil
	})

	for i := 0; i < 3; i++ {
		if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
			t.Fatal(err)
		}
		clk.advance(time.Hour)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one request within 24 hours", requests)
	}

	clk.advance(checkInterval)
	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want a second request after 24 hours", requests)
	}
}

func TestAutomaticCheckFastPathDoesNotCreateTheLock(t *testing.T) {
	clk := newClock()
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(*http.Request) (*http.Response, error) {
		t.Fatal("a throttled check must not make a request")
		return nil, nil
	})
	wantRelease := Release{Version: "v1.3.0", URL: "https://example.test/v1.3.0"}
	if err := checker.writeState(state{
		NextCheckAt:   clk.Now().Add(checkInterval),
		LatestRelease: wantRelease,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := checker.CheckAutomatic(context.Background(), "v1.2.0")
	if err != nil || got.LatestVersion != wantRelease.Version {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
	if _, err := os.Stat(checker.lockPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("throttled fast path touched the lock file: %v", err)
	}
}

func TestAutomaticCheckIsDisabledWithoutAStatePath(t *testing.T) {
	clk := newClock()
	var requests int
	checker := newTestChecker(t, "", clk, func(*http.Request) (*http.Response, error) {
		requests++
		return releaseResponse(200, releaseBody), nil
	})

	got, err := checker.CheckAutomatic(context.Background(), "v1.2.0")
	if err != nil || got.UpdateAvailable || requests != 0 {
		t.Fatalf("result = %+v, err = %v, requests = %d", got, err, requests)
	}
}

func TestConcurrentAutomaticChecksMakeOneRequest(t *testing.T) {
	clk := newClock()
	statePath := filepath.Join(t.TempDir(), "update.json")
	var requests atomic.Int64
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		started <- struct{}{}
		<-release
		return releaseResponse(200, releaseBody), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		checker := newTestChecker(t, statePath, clk, transport)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
				t.Error(err)
			}
		}()
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no check was started")
	}
	close(release)
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want exactly one outbound check", got)
	}
}

func TestFailedCheckDoesNotRetryImmediately(t *testing.T) {
	clk := newClock()
	var requests int
	fail := true
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(*http.Request) (*http.Response, error) {
		requests++
		if fail {
			return nil, errors.New("offline")
		}
		return releaseResponse(200, releaseBody), nil
	})

	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err == nil {
		t.Fatal("expected the first check to fail")
	}
	for i := 0; i < 3; i++ {
		_, _ = checker.CheckAutomatic(context.Background(), "v1.2.0")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want the failed attempt to back off", requests)
	}

	fail = false
	clk.advance(failureBackoff + time.Minute)
	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want a retry after the backoff", requests)
	}
}

func TestCancelledCheckDoesNotRetryImmediately(t *testing.T) {
	clk := newClock()
	var requests int
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(req *http.Request) (*http.Response, error) {
		requests++
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	// The command finished before the check did, exactly as the CLI does.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := checker.CheckAutomatic(ctx, "v1.2.0"); err == nil {
		t.Fatal("expected a cancelled check to fail")
	}
	got, err := checker.CheckAutomatic(context.Background(), "v1.2.0")
	if err != nil || got.UpdateAvailable {
		t.Fatalf("throttled check = %+v, err = %v", got, err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want the cancelled attempt to back off", requests)
	}
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	clk := newClock()
	var requests int
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, func(*http.Request) (*http.Response, error) {
		requests++
		return releaseResponse(200, releaseBody), nil
	})

	// A process that died holding the lease: the lease is never released and
	// only the short backoff was written.
	lease, ok := checker.claimCheck()
	if !ok || lease == "" {
		t.Fatal("expected the first claim to succeed")
	}
	if _, claimed := checker.claimCheck(); claimed {
		t.Fatal("a live lease should not be reclaimed")
	}

	clk.advance(failureBackoff + leaseDuration)
	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want the abandoned lease to be reclaimed", requests)
	}
	s, err := checker.readState()
	if err != nil {
		t.Fatal(err)
	}
	if s.LeaseID != "" || s.LatestRelease.Version != "v1.3.0" {
		t.Fatalf("state = %+v", s)
	}
}

func TestStaleLeaseOwnerCannotOverwriteNewerState(t *testing.T) {
	clk := newClock()
	statePath := filepath.Join(t.TempDir(), "update.json")
	checker := newTestChecker(t, statePath, clk, nil)

	stale, ok := checker.claimCheck()
	if !ok {
		t.Fatal("expected the first claim to succeed")
	}

	// The owner stalls past its lease; a second process claims and records a
	// newer release.
	clk.advance(failureBackoff + leaseDuration)
	fresh, ok := checker.claimCheck()
	if !ok {
		t.Fatal("expected the expired lease to be reclaimed")
	}
	checker.finishCheck(fresh, Release{Version: "v1.4.0", URL: "https://example.test/v1.4.0"}, nil)

	// The stale owner finally returns with an older answer.
	checker.finishCheck(stale, Release{Version: "v1.3.0", URL: "https://example.test/v1.3.0"}, nil)

	s, err := checker.readState()
	if err != nil {
		t.Fatal(err)
	}
	if s.LatestRelease.Version != "v1.4.0" {
		t.Fatalf("stale owner overwrote the state: %+v", s)
	}
	if !s.NextCheckAt.Equal(clk.Now().Add(checkInterval)) {
		t.Fatalf("NextCheckAt = %v", s.NextCheckAt)
	}
}

func TestConcurrentNotificationClaimsProduceOneNotice(t *testing.T) {
	clk := newClock()
	statePath := filepath.Join(t.TempDir(), "update.json")
	r := result("v1.2.0", Release{Version: "v1.3.0", URL: "https://example.test/release"})

	var claims atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		checker := newTestChecker(t, statePath, clk, nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if checker.ClaimNotification(r) {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := claims.Load(); got != 1 {
		t.Fatalf("claims = %d, want exactly one notice", got)
	}
}

func TestNotificationThrottle(t *testing.T) {
	clk := newClock()
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, nil)
	r := result("v1.2.0", Release{Version: "v1.3.0", URL: "https://example.test/release"})
	if !checker.ClaimNotification(r) {
		t.Fatal("a new release should be claimable before its first notification")
	}
	if checker.ClaimNotification(r) {
		t.Fatal("notification should be suppressed for 24 hours")
	}
	clk.advance(25 * time.Hour)
	if !checker.ClaimNotification(r) {
		t.Fatal("notification should be due again after 24 hours")
	}
	if checker.ClaimNotification(result("v1.3.0", Release{Version: "v1.3.0"})) {
		t.Fatal("an up-to-date result should never be claimed")
	}
}

func TestNotificationFastPathDoesNotCreateTheLock(t *testing.T) {
	clk := newClock()
	checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), clk, nil)
	r := result("v1.2.0", Release{Version: "v1.3.0", URL: "https://example.test/release"})
	if err := checker.writeState(state{
		NotifiedVersion: r.LatestVersion,
		NotifiedAt:      clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if checker.ClaimNotification(r) {
		t.Fatal("a recently claimed notification should stay suppressed")
	}
	if _, err := os.Stat(checker.lockPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("notification fast path touched the lock file: %v", err)
	}
}

func TestAutomaticCheckFallsBackToTheCachedRelease(t *testing.T) {
	clk := newClock()
	statePath := filepath.Join(t.TempDir(), "update.json")
	offline := false
	checker := newTestChecker(t, statePath, clk, func(*http.Request) (*http.Response, error) {
		if offline {
			return nil, errors.New("offline")
		}
		return releaseResponse(200, releaseBody), nil
	})
	if _, err := checker.CheckAutomatic(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}

	offline = true
	clk.advance(checkInterval + time.Hour)
	got, err := checker.CheckAutomatic(context.Background(), "v1.2.0")
	if err != nil || got.LatestVersion != "v1.3.0" {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
}

func TestLockIsSeparateFromTheStateFile(t *testing.T) {
	clk := newClock()
	statePath := filepath.Join(t.TempDir(), "update.json")
	checker := newTestChecker(t, statePath, clk, nil)
	if got := checker.lockPath(); got != filepath.Join(filepath.Dir(statePath), "update.lock") {
		t.Fatalf("lockPath() = %q", got)
	}

	unlock, ok := checker.tryLock()
	if !ok {
		t.Fatal("expected to take the lock")
	}
	contender := newTestChecker(t, statePath, clk, nil)
	if _, ok := contender.tryLock(); ok {
		t.Fatal("the lock should not be handed out twice")
	}
	unlock()
	before, err := os.Stat(checker.lockPath())
	if err != nil {
		t.Fatalf("the stable lock file was removed on unlock: %v", err)
	}

	unlock2, ok := contender.tryLock()
	if !ok {
		t.Fatal("expected the released lock to be available")
	}
	after, err := os.Stat(checker.lockPath())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock acquisition replaced the stable lock file: %v", err)
	}
	unlock2()
}

func TestLockIsReleasedWhenOwnerExits(t *testing.T) {
	const childStatePathEnv = "OMNI_UPDATECHECK_TEST_LOCK_CHILD_STATE"
	if statePath := os.Getenv(childStatePathEnv); statePath != "" {
		checker := &Checker{StatePath: statePath}
		if _, ok := checker.tryLock(); !ok {
			os.Exit(2)
		}
		// Simulate abrupt process termination: do not call the unlock callback.
		os.Exit(0)
	}

	statePath := filepath.Join(t.TempDir(), "update.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockIsReleasedWhenOwnerExits$")
	cmd.Env = append(os.Environ(), childStatePathEnv+"="+statePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock-owner subprocess: %v\n%s", err, output)
	}

	checker := &Checker{StatePath: statePath}
	unlock, ok := checker.tryLock()
	if !ok {
		t.Fatal("the OS did not release the lock when its owner exited")
	}
	unlock()
}

func TestStatePathFromCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	if got, want := statePathFromCacheDir(cacheDir, nil), filepath.Join(cacheDir, "omni-cli", "update.json"); got != want {
		t.Fatalf("statePathFromCacheDir() = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		name string
		dir  string
		err  error
	}{
		{name: "empty", dir: ""},
		{name: "relative", dir: filepath.Join("relative", "cache")},
		{name: "error", dir: cacheDir, err: errors.New("cache unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := statePathFromCacheDir(tc.dir, tc.err); got != "" {
				t.Fatalf("statePathFromCacheDir() = %q, want disabled", got)
			}
		})
	}
}

func TestUpgradeInstructionsArePlatformSpecific(t *testing.T) {
	windows := upgradeInstructions("windows")
	if windows.Homebrew != "" || strings.Contains(windows.Other, "install.sh") {
		t.Fatalf("windows instructions = %+v, want no Homebrew and no install.sh", windows)
	}
	if !strings.Contains(windows.Other, releasesPage) {
		t.Fatalf("windows instructions = %+v, want the releases page", windows)
	}
	for _, goos := range []string{"darwin", "linux"} {
		got := upgradeInstructions(goos)
		if got.Homebrew != "brew upgrade omni" || got.Other != installCommand {
			t.Fatalf("%s instructions = %+v", goos, got)
		}
	}
	// The JSON contract keeps "other" even when Homebrew is unavailable.
	data, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "homebrew") || !strings.Contains(string(data), `"other"`) {
		t.Fatalf("windows JSON = %s", data)
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
			checker := newTestChecker(t, filepath.Join(t.TempDir(), "update.json"), newClock(), func(*http.Request) (*http.Response, error) {
				return tt.resp, nil
			})
			if _, err := checker.Check(context.Background(), "v1.2.0"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
