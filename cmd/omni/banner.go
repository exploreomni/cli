package main

import (
	"fmt"
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
// the welcome banner for the profile that was just connected.
func maybePrintBanner(cmd *cobra.Command, name string, cfg *config.Config) {
	stdout := os.Stdout
	fd := int(stdout.Fd())
	isTTY := term.IsTerminal(fd) && term.IsTerminal(int(os.Stderr.Fd()))
	formatFlag, _ := cmd.Flags().GetString("format")
	if !bannerAllowed(isTTY, config.ResolveOutputFormat(formatFlag, isTTY), os.Getenv) {
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

// bannerAllowed reports whether a person at a terminal is reading this
// command's output. Everyone else goes without: piped or redirected output,
// a JSON output format, CI, a dumb terminal, a coding agent's shell, and
// anyone who set OMNI_NO_BANNER.
func bannerAllowed(isTTY bool, format string, getenv func(string) string) bool {
	if !isTTY || format != config.FormatHuman {
		return false
	}
	if getenv("OMNI_NO_BANNER") != "" || getenv("CI") != "" || getenv("TERM") == "dumb" {
		return false
	}
	for _, name := range agentEnvVars {
		if getenv(name) != "" {
			return false
		}
	}
	return true
}

// bannerInfo describes the profile named name as cfg holds it. It reads the
// config alone: the banner never shows a secret and never touches the
// network.
func bannerInfo(version, name string, cfg *config.Config, dir string) output.BannerInfo {
	p := cfg.Profiles[name]
	instance := strings.TrimPrefix(p.APIEndpoint, "https://")
	if instance == "" {
		instance = "not configured"
	}
	auth := p.AuthMethod
	if auth == "" {
		auth = "api-key"
	}
	hint := "Next: omni models list"
	if cfg.DefaultProfile != name {
		hint = "Not the default profile: omni config use"
	}
	return output.BannerInfo{
		Version: bannerVersion(version),
		Fields: []output.BannerField{
			{Label: "Profile", Value: name},
			{Label: "Instance", Value: instance},
			{Label: "Auth", Value: auth},
		},
		Hint: hint,
		Dir:  dir,
	}
}

// bannerVersion is the build's version as the banner shows it: v-prefixed
// for a release, and as-is ("dev") for a build that doesn't have one.
func bannerVersion(version string) string {
	if updatecheck.IsReleaseVersion(version) {
		return displayVersion(version)
	}
	return version
}
