package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// oauthFixture reads a server response body from the oauth package's testdata.
func oauthFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "oauth", "testdata", name+".json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return b
}

// deviceOrgServer is a fake org host that offers the device grant and
// approves on the first poll.
func deviceOrgServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{
			"/.well-known/oauth-authorization-server": "discovery_with_device",
			"/oauth/device_authorization":             "device_authorization",
			"/oauth/token":                            "device_token_success",
		}[r.URL.Path]
		if name == "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(oauthFixture(t, name))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// refusingServer fails the test on any request: --browser must not touch
// discovery or the device endpoints.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubBrowserLogin replaces the browser login for the test; fn runs in its
// place and returns the token to save.
func stubBrowserLogin(t *testing.T, fn func(endpoint string) (*oauth2.Token, error)) {
	t.Helper()
	stubBrowserLoginWithOpener(t, func(endpoint string, _ func(string)) (*oauth2.Token, error) {
		return fn(endpoint)
	})
}

// stubBrowserLoginWithOpener is stubBrowserLogin for a test that checks the
// opener the browser login is handed.
func stubBrowserLoginWithOpener(t *testing.T, fn func(endpoint string, open func(string)) (*oauth2.Token, error)) {
	t.Helper()
	prev := browserLogin
	browserLogin = fn
	t.Cleanup(func() { browserLogin = prev })
}

// rootWithConfig builds the parts of the real root a config command needs:
// the global flags (--format, --profile) and the config group.
func rootWithConfig(args ...string) *cobra.Command {
	root := &cobra.Command{Use: "omni", SilenceUsage: true, SilenceErrors: true}
	addGlobalFlags(root)
	addConfigCommands(root)
	root.SetArgs(args)
	return root
}

// clearOpenerEnv blanks the variables that keep loginOpener shut (CI, a
// coding agent's markers, SSH), so a test that expects a browser to open
// passes under CI and in an agent's shell. A test that wants one sets it
// after.
func clearOpenerEnv(t *testing.T) {
	t.Helper()
	for _, name := range append(append([]string{"CI"}, agentEnvVars...), sshEnvVars...) {
		t.Setenv(name, "")
	}
}

func loginTestEnv(t *testing.T) {
	t.Helper()
	clearOpenerEnv(t)
	t.Setenv("OMNI_CLI_DANGEROUSLY_ALLOW_INSECURE_REQUESTS", "1")
	t.Setenv("OMNI_OUTPUT_FORMAT", "")
	t.Setenv("OMNI_NO_BANNER", "1")
	prev := devicePollInterval
	devicePollInterval = time.Second
	t.Cleanup(func() { devicePollInterval = prev })
}

// `config login --format json` leaves exactly one JSON line on stdout, the
// device code, and saves the tokens the device grant returned.
func TestConfigLogin_FormatJSONWritesOneDeviceLine(t *testing.T) {
	loginTestEnv(t)
	srv := deviceOrgServer(t)
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		t.Error("browser login ran; the server offers the device grant")
		return nil, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {APIEndpoint: srv.URL, AuthMethod: "api-key", APIKey: "old-key"},
		},
	})

	var err error
	out := captureStdout(t, func() {
		err = rootWithConfig("config", "login", "prod", "--format", "json").Execute()
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout = %q, want exactly one line", out)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(out), &line); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", out, err)
	}
	for _, k := range []string{"verification_uri", "verification_uri_complete", "user_code", "expires_in"} {
		if _, ok := line[k]; !ok {
			t.Errorf("JSON line %v lacks %q", line, k)
		}
	}
	if len(line) != 4 {
		t.Errorf("JSON line %v, want exactly the four device fields", line)
	}
	if line["user_code"] != "BDKF-QRTW" {
		t.Errorf("user_code = %v, want BDKF-QRTW", line["user_code"])
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}
	p := cfg.Profiles["prod"]
	if p.AuthMethod != "oauth" || p.AccessToken != "device-access-token" || p.RefreshToken != "device-refresh-token" {
		t.Errorf("saved profile = %+v, want the device grant's OAuth tokens", p)
	}
}

