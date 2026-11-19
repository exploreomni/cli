package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/oauth"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/term"
)

// applyOAuthToken copies an oauth2.Token into a config.Profile in OAuth mode.
func applyOAuthToken(p *config.Profile, tok *oauth2.Token) {
	p.AuthMethod = "oauth"
	p.AccessToken = tok.AccessToken
	p.RefreshToken = tok.RefreshToken
	p.TokenExpiresAt = tok.Expiry.Format(time.RFC3339)
}

// Test seams for the OAuth login: the browser flow, and an override for the
// device grant's polling interval (zero keeps the server's).
var (
	browserLogin       = oauth.Login
	devicePollInterval time.Duration
)

// oauthLogin logs in to endpoint with the device grant, or with the browser
// flow when --browser is set or the server doesn't offer the device grant.
// The verification URL opens in a browser only for a person at a terminal
// with a browser to open. With jsonOut, the device code is also written to
// stdout as one JSON line before polling starts.
func oauthLogin(cmd *cobra.Command, endpoint string, jsonOut bool) (*oauth2.Token, error) {
	forceBrowser, _ := cmd.Flags().GetBool("browser")
	device := oauth.DeviceOptions{Stderr: os.Stderr, Interval: devicePollInterval}
	if jsonOut {
		device.JSON = os.Stdout
	}
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		device.OpenBrowser = oauth.BrowserOpener()
	}
	return oauth.LoginDefault(context.Background(), endpoint, oauth.LoginOptions{
		Browser:      forceBrowser,
		Device:       device,
		BrowserLogin: browserLogin,
	})
}

// jsonOutputChosen reports whether JSON output was explicitly asked for (by
// --format, OMNI_OUTPUT_FORMAT or the config default), as opposed to a pipe
// auto-detecting it. Login then keeps stdout to the device code's JSON line.
func jsonOutputChosen(cmd *cobra.Command) bool {
	format, _ := cmd.Flags().GetString("format")
	return config.ChosenOutputFormat(format) == config.FormatJSON
}

// addBrowserFlag registers --browser on a command that runs the OAuth login.
func addBrowserFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("browser", false, "log in through a browser on this machine (localhost callback) instead of with a device code")
}

func addConfigCommands(root *cobra.Command) {
	// Same unknown-subcommand handling as the generated groups.
	configCmd := openapi.NewGroupCommand("config", "Manage CLI configuration profiles")

	configCmd.AddCommand(configInitCmd())
	configCmd.AddCommand(configShowCmd())
	configCmd.AddCommand(configListCmd())
	configCmd.AddCommand(configUseCmd())
	configCmd.AddCommand(configRenameCmd())
	configCmd.AddCommand(configDeleteCmd())
	configCmd.AddCommand(configLoginCmd())
	configCmd.AddCommand(configLogoutCmd())
	configCmd.AddCommand(configSetFormatCmd())

	root.AddCommand(configCmd)
}

