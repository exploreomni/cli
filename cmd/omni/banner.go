package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// agentEnvVars are set by coding agents in the shells they run commands in.
// Some of them hand the command a pseudo-terminal, so a TTY alone doesn't
// prove a person is reading.
var agentEnvVars = []string{"AI_AGENT", "AGENT", "CLAUDECODE", "CODEX_SANDBOX", "CURSOR_AGENT", "GEMINI_CLI"}

// maybePrintBanner closes a successful `config init` or `config login` with
// the welcome banner for the profile that was just connected. It is for a
// person at a terminal: the same audience as the update notice, less anyone
// who opted out and less a coding agent's shell.
func maybePrintBanner(cmd *cobra.Command, name string, cfg *config.Config) {
	stdout := os.Stdout
	fd := int(stdout.Fd())
	isTTY := term.IsTerminal(fd) && term.IsTerminal(int(os.Stderr.Fd()))
	if !interactiveHumanOutput(cmd, isTTY) || bannerOptedOut(os.Getenv) {
		return
	}
	// Leave the last column free: some terminals wrap a line that fills it.
	width, _, err := term.GetSize(fd)
	if err != nil || width-1 < output.BannerMinWidth {
		return
	}
	dir, _ := os.Getwd()
	fmt.Fprintln(stdout)
	output.Banner(stdout, bannerInfo(version, name, cfg, dir), width-1)
}

// bannerOptedOut reports an environment that doesn't want the banner even
// at a terminal: OMNI_NO_BANNER, CI, a dumb terminal, or a coding agent's
// shell.
func bannerOptedOut(getenv func(string) string) bool {
	if getenv("OMNI_NO_BANNER") != "" || getenv("CI") != "" || getenv("TERM") == "dumb" {
		return true
	}
	for _, name := range agentEnvVars {
		if getenv(name) != "" {
			return true
		}
	}
	return false
}

// bannerInfo describes the profile named name as cfg holds it. It reads the
// config alone: the banner never shows a secret and never touches the
// network.
func bannerInfo(version, name string, cfg *config.Config, dir string) output.BannerInfo {
	p := cfg.Profiles[name]
	auth := p.AuthMethod
	if auth == "" {
		auth = "api-key"
	}
	hint := "Next: omni models list"
	if cfg.DefaultProfile != name {
		hint = "Set as default: omni config use " + shellWord(name)
	}
	return output.BannerInfo{
		Version: bannerVersion(version),
		Fields: []output.BannerField{
			{Label: "Profile", Value: name},
			{Label: "Instance", Value: endpointHost(p.APIEndpoint)},
			{Label: "Auth", Value: auth},
		},
		Hint: hint,
		Dir:  dir,
	}
}

// endpointHost is the part of a saved endpoint the banner may show: the host
// and port alone. An endpoint is saved as typed, so it can carry a
// user:password, a query string or a path that must not be echoed.
func endpointHost(endpoint string) string {
	if endpoint == "" {
		return "not configured"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "unrecognized endpoint"
	}
	return u.Host
}

// shellWord quotes a profile name for pasting into a shell when it needs
// it, the way `omni config use` documents.
func shellWord(name string) string {
	if strings.ContainsAny(name, " \t\"'$`\\!#&|;<>(){}[]*?~") {
		return "'" + strings.ReplaceAll(name, "'", "'\\''") + "'"
	}
	return name
}

// bannerVersion is the build's version as the banner shows it: v-prefixed
// for a release, and as-is ("dev") for a build that doesn't have one.
func bannerVersion(version string) string {
	if updatecheck.IsReleaseVersion(version) {
		return displayVersion(version)
	}
	return version
}