func TestConfigLogin_BrowserFlagForcesBrowser(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	var gotEndpoint string
	stubBrowserLogin(t, func(endpoint string) (*oauth2.Token, error) {
		gotEndpoint = endpoint
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh", Expiry: time.Now().Add(time.Hour)}, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles:       map[string]config.Profile{"prod": {APIEndpoint: srv.URL, AuthMethod: "oauth"}},
	})

	var err error
	out := captureStdout(t, func() {
		err = rootWithConfig("config", "login", "--browser").Execute()
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if gotEndpoint != srv.URL {
		t.Errorf("browser login endpoint = %q, want %q", gotEndpoint, srv.URL)
	}
	if !strings.Contains(out, `Login successful! Profile "prod" updated.`) {
		t.Errorf("stdout = %q, want the success message", out)
	}
	cfg, _ := config.Load()
	if got := cfg.Profiles["prod"].AccessToken; got != "browser-access" {
		t.Errorf("saved access token = %q, want browser-access", got)
	}
}

// The tokens land on the config as it is on disk once the login finishes:
// a write another process made while the person was approving survives.
func TestConfigLogin_KeepsConcurrentConfigChanges(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod":  {APIEndpoint: srv.URL, AuthMethod: "oauth"},
			"other": {APIEndpoint: "https://other.omniapp.co", AuthMethod: "oauth", RefreshToken: "r1"},
		},
	})
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		// A refresh of another profile, mid-login.
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("loading config: %v", err)
		}
		other := cfg.Profiles["other"]
		other.RefreshToken = "r2"
		cfg.Profiles["other"] = other
		if err := config.Save(cfg); err != nil {
			t.Fatalf("saving config: %v", err)
		}
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
	})

	captureStdout(t, func() {
		if err := rootWithConfig("config", "login", "prod", "--browser").Execute(); err != nil {
			t.Errorf("login: %v", err)
		}
	})

	cfg, _ := config.Load()
	if got := cfg.Profiles["other"].RefreshToken; got != "r2" {
		t.Errorf("other profile's refresh token = %q, want r2 (login wrote back the config it read before logging in)", got)
	}
	if got := cfg.Profiles["prod"].AccessToken; got != "browser-access" {
		t.Errorf("prod access token = %q, want browser-access", got)
	}
}

func TestConfigInit_OAuthBrowserSavesProfile(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
	})
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "existing",
		Profiles:       map[string]config.Profile{"existing": {APIEndpoint: "https://x.omniapp.co", AuthMethod: "api-key", APIKey: "k"}},
	})

	captureStdout(t, func() {
		if err := rootWithConfig("config", "init", "--name", "prod", "--endpoint", srv.URL, "--auth", "oauth", "--browser").Execute(); err != nil {
			t.Errorf("init: %v", err)
		}
	})

	cfg, _ := config.Load()
	if got := cfg.Profiles["prod"]; got.AuthMethod != "oauth" || got.AccessToken != "browser-access" || got.APIEndpoint != srv.URL {
		t.Errorf("prod profile = %+v, want the OAuth tokens for %s", got, srv.URL)
	}
	if _, ok := cfg.Profiles["existing"]; !ok || cfg.DefaultProfile != "existing" {
		t.Errorf("config = %+v, want the existing profile and default kept", cfg)
	}
}

// The guards that keep a login from saving tokens onto a profile that moved
// under it while the person was approving.
func TestConfigLogin_ProfileChangedDuringLogin(t *testing.T) {
	cases := []struct {
		name   string
		change func(cfg *config.Config)
		want   string
	}{
		{"removed", func(cfg *config.Config) { delete(cfg.Profiles, "prod") }, `profile "prod" was removed during login`},
		{"endpoint changed", func(cfg *config.Config) {
			p := cfg.Profiles["prod"]
			p.APIEndpoint = "https://elsewhere.omniapp.co"
			cfg.Profiles["prod"] = p
		}, `profile "prod" changed endpoint during login; run login again`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loginTestEnv(t)
			srv := refusingServer(t)
			withConfig(t, &config.Config{
				Version:        1,
				DefaultProfile: "prod",
				Profiles:       map[string]config.Profile{"prod": {APIEndpoint: srv.URL, AuthMethod: "oauth"}},
			})
			stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
				cfg, _ := config.Load()
				tc.change(cfg)
				if err := config.Save(cfg); err != nil {
					t.Fatalf("saving config: %v", err)
				}
				return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
			})

			var err error
			captureStdout(t, func() { err = rootWithConfig("config", "login", "prod", "--browser").Execute() })
			if err == nil || err.Error() != tc.want {
				t.Fatalf("login error = %v, want %q", err, tc.want)
			}
			cfg, _ := config.Load()
			if p, ok := cfg.Profiles["prod"]; ok && p.AccessToken != "" {
				t.Errorf("prod profile = %+v, want no tokens saved", p)
			}
		})
	}
}

