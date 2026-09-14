package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type automaticReleaseChecker interface {
	CheckAutomatic(ctx context.Context, currentVersion string) (updatecheck.Result, error)
	ClaimNotification(updatecheck.Result) bool
}

type updateOutcome struct {
	result updatecheck.Result
	err    error
}

type automaticUpdate struct {
	cancel     context.CancelFunc
	result     <-chan updateOutcome
	checker    automaticReleaseChecker
	enabled    bool
	version    string
	isHomebrew func() bool
	now        func() time.Time
}

const machineOutputAnnotation = "omni.machine-output"

// startAutomaticUpdate begins a background check alongside the command the user
// asked for. It runs from the root's PersistentPreRunE, so cmd is the command
// cobra resolved and its flags are parsed: eligibility can look at the real
// command and the resolved --format rather than guessing from raw os.Args.
func startAutomaticUpdate(checker automaticReleaseChecker, currentVersion string, cmd *cobra.Command, stdout, stderr *os.File) automaticUpdate {
	if !automaticUpdatesEnabled(currentVersion, cmd, stdout, stderr) {
		return automaticUpdate{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan updateOutcome, 1)
	go func() {
		r, err := checker.CheckAutomatic(ctx, currentVersion)
		result <- updateOutcome{result: r, err: err}
	}()
	return automaticUpdate{
		cancel: cancel, result: result, checker: checker, enabled: true,
		version: currentVersion, isHomebrew: isHomebrewInstall, now: time.Now,
	}
}

func (u automaticUpdate) finish(success bool, stderr io.Writer) {
	if !u.enabled {
		return
	}
	u.cancel()
	if !success {
		return
	}
	var outcome updateOutcome
	select {
	case outcome = <-u.result:
	default:
		return
	}
	if outcome.err != nil || !outcome.result.UpdateAvailable {
		return
	}

	homebrew := u.isHomebrew != nil && u.isHomebrew()
	if homebrew && recentHomebrewRelease(outcome.result, u.now()) {
		return
	}
	// Claim before printing: the claim is what makes concurrent commands
	// announce a release once between them.
	if !u.checker.ClaimNotification(outcome.result) {
		return
	}
	fmt.Fprintf(stderr, "\nA newer Omni CLI is available: %s -> %s\n", displayVersion(u.version), displayVersion(outcome.result.LatestVersion))
	fmt.Fprintf(stderr, "To upgrade, %s\n", upgradeHint(outcome.result.Upgrade, homebrew))
	fmt.Fprintf(stderr, "%s\n", outcome.result.ReleaseURL)
}

// upgradeHint phrases the advice for the platform it's given on. Homebrew is
// empty where Homebrew isn't an option (Windows), and there "other" is already
// a sentence — install.sh refuses to run there — rather than a command to run.
func upgradeHint(u updatecheck.UpgradeInstructions, homebrew bool) string {
	if homebrew && u.Homebrew != "" {
		return "run: " + u.Homebrew
	}
	if u.Homebrew == "" {
		return u.Other
	}
	return "run: " + u.Other
}

func automaticUpdatesEnabled(currentVersion string, cmd *cobra.Command, stdout, stderr *os.File) bool {
	isTTY := term.IsTerminal(int(stdout.Fd())) && term.IsTerminal(int(stderr.Fd()))
	return automaticUpdatesAllowed(currentVersion, cmd, interactiveHumanOutput(cmd, isTTY), os.Getenv)
}

// interactiveHumanOutput reports whether this invocation is a human sitting at
// a terminal reading human-formatted output — the only audience a notice on
// stderr is meant for. The format comes from the parsed flag, so every spelling
// cobra accepts (-o json, -ojson, --format=json, --FORMAT json) is honoured.
func interactiveHumanOutput(cmd *cobra.Command, isTTY bool) bool {
	if !isTTY || cmd == nil || commandUsesMachineOutput(cmd) {
		return false
	}
	formatFlag, _ := cmd.Flags().GetString("format")
	return config.ResolveOutputFormat(formatFlag, isTTY) == config.FormatHuman
}

func commandUsesMachineOutput(cmd *cobra.Command) bool {
	return cmd.Annotations[machineOutputAnnotation] == "true" || openapi.IsSchemaRequest(cmd)
}

func automaticUpdatesAllowed(currentVersion string, cmd *cobra.Command, interactiveHuman bool, getenv func(string) string) bool {
	if cmd == nil || !interactiveHuman || !updatecheck.IsReleaseVersion(currentVersion) ||
		getenv("OMNI_NO_UPDATE_NOTIFIER") != "" || getenv("CI") != "" {
		return false
	}
	// `omni help ...` prints a command's usage, not its output; --help and
	// --version never reach this hook, cobra answers them first.
	if cmd.Name() == "help" {
		return false
	}
	// The explicit `omni update check` does its own reporting, and shell
	// completion output is consumed by the shell. Match on the top-level
	// group so an API command that happens to be named "update" (say
	// `omni dashboards update`) still gets a notice.
	switch topLevelCommand(cmd).Name() {
	case "update", "completion":
		return false
	}
	return true
}

// topLevelCommand returns cmd's ancestor directly below the root, or cmd
// itself when it is the root or one of the root's own children.
func topLevelCommand(cmd *cobra.Command) *cobra.Command {
	for cmd.Parent() != nil && cmd.Parent().Parent() != nil {
		cmd = cmd.Parent()
	}
	return cmd
}

func isHomebrewInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path := filepath.ToSlash(exe)
	return strings.Contains(path, "/Cellar/omni/") || strings.Contains(path, "/Homebrew/Cellar/omni/")
}
