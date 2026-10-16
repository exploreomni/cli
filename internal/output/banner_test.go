package output

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var bannerCHA = regexp.MustCompile(`\x1b\[\d+G`)

func banner(info BannerInfo, width int) []string {
	var buf bytes.Buffer
	Banner(&buf, info, width)
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func demoBanner() BannerInfo {
	return BannerInfo{
		Version: "v1.4.0",
		Fields: []BannerField{
			{Label: "Profile", Value: "default"},
			{Label: "Instance", Value: "acme.omniapp.co"},
			{Label: "Auth", Value: "api-key"},
		},
		Hint: "AI agents: start with omni agent-help",
		Dir:  "/Users/blobby/analytics",
	}
}

// A line wider than the terminal wraps and tears the box apart, so every
// line has to come out at the terminal's width, clamped to what the banner
// can draw.
func TestBanner_EveryLineFitsTheTerminal(t *testing.T) {
	for _, tc := range []struct{ terminal, want int }{
		{40, BannerMinWidth},
		{BannerMinWidth, BannerMinWidth},
		{80, 80},
		{BannerMaxWidth - 1, BannerMaxWidth - 1},
		{BannerMaxWidth, BannerMaxWidth},
		{200, BannerMaxWidth},
	} {
		lines := banner(demoBanner(), tc.terminal)
		if len(lines) != len(bannerSky)+4 {
			t.Errorf("terminal %d: got %d lines, want %d", tc.terminal, len(lines), len(bannerSky)+4)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != tc.want {
				t.Errorf("terminal %d: line %d is %d cells wide, want %d:\n%s", tc.terminal, i, got, tc.want, line)
			}
		}
	}
}

func TestBanner_ShowsWhatItWasGiven(t *testing.T) {
	out := strings.Join(banner(demoBanner(), BannerMaxWidth), "\n")
	for _, want := range []string{
		"╭─ Omni CLI v1.4.0 ─",
		"Profile:    default",
		"Instance:   acme.omniapp.co",
		"Auth:       api-key",
		"AI agents: start with omni agent-help",
		"/Users/blobby/analytics",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner is missing %q:\n%s", want, out)
		}
	}
}

// Thunder salmon: the sky carries lightning and salmon. At the narrowest
// width the right-hand cloud's share is cropped, not squeezed in.
func TestBanner_Weather(t *testing.T) {
	wide := strings.Join(banner(demoBanner(), BannerMaxWidth), "\n")
	if strings.Count(wide, "⚡") != 2 || strings.Count(wide, "🐟") != 2 {
		t.Errorf("full-width sky should carry two bolts and two salmon:\n%s", wide)
	}
	narrow := strings.Join(banner(demoBanner(), BannerMinWidth), "\n")
	if strings.ContainsAny(narrow, "⚡🐟░") {
		t.Errorf("the narrowest banner has room for Blobby alone:\n%s", narrow)
	}
}

// Blobby's last pixel row lands on the divider, with a gap in the line
// either side.
func TestBanner_BlobbySitsOnTheDivider(t *testing.T) {
	lines := banner(demoBanner(), BannerMaxWidth)
	divider := lines[len(bannerSky)+1]
	if !strings.HasPrefix(divider, "├─────   ▀▀██████▀▀   ───") {
		t.Errorf("divider should break around Blobby: %s", divider)
	}
}

func TestBanner_TruncatesWhatDoesNotFit(t *testing.T) {
	info := demoBanner()
	info.Version = "v1.4.1-0.20261005120000-0123456789ab+dirty.and.then.some.more.build.metadata"
	info.Fields[1].Value = "an-extremely-long-instance-name.that-goes-on.omniapp.co"
	info.Fields = append(info.Fields, BannerField{"Four", "4"}, BannerField{"Five", "5"})
	info.Hint = strings.Repeat("hint ", 20)
	info.Dir = "/Users/blobby/" + strings.Repeat("deeply/nested/", 12) + "analytics"

	lines := banner(info, BannerMaxWidth)
	for i, line := range lines {
		if got := lipgloss.Width(line); got != BannerMaxWidth {
			t.Errorf("line %d is %d cells wide, want %d:\n%s", i, got, BannerMaxWidth, line)
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "nested/analytics") || !strings.Contains(out, "│  …") {
		t.Errorf("a long directory should keep its end behind an ellipsis:\n%s", out)
	}
	if !strings.Contains(out, "Four:") || strings.Contains(out, "Five:") {
		t.Errorf("the info panel holds four fields:\n%s", out)
	}
}

// The directory comes from the filesystem and the version from the build;
// neither gets to drive the terminal.
func TestBanner_StripsControlCharacters(t *testing.T) {
	info := demoBanner()
	info.Dir = "/tmp/\x1b]0;owned\x07dir\nname"
	info.Fields[0].Value = "pro\x1b[31mfile"
	out := strings.Join(banner(info, BannerMaxWidth), "\n")
	// The banner's own cursor-column sequences are the only escapes allowed.
	if rest := bannerCHA.ReplaceAllString(out, ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Errorf("control characters reached the output: %q", rest)
	}
	if got := len(banner(info, BannerMaxWidth)); got != len(bannerSky)+4 {
		t.Errorf("a newline in the directory added lines: got %d", got)
	}
}
