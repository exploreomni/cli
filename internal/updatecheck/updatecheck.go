// Package updatecheck discovers newer Omni CLI releases without affecting the
// command the user actually asked to run.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://api.github.com/repos/exploreomni/cli/releases/latest"
	checkInterval   = 24 * time.Hour
)

// Release is the subset of a GitHub release used by the updater.
type Release struct {
	Version     string    `json:"tag_name"`
	URL         string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// UpgradeInstructions lists supported ways to install the newest release.
type UpgradeInstructions struct {
	Homebrew string `json:"homebrew"`
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

type state struct {
	CheckedAt       time.Time `json:"checkedAt"`
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

// DefaultStatePath returns the per-user update-check state file.
func DefaultStatePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "omni-cli", "update.json")
}

// Check returns the newest stable release. Unless force is true, a result
// fetched during the previous 24 hours is reused.
func (c *Checker) Check(ctx context.Context, currentVersion string, force bool) (Result, error) {
	now := c.now()
	s, _ := c.readState()
	release := s.LatestRelease
	if force || s.CheckedAt.IsZero() || now.Sub(s.CheckedAt) >= checkInterval {
		var err error
		release, err = c.fetch(ctx, currentVersion)
		if err != nil {
			return Result{}, err
		}
		s.CheckedAt = now
		s.LatestRelease = release
		if err := c.writeState(s); err != nil {
			return Result{}, err
		}
	}

	return result(currentVersion, release), nil
}

// NotificationDue reports whether this release has not been announced in the
// previous 24 hours.
func (c *Checker) NotificationDue(r Result) bool {
	if !r.UpdateAvailable {
		return false
	}
	s, _ := c.readState()
	return s.NotifiedVersion != r.LatestVersion || s.NotifiedAt.IsZero() || c.now().Sub(s.NotifiedAt) >= checkInterval
}

// MarkNotified records that a notice was displayed. Notification failures are
// deliberately allowed to be ignored by callers.
func (c *Checker) MarkNotified(version string) error {
	s, _ := c.readState()
	s.NotifiedVersion = version
	s.NotifiedAt = c.now()
	return c.writeState(s)
}

func (c *Checker) fetch(ctx context.Context, currentVersion string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "omni-cli/"+currentVersion)

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
		Upgrade: UpgradeInstructions{
			Homebrew: "brew upgrade omni",
			Other:    "curl -fsSL https://raw.githubusercontent.com/exploreomni/cli/main/install.sh | sh",
		},
	}
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
