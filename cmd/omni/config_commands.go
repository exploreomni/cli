package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/oauth"
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

func addConfigCommands(root *cobra.Command) {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration profiles",
	}

	configCmd.AddCommand(configInitCmd())
	configCmd.AddCommand(configShowCmd())
	configCmd.AddCommand(configUseCmd())
	configCmd.AddCommand(configLoginCmd())
	configCmd.AddCommand(configLogoutCmd())
	configCmd.AddCommand(configSetFormatCmd())

	root.AddCommand(configCmd)
}

func configInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a new configuration profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			fmt.Print("Profile name: ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)
			if name == "" {
				name = "default"
			}

			fmt.Print("API endpoint (e.g., https://myorg.omni.co): ")
			endpoint, _ := reader.ReadString('\n')
			endpoint = strings.TrimSpace(endpoint)

			fmt.Println("Authentication method:")
			fmt.Println("  1) API key")
			fmt.Println("  2) OAuth (browser login)")
			fmt.Print("Choose [1/2]: ")
			choice, _ := reader.ReadString('\n')
			choice = strings.TrimSpace(strings.ToLower(choice))

			cfg, err := config.Load()
			if err != nil {
				cfg = &config.Config{
					Version:  1,
					Profiles: make(map[string]config.Profile),
				}
			}

			switch choice {
			case "2", "o", "oauth":
				if err := config.ValidateEndpoint(endpoint); err != nil {
					return err
				}
				tok, err := oauth.Login(endpoint)
				if err != nil {
					return fmt.Errorf("OAuth login failed: %w", err)
				}

				p := config.Profile{APIEndpoint: endpoint}
				applyOAuthToken(&p, tok)
				cfg.Profiles[name] = p

			default: // "1", "a", "api-key", or empty
				fmt.Print("API key: ")
				apiKeyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return fmt.Errorf("reading API key: %w", err)
				}
				apiKey := strings.TrimSpace(string(apiKeyBytes))

				cfg.Profiles[name] = config.Profile{
					APIEndpoint: endpoint,
					AuthMethod:  "api-key",
					APIKey:      apiKey,
				}
			}

			if cfg.DefaultProfile == "" {
				cfg.DefaultProfile = name
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Profile %q saved to %s\n", name, config.ConfigPath())
			return nil
		},
	}
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init`")
			}

			name := args[0]
			if _, ok := cfg.Profiles[name]; !ok {
				available := make([]string, 0, len(cfg.Profiles))
				for k := range cfg.Profiles {
					available = append(available, k)
				}
				return fmt.Errorf("profile %q not found. Available: %s", name, strings.Join(available, ", "))
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

func configLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [profile]",
		Short: "Log in via OAuth browser flow",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init` first")
			}

			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				name = cfg.DefaultProfile
			}
			if name == "" {
				return fmt.Errorf("no profile specified and no default profile set")
			}

			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			if err := config.ValidateEndpoint(p.APIEndpoint); err != nil {
				return err
			}
			tok, err := oauth.Login(p.APIEndpoint)
			if err != nil {
				return fmt.Errorf("OAuth login failed: %w", err)
			}

			applyOAuthToken(&p, tok)
			cfg.Profiles[name] = p

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Login successful! Profile %q updated.\n", name)
			return nil
		},
	}
}

func configLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [profile]",
		Short: "Clear OAuth tokens from a profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — run `omni config init` first")
			}

			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				name = cfg.DefaultProfile
			}
			if name == "" {
				return fmt.Errorf("no profile specified and no default profile set")
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
