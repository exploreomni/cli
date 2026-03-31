package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/spf13/cobra"
)

func addConfigCommands(root *cobra.Command) {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration profiles",
	}

	configCmd.AddCommand(configInitCmd())
	configCmd.AddCommand(configShowCmd())
	configCmd.AddCommand(configUseCmd())

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

			fmt.Print("Organization ID: ")
			orgID, _ := reader.ReadString('\n')
			orgID = strings.TrimSpace(orgID)

			fmt.Print("Organization short ID: ")
			orgShortID, _ := reader.ReadString('\n')
			orgShortID = strings.TrimSpace(orgShortID)

			fmt.Print("API key: ")
			apiKey, _ := reader.ReadString('\n')
			apiKey = strings.TrimSpace(apiKey)

			cfg, err := config.Load()
			if err != nil {
				cfg = &config.Config{
					Version:  1,
					Profiles: make(map[string]config.Profile),
				}
			}

			cfg.Profiles[name] = config.Profile{
				OrganizationID:      orgID,
				OrganizationShortID: orgShortID,
				APIEndpoint:         endpoint,
				AuthMethod:          "api-key",
				APIKey:              apiKey,
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

			// Redact API keys for display
			display := *cfg
			for name, p := range display.Profiles {
				if p.APIKey != "" {
					p.APIKey = p.APIKey[:4] + "..." + p.APIKey[len(p.APIKey)-4:]
				}
				display.Profiles[name] = p
			}

			data, _ := json.MarshalIndent(display, "", "  ")
			fmt.Println(string(data))
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
