package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

//go:embed openapi.json
var specFS embed.FS

var version = "dev"

func init() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			version = info.Main.Version
		}
	}
}

func main() {
	checker := updatecheck.New()
	root := &cobra.Command{
		Use:     "omni",
		Short:   "Omni CLI — programmatic access to the Omni API",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip auth for config commands
			if cmd.Name() == "init" || cmd.Name() == "show" || cmd.Name() == "use" || cmd.Name() == "login" || cmd.Name() == "logout" || cmd.Name() == "config" {
				return nil
			}
			// Skip auth for help/version
			if cmd.Name() == "agent-help" {
				return nil
			}
			if cmd.Name() == "help" || cmd.Name() == "version" {
				return nil
			}
			return nil
		},
	}

	addGlobalFlags(root)

	// Flag names are matched ignoring case and dash/underscore placement, so
	// --base-url, --baseurl and --base_url are the same flag on every command.
	// Set before the subcommands are added: cobra propagates the function to
	// children as they're attached, which covers the hand-written commands and
	// the root's persistent flags as well as the generated ones.
	root.SetGlobalNormalizationFunc(openapi.NormalizeFlagName)

	// Hand-written commands (not from spec)
	addConfigCommands(root)
	addAgentHelpCommand(root)
	addUpdateCommand(root, checker, version)

	// Load OpenAPI spec and generate API commands
	specData, err := specFS.ReadFile("openapi.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load embedded API spec: %v\n", err)
		os.Exit(1)
	}

	apiCmds, err := openapi.GenerateCommands(specData, executeAPICall)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to parse API spec: %v\n", err)
		os.Exit(1)
	}

	for _, cmd := range apiCmds {
		root.AddCommand(cmd)
	}

	// Hand-written commands that attach to generated command groups
	addBranchCommands(root, executeAPICall)
	addUserCommands(root, executeAPICall)

	// ExecuteC, not Execute: cobra returns a nil error whenever it answers the
	// help flag, including for `omni models list-branches --help`, where the
	// "help" is really an unknown-subcommand error. UnknownSubcommand asks the
	// command that ran whether that's what happened.
	automaticUpdate := startAutomaticUpdate(checker, version, os.Args[1:], os.Stdout, os.Stderr)
	cmd, err := root.ExecuteC()
	success := err == nil && !openapi.UnknownSubcommand(cmd)
	automaticUpdate.finish(success, os.Stderr)
	if !success {
		os.Exit(1)
	}
}

// addGlobalFlags registers the flags every command inherits.
//
// These names are reserved: openapi.IsReservedFlagName knows them, so a spec
// query param that would otherwise shadow one (say a param named "baseUrl"
// taking over --base-url) is registered under a --param- prefix instead.
// TestGlobalFlagsAreReserved fails if a flag is added here without being
// added there too.
func addGlobalFlags(root *cobra.Command) {
	root.PersistentFlags().StringP("profile", "p", "", "config profile to use")
	root.PersistentFlags().String("token", "", "API token (overrides profile/env)")
	root.PersistentFlags().String("base-url", "", "API base URL (overrides profile)")
	root.PersistentFlags().Bool("compact", false, "compact JSON output (no indentation)")
	root.PersistentFlags().StringP("format", "o", "", "output format: json, human, auto (default auto: human on TTY, json when piped)")
}

// executeAPICall is the callback invoked by generated commands to make the actual HTTP request.
func executeAPICall(req openapi.APIRequest) error {
	cfg, err := resolveConfig(req.Cmd)
	if err != nil {
		return err
	}

	compact, _ := req.Cmd.Flags().GetBool("compact")
	formatFlag, _ := req.Cmd.Flags().GetString("format")
	format := config.ResolveOutputFormat(formatFlag, term.IsTerminal(int(os.Stdout.Fd())))

	// Show a spinner on stderr while the request is in flight. Only when the
	// user is at an interactive terminal AND they're going to see human output;
	// scripts piping JSON shouldn't get decorative noise on stderr.
	sp := maybeStartSpinner(format)

	resp, err := auth.DoWithContentType(cfg, req.Method, req.Path, req.Body, req.ContentType)
	sp.Stop()
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = outputResponse(resp, format, compact)
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		// outputResponse already wrote a complete error message to stderr —
		// a JSON envelope carrying the status, or the human-mode one-liner.
		// Letting cobra append its own line would say it twice, and would
		// leave two documents on stderr for JSON consumers to trip over.
		req.Cmd.SilenceErrors = true
	}
	return err
}

// resolveConfig builds the runtime config from flags, env, and config file.
func resolveConfig(cmd *cobra.Command) (*config.ResolvedConfig, error) {
	profileName, _ := cmd.Flags().GetString("profile")
	tokenFlag, _ := cmd.Flags().GetString("token")
	baseURLFlag, _ := cmd.Flags().GetString("base-url")

	return config.Resolve(profileName, tokenFlag, baseURLFlag)
}
