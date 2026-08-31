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
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"golang.org/x/term"
)

type automaticReleaseChecker interface {
	Check(ctx context.Context, currentVersion string, force bool) (updatecheck.Result, error)
	NotificationDue(updatecheck.Result) bool
	MarkNotified(version string) error
}

type updateOutcome struct {
	result updatecheck.Result
	err    error
}

type automaticUpdate struct {
	cancel   context.CancelFunc
	result   <-chan updateOutcome
	checker  automaticReleaseChecker
	enabled  bool
	version  string
	homebrew bool
	now      func() time.Time
}

func startAutomaticUpdate(checker automaticReleaseChecker, currentVersion string, args []string, stdout, stderr *os.File) automaticUpdate {
	if !automaticUpdatesEnabled(currentVersion, args, stdout, stderr) {
		return automaticUpdate{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan updateOutcome, 1)
	go func() {
		r, err := checker.Check(ctx, currentVersion, false)
		result <- updateOutcome{result: r, err: err}
	}()
	return automaticUpdate{
		cancel: cancel, result: result, checker: checker, enabled: true,
		version: currentVersion, homebrew: isHomebrewInstall(), now: time.Now,
	}
}

func (u automaticUpdate) finish(success bool, stderr io.Writer) {
	if !u.enabled {
		return
	}
	u.cancel()
	outcome := <-u.result
	if !success || outcome.err != nil || !u.checker.NotificationDue(outcome.result) {
		return
	}

	if u.homebrew && recentHomebrewRelease(outcome.result, u.now()) {
		return
	}
	fmt.Fprintf(stderr, "\nA newer Omni CLI is available: %s -> %s\n", displayVersion(u.version), displayVersion(outcome.result.LatestVersion))
	if u.homebrew {
		fmt.Fprintf(stderr, "To upgrade, run: %s\n", outcome.result.Upgrade.Homebrew)
	} else {
		fmt.Fprintf(stderr, "To upgrade, run: %s\n", outcome.result.Upgrade.Other)
	}
	fmt.Fprintf(stderr, "%s\n", outcome.result.ReleaseURL)
	_ = u.checker.MarkNotified(outcome.result.LatestVersion)
}

func automaticUpdatesEnabled(currentVersion string, args []string, stdout, stderr *os.File) bool {
	isTTY := term.IsTerminal(int(stdout.Fd())) && term.IsTerminal(int(stderr.Fd()))
	format := config.ResolveOutputFormat(requestedOutputFormat(args), true)
	return automaticUpdatesAllowed(currentVersion, args, isTTY && format == config.FormatHuman, os.Getenv)
}

func automaticUpdatesAllowed(currentVersion string, args []string, interactiveHuman bool, getenv func(string) string) bool {
	if len(args) == 0 || !updatecheck.IsReleaseVersion(currentVersion) || getenv("OMNI_NO_UPDATE_NOTIFIER") != "" || getenv("CI") != "" || !interactiveHuman {
		return false
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "--version" {
			return false
		}
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "update" && args[i+1] == "check" {
			return false
		}
	}
	return true
}

func requestedOutputFormat(args []string) string {
	for i, arg := range args {
		if strings.HasPrefix(arg, "--format=") {
			return strings.TrimPrefix(arg, "--format=")
		}
		if (arg == "--format" || arg == "-o") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
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