// The tokens go onto the profile as it is on disk after the login: a
// change to the same profile made meanwhile survives.
func TestConfigLogin_AppliesTokensToFreshProfile(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	withConfig(t, &config.Config{
		Version:        1,
		DefaultProfile: "prod",
		Profiles:       map[string]config.Profile{"prod": {APIEndpoint: srv.URL, AuthMethod: "api-key", APIKey: "old-key"}},
	})
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		cfg, _ := config.Load()
		p := cfg.Profiles["prod"]
		p.APIKey = "new-key"
		cfg.Profiles["prod"] = p
		if err := config.Save(cfg); err != nil {
			t.Fatalf("saving config: %v", err)
		}
		return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
	})

	captureStdout(t, func() {
		if err := rootWithConfig("config", "login", "prod", "--browser").Execute(); err != nil {
			t.Errorf("login: %v", err)
		}
	})
	cfg, _ := config.Load()
	p := cfg.Profiles["prod"]
	if p.APIKey != "new-key" || p.AccessToken != "browser-access" {
		t.Errorf("prod profile = %+v, want the concurrent APIKey change kept alongside the new tokens", p)
	}
}

// init refuses a config file it can't parse before running any login, and
// leaves the file alone.
func TestConfigInit_CorruptConfigFailsBeforeLogin(t *testing.T) {
	loginTestEnv(t)
	srv := refusingServer(t)
	path := withConfig(t, nil)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubBrowserLogin(t, func(string) (*oauth2.Token, error) {
		t.Error("login ran against a config file init can't save to")
		return &oauth2.Token{AccessToken: "a"}, nil
	})

	var err error
	captureStdout(t, func() {
		err = rootWithConfig("config", "init", "--name", "prod", "--endpoint", srv.URL, "--auth", "oauth", "--browser").Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "fix or delete "+path) {
		t.Fatalf("init error = %v, want a hint to fix or delete %s", err, path)
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Errorf("config file = %q, want it untouched", b)
	}
}

// The device login opens a browser only when stdin and stdout are both
// terminals, and so does --browser (terminalOpener).
func TestLoginOpener_RequiresBothTTYs(t *testing.T) {
	prevTerm, prevOpener := isTerminal, browserOpener
	t.Cleanup(func() { isTerminal, browserOpener = prevTerm, prevOpener })
	browserOpener = func() func(string) { return func(string) {} }

	stdin, stdout := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if stdin == stdout {
		t.Skip("stdin and stdout share a descriptor")
	}
	for _, tc := range []struct {
		stdinTTY, stdoutTTY, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	} {
		isTerminal = func(fd int) bool {
			if fd == stdin {
				return tc.stdinTTY
			}
			return fd == stdout && tc.stdoutTTY
		}
		if got := loginOpener(false, getenv(nil)) != nil; got != tc.want {
			t.Errorf("stdin TTY %v, stdout TTY %v: opener set = %v, want %v", tc.stdinTTY, tc.stdoutTTY, got, tc.want)
		}
		if got := terminalOpener() != nil; got != tc.want {
			t.Errorf("stdin TTY %v, stdout TTY %v: --browser opener set = %v, want %v", tc.stdinTTY, tc.stdoutTTY, got, tc.want)
		}
	}
	isTerminal = func(int) bool { return true }
	browserOpener = func() func(string) { return nil }
	if loginOpener(false, getenv(nil)) != nil || terminalOpener() != nil {
		t.Error("opener set with no browser to open")
	}
}

// Even at a terminal, the device login and the browser login it falls back
// to don't open a browser with JSON output, in CI or a coding agent's shell,
// or over SSH.
func TestLoginOpener_SkipsForJSONAgentCIAndSSH(t *testing.T) {
	prevTerm, prevOpener := isTerminal, browserOpener
	t.Cleanup(func() { isTerminal, browserOpener = prevTerm, prevOpener })
	isTerminal = func(int) bool { return true }
	browserOpener = func() func(string) { return func(string) {} }

	type tc struct {
		name    string
		jsonOut bool
		env     map[string]string
		want    bool
	}
	cases := []tc{
		{"a person at a terminal", false, nil, true},
		{"an ordinary terminal", false, map[string]string{"TERM": "xterm-256color", "OMNI_NO_BANNER": "1"}, true},
		{"--format json", true, nil, false},
		{"ci", false, map[string]string{"CI": "true"}, false},
		{"ssh connection", false, map[string]string{"SSH_CONNECTION": "203.0.113.5 52144 198.51.100.7 22"}, false},
		{"ssh client", false, map[string]string{"SSH_CLIENT": "203.0.113.5 52144 22"}, false},
		{"ssh tty", false, map[string]string{"SSH_TTY": "/dev/ttys004"}, false},
	}
	for _, name := range agentEnvVars {
		cases = append(cases, tc{name, false, map[string]string{name: "1"}, false})
	}
	for _, c := range cases {
		if got := loginOpener(c.jsonOut, getenv(c.env)) != nil; got != c.want {
			t.Errorf("%s: opener set = %v, want %v", c.name, got, c.want)
		}
	}
}

// The browser login, whether forced by --browser or fallen back to because
// the server doesn't offer the device grant, opens a browser only when stdin
// and stdout are both terminals.
func TestConfigLogin_BrowserLoginOpensOnlyOnATerminal(t *testing.T) {
	// 404s discovery, so login falls back to the browser login.
	noDeviceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(noDeviceSrv.Close)

	for _, flow := range []struct {
		name string
		args []string
	}{
		{"fallback", nil},
		{"--browser", []string{"--browser"}},
	} {
		for _, tty := range []struct {
			stdin, stdout, wantOpen bool
		}{
			{true, true, true},
			{true, false, false},
			{false, true, false},
			{false, false, false},
		} {
			name := fmt.Sprintf("%s/stdin TTY %v, stdout TTY %v", flow.name, tty.stdin, tty.stdout)
			t.Run(name, func(t *testing.T) {
				loginTestEnv(t)
				prevTerm, prevOpener := isTerminal, browserOpener
				t.Cleanup(func() { isTerminal, browserOpener = prevTerm, prevOpener })
				// captureStdout swaps os.Stdout, so any descriptor but stdin's
				// stands for stdout.
				stdin := int(os.Stdin.Fd())
				isTerminal = func(fd int) bool {
					if fd == stdin {
						return tty.stdin
					}
					return tty.stdout
				}
				var opened []string
				browserOpener = func() func(string) {
					return func(url string) { opened = append(opened, url) }
				}
				stubBrowserLoginWithOpener(t, func(_ string, open func(string)) (*oauth2.Token, error) {
					if open != nil {
						open("https://auth.example/authorize")
					}
					return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
				})
				withConfig(t, &config.Config{
					Version:        1,
					DefaultProfile: "prod",
					Profiles:       map[string]config.Profile{"prod": {APIEndpoint: noDeviceSrv.URL, AuthMethod: "oauth"}},
				})

				var err error
				captureStdout(t, func() {
					err = rootWithConfig(append([]string{"config", "login", "prod"}, flow.args...)...).Execute()
				})
				if err != nil {
					t.Fatalf("login: %v", err)
				}
				if got := len(opened) == 1; got != tty.wantOpen {
					t.Errorf("browser opened %v times, want opened = %v", len(opened), tty.wantOpen)
				}
			})
		}
	}
}

// At a terminal, --browser opens this machine's browser even in a coding
// agent's shell, in CI, over SSH or with --format json, where the browser
// login the CLI falls back to on its own leaves the URL for the person to
// open.
func TestConfigLogin_BrowserFlagOpensDespiteEnvGate(t *testing.T) {
	// 404s discovery, so login without --browser falls back to the browser login.
	noDeviceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(noDeviceSrv.Close)

	for _, gate := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"CLAUDECODE=1", map[string]string{"CLAUDECODE": "1"}, nil},
		{"SSH_TTY", map[string]string{"SSH_TTY": "/dev/ttys004"}, nil},
		{"CI", map[string]string{"CI": "true"}, nil},
		{"--format json", nil, []string{"--format", "json"}},
	} {
		for _, flow := range []struct {
			name     string
			args     []string
			wantOpen bool
		}{
			{"fallback", nil, false},
			{"--browser", []string{"--browser"}, true},
		} {
			t.Run(gate.name+"/"+flow.name, func(t *testing.T) {
				loginTestEnv(t)
				for k, v := range gate.env {
					t.Setenv(k, v)
				}
				prevTerm, prevOpener := isTerminal, browserOpener
				t.Cleanup(func() { isTerminal, browserOpener = prevTerm, prevOpener })
				isTerminal = func(int) bool { return true }
				var opened []string
				browserOpener = func() func(string) {
					return func(url string) { opened = append(opened, url) }
				}
				stubBrowserLoginWithOpener(t, func(_ string, open func(string)) (*oauth2.Token, error) {
					if open != nil {
						open("https://auth.example/authorize")
					}
					return &oauth2.Token{AccessToken: "browser-access", RefreshToken: "browser-refresh"}, nil
				})
				withConfig(t, &config.Config{
					Version:        1,
					DefaultProfile: "prod",
					Profiles:       map[string]config.Profile{"prod": {APIEndpoint: noDeviceSrv.URL, AuthMethod: "oauth"}},
				})

				args := append(append([]string{"config", "login", "prod"}, gate.args...), flow.args...)
				var err error
				captureStdout(t, func() { err = rootWithConfig(args...).Execute() })
				if err != nil {
					t.Fatalf("login: %v", err)
				}
				if got := len(opened) == 1; got != flow.wantOpen {
					t.Errorf("browser opened %v times, want opened = %v", len(opened), flow.wantOpen)
				}
			})
		}
	}
}
