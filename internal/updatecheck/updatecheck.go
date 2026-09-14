// Package updatecheck discovers newer Omni CLI releases without affecting the
// command the user actually asked to run.
package updatecheck

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/exploreomni/omni-cli/internal/useragent"
)

const (
	defaultEndpoint = "https://api.github.com/repos/exploreomni/cli/releases/latest"
	releasesPage    = "https://github.com/exploreomni/cli/releases/latest"
	installCommand  = "curl -fsSL https://raw.githubusercontent.com/exploreomni/cli/main/install.sh | sh"

	// checkInterval is the throttle between successful checks, failureBackoff
	// the much shorter one applied after a failed or abandoned attempt, and
	// leaseDuration the window in which the process that claimed a check is
	// expected to finish it (comfortably longer than the HTTP timeout).
	checkInterval  = 24 * time.Hour
	failureBackoff = 15 * time.Minute
	leaseDuration  = 30 * time.Second
)

// Release is the subset of a GitHub release used by the updater.
type Release struct {
	Version     string    `json:"tag_name"`
	URL         string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// UpgradeInstructions lists supported ways to install the newest release.
// Homebrew is empty on platforms Homebrew doesn't serve.
type UpgradeInstructions struct {
	Homebrew string `json:"homebrew,omitempty"`
	Other    string `json:"other"`
}

// Result is the stable, machine-readable result of an update check.
type Result struct {
	UpdateAvailable bool                `json:"updateAvailable"`
	CurrentVersion  string              `json:"currentVersion"`
	LatestVersion   string              `json:"latestVersion"`
	ReleaseURL      string              `json:"releaseUrl"`
	Upgrade         UpgradeInstructions `json:"upgrade"`
	PublishedAt     time.Time           `json:"-"`
}

// state is the durable scheduler behind the automatic check. NextCheckAt is the
// persistent throttle, and it is written *before* any networking, so a
// cancelled or killed process still leaves a delay behind.
// LeaseID/LeaseUntil name the process currently allowed to record a result:
// the ID stops a slow owner from clobbering state claimed by a newer one, and
// the deadline lets a crashed owner's claim be reclaimed.
type state struct {
	CheckedAt       time.Time `json:"checkedAt,omitempty"`
	NextCheckAt     time.Time `json:"nextCheckAt,omitempty"`
	LeaseID         string    `json:"leaseId,omitempty"`
	LeaseUntil      time.Time `json:"leaseUntil,omitempty"`
	LatestRelease   Release   `json:"latestRelease"`
	NotifiedVersion string    `json:"notifiedVersion,omitempty"`
	NotifiedAt      time.Time `json:"notifiedAt,omitempty"`
}

// Checker fetches releases and persists the throttling state.
type Checker struct {
	Client    *http.Client
	Endpoint  string
	StatePath string
	Now       func() time.Time
}

// New returns a checker suitable for CLI use.
func New() *Checker {
	return &Checker{
		Client:    &http.Client{Timeout: 2 * time.Second},
		Endpoint:  defaultEndpoint,
		StatePath: DefaultStatePath(),
		Now:       time.Now,
	}
}

// DefaultStatePath returns the per-user update-check state file, or an empty
// path when the platform cache directory is unavailable. An empty path disables
// automatic checks while leaving explicit checks usable.
func DefaultStatePath() string {
	dir, err := os.UserCacheDir()
	return statePathFromCacheDir(dir, err)
}

func statePathFromCacheDir(dir string, err error) string {
	if err != nil || dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Join(dir, "omni-cli", "update.json")
}

// Check performs an explicit, user-requested check. It always contacts the
// release endpoint and returns what it fetched; caching is best-effort, so a
// broken or read-only cache never turns a successful check into a failure.
func (c *Checker) Check(ctx context.Context, currentVersion string) (Result, error) {
	release, err := c.fetch(ctx)
	if err != nil {
		return Result{}, err
	}
	c.recordCheck(release)
	return result(currentVersion, release), nil
}

// CheckAutomatic is the background check run alongside an interactive command.
// It waits checkInterval after a successful request and failureBackoff after a
// failed one across every process sharing the state file. It falls back to the
// cached release whenever a check isn't due, the state is claimed elsewhere,
// or the request fails.
func (c *Checker) CheckAutomatic(ctx context.Context, currentVersion string) (Result, error) {
	if c.StatePath == "" {
		return result(currentVersion, Release{}), nil
	}
	cached, readErr := c.readState()
	if readErr == nil && automaticCheckDeferred(cached, c.now()) {
		return result(currentVersion, cached.LatestRelease), nil
	}
	lease, claimed := c.claimCheck()
	if !claimed {
		return result(currentVersion, cached.LatestRelease), nil
	}

	release, err := c.fetch(ctx)
	c.finishCheck(lease, release, err)
	if err != nil {
		if validVersion(cached.LatestRelease.Version) {
			// A stale but valid answer is still worth reporting; the throttle
			// claimCheck wrote keeps the failed attempt from retrying.
			return result(currentVersion, cached.LatestRelease), nil
		}
		return Result{}, err
	}
	return result(currentVersion, release), nil
}

// ClaimNotification atomically decides whether this process should print a
// notice for r, recording the claim before it returns true. Deciding and
// recording under one lock is what stops concurrent commands from each
// announcing the same release.
func (c *Checker) ClaimNotification(r Result) bool {
	if !r.UpdateAvailable || c.StatePath == "" {
		return false
	}
	cached, readErr := c.readState()
	now := c.now()
	if readErr == nil && notificationRecentlyClaimed(cached, r, now) {
		return false
	}
	unlock, ok := c.tryLock()
	if !ok {
		return false
	}
	defer unlock()

	s, _ := c.readState()
	now = c.now()
	if notificationRecentlyClaimed(s, r, now) {
		return false
	}
	s.NotifiedVersion = r.LatestVersion
	s.NotifiedAt = now
	return c.writeState(s) == nil
}

// claimCheck reports whether this process may perform the next check, taking
// the lease that makes it the only one allowed to record the outcome.
func (c *Checker) claimCheck() (string, bool) {
	unlock, ok := c.tryLock()
	if !ok {
		return "", false
	}
	defer unlock()

	s, _ := c.readState()
	now := c.now()
	if automaticCheckDeferred(s, now) {
		return "", false
	}
	id, err := newLeaseID()
	if err != nil {
		return "", false
	}
	s.LeaseID = id
	s.LeaseUntil = now.Add(leaseDuration)
	// Written before any networking: if this process is cancelled or killed
	// mid-request, the next invocation still waits out the short backoff
	// instead of retrying straight away.
	s.NextCheckAt = now.Add(failureBackoff)
	if err := c.writeState(s); err != nil {
		return "", false
	}
	return id, true
}

func automaticCheckDeferred(s state, now time.Time) bool {
	return (!s.NextCheckAt.IsZero() && now.Before(s.NextCheckAt)) ||
		(s.LeaseID != "" && now.Before(s.LeaseUntil))
}

func notificationRecentlyClaimed(s state, r Result, now time.Time) bool {
	return s.NotifiedVersion == r.LatestVersion &&
		!s.NotifiedAt.IsZero() && now.Sub(s.NotifiedAt) < checkInterval
}

// finishCheck records the outcome of the check claimed under lease. A failure
// leaves the short backoff claimCheck already wrote in place.
func (c *Checker) finishCheck(lease string, release Release, fetchErr error) {
	unlock, ok := c.tryLock()
	if !ok {
		return
	}
	defer unlock()

	s, err := c.readState()
	if err != nil || s.LeaseID != lease {
		// This lease expired and another process claimed the state; its result
		// is the newer one, so leave it alone.
		return
	}
	s.LeaseID = ""
	s.LeaseUntil = time.Time{}
	if fetchErr == nil {
		now := c.now()
		s.LatestRelease = release
		s.CheckedAt = now
		s.NextCheckAt = now.Add(checkInterval)
	}
	_ = c.writeState(s)
}

// recordCheck stores an explicitly fetched release, ignoring cache failures.
func (c *Checker) recordCheck(release Release) {
	unlock, ok := c.tryLock()
	if !ok {
		return
	}
	defer unlock()

	s, _ := c.readState()
	now := c.now()
	s.LatestRelease = release
	s.CheckedAt = now
	s.NextCheckAt = now.Add(checkInterval)
	_ = c.writeState(s)
}

// tryLock takes the update lock without blocking, returning false when another
// process holds it — the requested command must never wait on the updater.
//
// The lock is a stable, separate file: writeState replaces update.json with a
// rename, so locking update.json itself would let a second process lock the
// replacement while the first still held the old inode. The file itself is
// never removed; an OS-backed advisory lock provides ownership and is released
// automatically if its process exits.
func (c *Checker) tryLock() (func(), bool) {
	if c.StatePath == "" {
		return nil, false
	}
	if err := os.MkdirAll(filepath.Dir(c.StatePath), 0o700); err != nil {
		return nil, false
	}
	lock := flock.New(c.lockPath(), flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return nil, false
	}
	return func() { _ = lock.Unlock() }, true
}

func (c *Checker) lockPath() string {
	return strings.TrimSuffix(c.StatePath, ".json") + ".lock"
}

func newLeaseID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (c *Checker) fetch(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", useragent.String())

	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return Release{}, fmt.Errorf("checking latest release: unexpected HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("decoding latest release: %w", err)
	}
	if !validVersion(release.Version) || release.URL == "" {
		return Release{}, fmt.Errorf("latest release response is missing a valid version or URL")
	}
	return release, nil
}

func result(current string, release Release) Result {
	return Result{
		UpdateAvailable: validVersion(current) && compareVersions(release.Version, current) > 0,
		CurrentVersion:  current,
		LatestVersion:   release.Version,
		ReleaseURL:      release.URL,
		PublishedAt:     release.PublishedAt,
		Upgrade:         upgradeInstructions(runtime.GOOS),
	}
}

// upgradeInstructions keeps the advice runnable on the platform being advised:
// install.sh refuses to run on Windows, so Windows users are pointed at the
// release downloads instead.
func upgradeInstructions(goos string) UpgradeInstructions {
	if goos == "windows" {
		return UpgradeInstructions{Other: "download the latest release from " + releasesPage}
	}
	return UpgradeInstructions{Homebrew: "brew upgrade omni", Other: installCommand}
}

func (c *Checker) readState() (state, error) {
	data, err := os.ReadFile(c.StatePath)
	if err != nil {
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, err
	}
	return s, nil
}

func (c *Checker) writeState(s state) error {
	dir := filepath.Dir(c.StatePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".update-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := json.NewEncoder(f).Encode(s); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.StatePath); err == nil {
		return nil
	}
	// Windows does not replace an existing destination with Rename. This cache
	// is disposable, so a remove-and-retry is preferable to disabling checks.
	if err := os.Remove(c.StatePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, c.StatePath)
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

type parsedVersion struct {
	major, minor, patch int
	prerelease          []string
}

func validVersion(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// IsReleaseVersion reports whether v is a semantic release version.
func IsReleaseVersion(v string) bool {
	return validVersion(v)
}

func compareVersions(a, b string) int {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0
	}
	for _, pair := range [][2]int{{av.major, bv.major}, {av.minor, bv.minor}, {av.patch, bv.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(av.prerelease) == 0 && len(bv.prerelease) > 0 {
		return 1
	}
	if len(av.prerelease) > 0 && len(bv.prerelease) == 0 {
		return -1
	}
	for i := 0; i < len(av.prerelease) && i < len(bv.prerelease); i++ {
		aa, aerr := strconv.Atoi(av.prerelease[i])
		bb, berr := strconv.Atoi(bv.prerelease[i])
		switch {
		case aerr == nil && berr == nil && aa != bb:
			if aa < bb {
				return -1
			}
			return 1
		case aerr == nil && berr != nil:
			return -1
		case aerr != nil && berr == nil:
			return 1
		case av.prerelease[i] < bv.prerelease[i]:
			return -1
		case av.prerelease[i] > bv.prerelease[i]:
			return 1
		}
	}
	if len(av.prerelease) < len(bv.prerelease) {
		return -1
	}
	if len(av.prerelease) > len(bv.prerelease) {
		return 1
	}
	return 0
}

func parseVersion(v string) (parsedVersion, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "+", 2)[0]
	parts := strings.SplitN(v, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return parsedVersion{}, false
	}
	nums := make([]int, 3)
	for i, part := range core {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return parsedVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsedVersion{}, false
		}
		nums[i] = n
	}
	p := parsedVersion{major: nums[0], minor: nums[1], patch: nums[2]}
	if len(parts) == 2 {
		if parts[1] == "" {
			return parsedVersion{}, false
		}
		p.prerelease = strings.Split(parts[1], ".")
		for _, id := range p.prerelease {
			if id == "" {
				return parsedVersion{}, false
			}
		}
	}
	return p, true
}