func configInitCmd() *cobra.Command {
	var (
		name       string
		endpoint   string
		authMethod string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new configuration profile",
		Long: `Create a new configuration profile.

Prompts interactively for any value not supplied via flags. With --name,
--endpoint, and --auth oauth set, OAuth setup runs with no prompts. For
api-key auth the key is always read from a hidden prompt — it is never
accepted as a flag, so it can't leak into shell history.

OAuth signs in with a device code: the CLI prints a URL and a code, you
approve it in a browser on any device, and the CLI picks up the tokens. Pass
--browser to use a browser on this machine instead (localhost callback); the
CLI also falls back to it when the Omni instance doesn't offer device login.
With --format json the URL and code are also printed to stdout as one JSON
line before the CLI waits for approval.`,
		Example: `  # Interactive setup
  omni config init

  # Non-interactive OAuth (prints a URL and code to approve)
  omni config init --name prod --endpoint https://myorg.omniapp.co --auth oauth

  # API key (prompts securely for the key)
  omni config init --name prod --endpoint https://myorg.omniapp.co --auth api-key`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			reader := bufio.NewReader(os.Stdin)

			if !cmd.Flags().Changed("name") {
				fmt.Print("Profile name: ")
				name, _ = reader.ReadString('\n')
			}
			name = strings.TrimSpace(name)
			if name == "" {
				name = "default"
			}

			if !cmd.Flags().Changed("endpoint") {
				fmt.Print("API endpoint (e.g., https://myorg.omniapp.co): ")
				endpoint, _ = reader.ReadString('\n')
			}
			endpoint = strings.TrimSpace(endpoint)

			choice := strings.TrimSpace(strings.ToLower(authMethod))
			switch {
			case choice == "":
				fmt.Println("Authentication method:")
				fmt.Println("  1) API key")
				fmt.Println("  2) OAuth (sign in with your Omni account)")
				fmt.Print("Choose [1/2]: ")
				choice, _ = reader.ReadString('\n')
				choice = strings.TrimSpace(strings.ToLower(choice))
			case choice != "oauth" && choice != "api-key":
				return fmt.Errorf("invalid --auth %q — must be %q or %q", authMethod, "api-key", "oauth")
			}

			// Fail before the login, not after it, on a config file that
			// exists but can't be read.
			if _, err := config.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("loading config %s: %w", config.ConfigPath(), err)
			}

			jsonOut := jsonOutputChosen(cmd)
			var p config.Profile
			switch choice {
			case "2", "o", "oauth":
				if err := config.ValidateEndpoint(endpoint); err != nil {
					return err
				}
				tok, err := oauthLogin(cmd, endpoint, jsonOut)
				if err != nil {
					return fmt.Errorf("OAuth login failed: %w", err)
				}

				p = config.Profile{APIEndpoint: endpoint}
				applyOAuthToken(&p, tok)

			default: // "1", "a", "api-key", or empty
				// The key is always read from a hidden prompt rather than a
				// flag, so it never lands in shell history or process listings.
				fmt.Print("API key: ")
				apiKeyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return fmt.Errorf("reading API key: %w", err)
				}
				apiKey := strings.TrimSpace(string(apiKeyBytes))

				p = config.Profile{
					APIEndpoint: endpoint,
					AuthMethod:  "api-key",
					APIKey:      apiKey,
				}
			}

			cfg, err := config.Update(func(cfg *config.Config) error {
				cfg.Profiles[name] = p
				if cfg.DefaultProfile == "" {
					cfg.DefaultProfile = name
				}
				return nil
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(successOutput(jsonOut), "Profile %q saved to %s\n", name, config.ConfigPath())
			maybePrintBanner(cmd, name, cfg)
			return nil
		},
	}
	addBrowserFlag(cmd)

	cmd.Flags().StringVar(&name, "name", "", "profile name (skips prompt)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "API endpoint, e.g. https://myorg.omniapp.co (skips prompt)")
	cmd.Flags().StringVar(&authMethod, "auth", "", `authentication method: "api-key" or "oauth" (skips prompt). The API key itself is always read from a hidden prompt, never a flag.`)

	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "show",
		Short:       "Display current configuration",
		Annotations: map[string]string{machineOutputAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found at %s — run `omni config init`", config.ConfigPath())
			}

			// Redact secrets for display
			display := *cfg
			for name, p := range display.Profiles {
				if len(p.APIKey) >= 12 {
					p.APIKey = p.APIKey[:4] + "..." + p.APIKey[len(p.APIKey)-4:]
				} else if p.APIKey != "" {
					p.APIKey = "****"
				}
				if p.AccessToken != "" {
					p.AccessToken = "****"
				}
				if p.RefreshToken != "" {
					p.RefreshToken = "****"
				}
				display.Profiles[name] = p
			}

			data, _ := json.MarshalIndent(display, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func configSetFormatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-format <json|human|auto>",
		Short: "Set the default output format",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			format := strings.ToLower(strings.TrimSpace(args[0]))
			if !config.ValidOutputFormat(format) {
				return fmt.Errorf("invalid format %q — must be one of: json, human, auto", args[0])
			}

			cfg, err := config.Load()
			if err != nil {
				cfg = &config.Config{
					Version:  1,
					Profiles: make(map[string]config.Profile),
				}
			}
			cfg.DefaultOutputFormat = format
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Default output format set to %q\n", format)
			return nil
		},
	}
}

func configUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Switch the default profile",
		Long:  "Switch the default profile.\n\nIf the profile name contains spaces, quote it: `omni config use \"My Profile\"`. Run `omni config list` to see profile names.",
		Args:  profileNameArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init`")
			}

			name := args[0]
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found. Available: %s", name, formatProfileList(cfg))
			}

			cfg.DefaultProfile = name
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Switched to profile %q\n", name)
			return nil
		},
	}
}

// profileNameArgs validates the profile-name positional argument, accepting
// between min and max args. A profile name is a single token, so when the shell
// hands us more than max args it almost always means the user typed a name with
// spaces without quoting it (the reported failure mode in #45). Rather than
// cobra's opaque "accepts 1 arg(s), received 2", we reconstruct the likely
// intended name and show how to quote it.
func profileNameArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > max {
			return fmt.Errorf("got %d arguments — a profile name is a single value; if it contains spaces, quote it: %s %q",
				len(args), cmd.CommandPath(), strings.Join(args, " "))
		}
		if len(args) < min {
			return fmt.Errorf("%s requires a profile name", cmd.CommandPath())
		}
		return nil
	}
}

// formatProfileList returns a comma-separated list of profile names, quoted so
// that names containing spaces are visually distinguishable.
func formatProfileList(cfg *config.Config) string {
	names := make([]string, 0, len(cfg.Profiles))
	for k := range cfg.Profiles {
		names = append(names, fmt.Sprintf("%q", k))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func configListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init`")
			}
			if len(cfg.Profiles) == 0 {
				fmt.Println("(no profiles)")
				return nil
			}
			names := make([]string, 0, len(cfg.Profiles))
			for k := range cfg.Profiles {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, n := range names {
				marker := "  "
				if n == cfg.DefaultProfile {
					marker = "* "
				}
				p := cfg.Profiles[n]
				fmt.Printf("%s%s\t%s\t%s\n", marker, n, p.AuthMethod, p.APIEndpoint)
			}
			return nil
		},
	}
}

func configRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			oldName, newName := args[0], args[1]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init`")
			}
			p, ok := cfg.Profiles[oldName]
			if !ok {
				return fmt.Errorf("profile %q not found. Available: %s", oldName, formatProfileList(cfg))
			}
			if oldName == newName {
				return nil
			}
			if _, exists := cfg.Profiles[newName]; exists {
				return fmt.Errorf("profile %q already exists", newName)
			}

			delete(cfg.Profiles, oldName)
			cfg.Profiles[newName] = p
			if cfg.DefaultProfile == oldName {
				cfg.DefaultProfile = newName
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			fmt.Printf("Renamed profile %q to %q\n", oldName, newName)
			return nil
		},
	}
}

func configDeleteCmd() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "delete <profile>",
		Short: "Delete a profile",
		Args:  profileNameArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init`")
			}
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found. Available: %s", name, formatProfileList(cfg))
			}

			if !assumeYes {
				fmt.Printf("Delete profile %q? This cannot be undone. [y/N]: ", name)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			delete(cfg.Profiles, name)
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = ""
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			fmt.Printf("Deleted profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// targetProfileName resolves which profile a login/logout command should act
// on: the positional argument wins, then the global -p/--profile flag, then the
// configured default profile.
func targetProfileName(cmd *cobra.Command, args []string, cfg *config.Config) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}
	if flag, _ := cmd.Flags().GetString("profile"); flag != "" {
		return flag, nil
	}
	if cfg.DefaultProfile != "" {
		return cfg.DefaultProfile, nil
	}
	return "", fmt.Errorf("no profile specified and no default profile set")
}

// successOutput is where a login's closing message goes: stdout, or stderr
// when JSON output keeps stdout to the device code's JSON line.
func successOutput(jsonOut bool) *os.File {
	if jsonOut {
		return os.Stderr
	}
	return os.Stdout
}

func configLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [profile]",
		Short: "Log in with OAuth (device code, or --browser)",
		Long: `Log in with OAuth and save the tokens to a profile.

The CLI prints a URL and a code; approve the code in a browser on any device
and the CLI picks up the tokens. At a terminal it also opens the URL for you.
Pass --browser to log in through a browser on this machine instead (localhost
callback); the CLI also falls back to it when the Omni instance doesn't offer
device login.

With --format json, the URL and code are printed to stdout as one JSON line
before the CLI waits for approval, so a script or agent can read it and show
it to the person signing in:

  {"verification_uri":"…","verification_uri_complete":"…","user_code":"BDKF-QRTW","expires_in":600}`,
		Args: profileNameArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init` first")
			}

			name, err := targetProfileName(cmd, args, cfg)
			if err != nil {
				return err
			}

			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			if err := config.ValidateEndpoint(p.APIEndpoint); err != nil {
				return err
			}
			jsonOut := jsonOutputChosen(cmd)
			tok, err := oauthLogin(cmd, p.APIEndpoint, jsonOut)
			if err != nil {
				return fmt.Errorf("OAuth login failed: %w", err)
			}

			// Apply the tokens to the profile as it is on disk now, not as it
			// was read before the (possibly minutes-long) login.
			cfg, err = config.Update(func(cfg *config.Config) error {
				latest, ok := cfg.Profiles[name]
				if !ok {
					return fmt.Errorf("profile %q was removed during login", name)
				}
				if latest.APIEndpoint != p.APIEndpoint {
					return fmt.Errorf("profile %q changed endpoint during login; run login again", name)
				}
				applyOAuthToken(&latest, tok)
				cfg.Profiles[name] = latest
				return nil
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(successOutput(jsonOut), "Login successful! Profile %q updated.\n", name)
			maybePrintBanner(cmd, name, cfg)
			return nil
		},
	}
	addBrowserFlag(cmd)
	return cmd
}

func configLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [profile]",
		Short: "Clear OAuth tokens from a profile",
		Args:  profileNameArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Args are validated: failures below are runtime errors, not usage errors.
			cmd.SilenceUsage = true

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init` first")
			}

			name, err := targetProfileName(cmd, args, cfg)
			if err != nil {
				return err
			}

			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			p.AccessToken = ""
			p.RefreshToken = ""
			p.TokenExpiresAt = ""
			cfg.Profiles[name] = p

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Logged out of profile %q. Tokens cleared.\n", name)
			return nil
		},
	}
}
