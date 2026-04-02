package main

import (
	"embed"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
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
	root := &cobra.Command{
		Use:     "omni",
		Short:   "Omni CLI — programmatic access to the Omni API",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip auth for config commands
			if cmd.Name() == "init" || cmd.Name() == "show" || cmd.Name() == "use" || cmd.Name() == "config" {
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

	// Global flags
	root.PersistentFlags().StringP("profile", "p", "", "config profile to use")
	root.PersistentFlags().String("token", "", "API token (overrides profile/env)")
	root.PersistentFlags().String("base-url", "", "API base URL (overrides profile)")
	root.PersistentFlags().Bool("compact", false, "compact JSON output (no indentation)")


	// Hand-written commands (not from spec)
	addConfigCommands(root)
	addAgentHelpCommand(root)

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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// executeAPICall is the callback invoked by generated commands to make the actual HTTP request.
func executeAPICall(req openapi.APIRequest) error {
	cfg, err := resolveConfig(req.Cmd)
	if err != nil {
		return err
	}

	resp, err := auth.Do(cfg, req.Method, req.Path, req.Body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	compact, _ := req.Cmd.Flags().GetBool("compact")
	return outputResponse(resp, compact)
}

// resolveConfig builds the runtime config from flags, env, and config file.
func resolveConfig(cmd *cobra.Command) (*config.ResolvedConfig, error) {
	profileName, _ := cmd.Flags().GetString("profile")
	tokenFlag, _ := cmd.Flags().GetString("token")
	baseURLFlag, _ := cmd.Flags().GetString("base-url")

	return config.Resolve(profileName, tokenFlag, baseURLFlag)
}
