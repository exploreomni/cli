package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type releaseChecker interface {
	Check(ctx context.Context, currentVersion string) (updatecheck.Result, error)
}

func addUpdateCommand(root *cobra.Command, checker releaseChecker, currentVersion string) {
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Check for Omni CLI updates",
	}
	updateCmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check whether a newer Omni CLI release is available",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			// Resolve the format before checking: a failure has to be reported
			// in the format the caller asked for, not as cobra's plain
			// "Error: ..." line, which would break the JSON contract agents
			// rely on.
			formatFlag, _ := cmd.Flags().GetString("format")
			compact, _ := cmd.Flags().GetBool("compact")
			format := config.ResolveOutputFormat(formatFlag, writerIsTerminal(cmd.OutOrStdout()))

			result, err := checker.Check(cmd.Context(), currentVersion)
			if err != nil {
				// Same contract as a failed API call: exactly one document on
				// stderr, nothing on stdout, and no duplicate line from cobra.
				writeError(cmd.ErrOrStderr(), format, 0, err.Error(), nil, compact)
				cmd.SilenceErrors = true
				return &apiError{detail: err.Error()}
			}
			if format == config.FormatHuman {
				writeHumanUpdateResult(cmd, result)
				return nil
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			if !compact {
				enc.SetIndent("", "  ")
			}
			return enc.Encode(result)
		},
	})
	root.AddCommand(updateCmd)
}

func writeHumanUpdateResult(cmd *cobra.Command, r updatecheck.Result) {
	if !r.UpdateAvailable {
		fmt.Fprintf(cmd.OutOrStdout(), "Omni CLI %s is up to date.\n", displayVersion(r.CurrentVersion))
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "A newer Omni CLI is available: %s -> %s\n", displayVersion(r.CurrentVersion), displayVersion(r.LatestVersion))
	if r.Upgrade.Homebrew != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Homebrew: %s\n", r.Upgrade.Homebrew)
		fmt.Fprintf(cmd.OutOrStdout(), "Other:    %s\n", r.Upgrade.Other)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Upgrade:  %s\n", r.Upgrade.Other)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Release:  %s\n", r.ReleaseURL)
}

func recentHomebrewRelease(r updatecheck.Result, now time.Time) bool {
	return !r.PublishedAt.IsZero() && now.Sub(r.PublishedAt) < 24*time.Hour
}

func writerIsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func displayVersion(version string) string {
	return "v" + strings.TrimPrefix(version, "v")
}
