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
	"time"

	"github.com/exploreomni/omni-cli/internal/oauth"
)

// Profile represents a saved API configuration.
type Profile struct {
	APIEndpoint    string `json:"apiEndpoint"`
	AuthMethod     string `json:"authMethod"`
	APIKey         string `json:"apiKey,omitempty"`
	AccessToken    string `json:"accessToken,omitempty"`
	RefreshToken   string `json:"refreshToken,omitempty"`
	TokenExpiresAt string `json:"tokenExpiresAt,omitempty"`
}

// IsTokenExpiringSoon returns true if the given RFC 3339 expiration time
// is within bufferSeconds of now (or unparseable).
func IsTokenExpiringSoon(expiresAt string, bufferSeconds int) bool {
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return time.Until(t) < time.Duration(bufferSeconds)*time.Second
}

// Config is the on-disk config file format (compatible with the TS CLI).
type Config struct {
	Version             int                `json:"version"`
	DefaultProfile      string             `json:"defaultProfile,omitempty"`
	DefaultOutputFormat string             `json:"defaultOutputFormat,omitempty"`
	Profiles            map[string]Profile `json:"profiles"`
}

// Output format values.
const (
	FormatAuto  = "auto"
	FormatJSON  = "json"
	FormatHuman = "human"
)

// ValidOutputFormat reports whether s is a recognized output format name.
func ValidOutputFormat(s string) bool {
	switch s {
	case FormatAuto, FormatJSON, FormatHuman:
		return true
	}
	return false
}

// ResolveOutputFormat picks the effective output format.
// Precedence: flag > OMNI_OUTPUT_FORMAT env > config file > auto(TTY).
// An "auto" result from any layer resolves to "human" when isTTY, else "json".
func ResolveOutputFormat(flagValue string, isTTY bool) string {
	chosen := ""
	if flagValue != "" {
		chosen = flagValue
	} else if v := os.Getenv("OMNI_OUTPUT_FORMAT"); v != "" {
		chosen = v
	} else if cfg, _ := Load(); cfg != nil && cfg.DefaultOutputFormat != "" {
		chosen = cfg.DefaultOutputFormat
	}
	if chosen == "" || chosen == FormatAuto {
		if isTTY {
			return FormatHuman
		}
		return FormatJSON
	}
	return chosen
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
	var profile *Profile
	if cfg != nil {
		name := profileName
		if name == "" {
			name = cfg.DefaultProfile
		}
		if name != "" {
			if p, ok := cfg.Profiles[name]; ok {
				profile = &p
				rc.BaseURL = p.APIEndpoint
				switch p.AuthMethod {
				case "oauth":
					rc.Token = p.AccessToken
				default: // "api-key"
					rc.Token = p.APIKey
				}
			}
		}
	}

	// Env vars override config file
	if v := os.Getenv("OMNI_API_TOKEN"); v != "" {
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

	// Auto-refresh OAuth tokens expiring within 5 minutes
	if profile != nil && profile.AuthMethod == "oauth" && profile.TokenExpiresAt != "" {
		if IsTokenExpiringSoon(profile.TokenExpiresAt, 300) && profile.RefreshToken != "" {
			refreshed, err := oauth.RefreshAccessToken(profile.APIEndpoint, profile.RefreshToken)
			if err == nil {
				profile.AccessToken = refreshed.AccessToken
				profile.RefreshToken = refreshed.RefreshToken
				profile.TokenExpiresAt = time.Now().Add(
					time.Duration(refreshed.ExpiresIn) * time.Second,
				).Format(time.RFC3339)
				name := profileName
				if name == "" {
					name = cfg.DefaultProfile
				}
				cfg.Profiles[name] = *profile
				_ = Save(cfg)
				rc.Token = refreshed.AccessToken
			}
			// If refresh fails, continue with stale token — let the API call fail with 401
		}
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

