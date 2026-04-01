// Package config manages CLI profiles and resolves runtime configuration
// from flags, environment variables, and the config file.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Profile represents a saved API configuration.
type Profile struct {
	APIEndpoint string `json:"apiEndpoint"`
	AuthMethod  string `json:"authMethod"`
	APIKey      string `json:"apiKey,omitempty"`
}

// Config is the on-disk config file format (compatible with the TS CLI).
type Config struct {
	Version        int                `json:"version"`
	DefaultProfile string             `json:"defaultProfile,omitempty"`
	Profiles       map[string]Profile `json:"profiles"`
}

// ResolvedConfig is the final runtime config after merging flags, env, and file.
type ResolvedConfig struct {
	Token   string
	BaseURL string
}

// Resolve builds the runtime config with precedence: flags > env > config file.
func Resolve(profileName, tokenFlag, baseURLFlag string) (*ResolvedConfig, error) {
	rc := &ResolvedConfig{}

	// Start from config file
	cfg, _ := Load()
	if cfg != nil {
		name := profileName
		if name == "" {
			name = cfg.DefaultProfile
		}
		if name != "" {
			if p, ok := cfg.Profiles[name]; ok {
				rc.BaseURL = p.APIEndpoint
				rc.Token = p.APIKey
			}
		}
	}

	// Env vars override config file
	if v := os.Getenv("OMNI_API_TOKEN"); v != "" {
		rc.Token = v
	}
	if v := os.Getenv("OMNI_API_KEY"); v != "" && rc.Token == "" {
		rc.Token = v
	}
	if v := os.Getenv("OMNI_BASE_URL"); v != "" {
		rc.BaseURL = v
	}

	// Flags override everything
	if tokenFlag != "" {
		rc.Token = tokenFlag
	}
	if baseURLFlag != "" {
		rc.BaseURL = baseURLFlag
	}

	// Validate
	if rc.Token == "" {
		return nil, fmt.Errorf("no API token configured. Set OMNI_API_TOKEN, use --token, or run `omni config init`")
	}
	if rc.BaseURL == "" {
		return nil, fmt.Errorf("no API base URL configured. Set OMNI_BASE_URL, use --base-url, or run `omni config init`")
	}
	insecure := os.Getenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS") != ""
	if !insecure {
		if !strings.HasPrefix(rc.BaseURL, "https://") {
			return nil, fmt.Errorf("base URL %q does not use HTTPS — refusing to send API token in plaintext. Set OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS=1 to override", rc.BaseURL)
		}
		if !isAllowedHost(rc.BaseURL) {
			return nil, fmt.Errorf("base URL %q is not a recognized Omni domain — refusing to send API token. Set OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS=1 to override", rc.BaseURL)
		}
	}

	return rc, nil
}

// allowedDomains are the Omni domains the CLI will send API tokens to.
var allowedDomains = []string{
	".omniapp.co",
	".exploreomni.dev",
	".thundersalmon.com",
	".embed-omniapp.co",
	".embed-exploreomni.dev",
	".embed-thundersalmon.com",
}

// isAllowedHost checks whether the base URL's host is a recognized Omni domain.
func isAllowedHost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range allowedDomains {
		if host == domain[1:] || strings.HasSuffix(host, domain) {
			return true
		}
	}
	return false
}

// Load reads the config file from the default location.
func Load() (*Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes the config file to the default location.
func Save(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// configDir returns the omni-cli config directory following XDG conventions
// (like gh CLI): OMNI_CONFIG_DIR > XDG_CONFIG_HOME > ~/.config on Unix, %AppData% on Windows.
func configDir() string {
	if v := os.Getenv("OMNI_CONFIG_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "omni-cli")
	}
	if runtime.GOOS == "windows" {
		appData, _ := os.UserConfigDir() // %AppData%
		return filepath.Join(appData, "omni-cli")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "omni-cli")
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	if v := os.Getenv("OMNI_CONFIG_PATH"); v != "" {
		return v
	}
	return filepath.Join(configDir(), "config.json")
}

