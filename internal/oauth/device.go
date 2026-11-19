package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/useragent"
	"golang.org/x/oauth2"
)

// Org-host paths for the RFC 8628 device authorization grant. The discovery
// document is read only to learn whether the org offers the grant; requests
// always go to these fixed paths on the profile's own endpoint.
const (
	deviceAuthorizationPath = "/oauth/device_authorization"
	discoveryPath           = "/.well-known/oauth-authorization-server"
)

// cliLoginDisabledDescription is the error_description the server sends with
// access_denied when the org has turned CLI login off. Kept in step with
// testdata/device_token_cli_login_disabled.json.
const cliLoginDisabledDescription = "This organization does not allow CLI login"

// defaultDeviceTimeout caps the whole device login, whatever expires_in says.
const defaultDeviceTimeout = 10 * time.Minute

// deviceAuthRequestTimeout bounds the device authorization request.
const deviceAuthRequestTimeout = 30 * time.Second

// Device login failures callers (and tests) can tell apart with errors.Is.
var (
	// ErrDeviceUnsupported means the server has no device authorization
	// endpoint; the caller should fall back to the browser login.
	ErrDeviceUnsupported = errors.New("device login is not available on this Omni instance")
	ErrCLILoginDisabled  = errors.New("this organization has CLI login turned off")
	ErrAccessDenied      = errors.New("login was denied")
	ErrCodeExpired       = errors.New("code expired, run login again")
	ErrForbidden         = errors.New("the Omni instance refused the request (HTTP 403); the organization's IP allowlist likely blocks this machine's IP address")
	ErrCancelled         = errors.New("login cancelled")
)

// DeviceConfig builds the oauth2.Config for the device grant: Config's client
// and endpoints plus the device authorization endpoint. The client id goes in
// the form body (AuthStyleInParams), which is what RFC 8628 §3.4 shows and
// what a public client sends; leaving x/oauth2 to auto-detect would send
// every authorization_pending poll twice, once per style.
func DeviceConfig(apiEndpoint string) *oauth2.Config {
	conf := Config(apiEndpoint, "")
	conf.Endpoint.DeviceAuthURL = strings.TrimRight(apiEndpoint, "/") + deviceAuthorizationPath
	conf.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	return conf
}

// DeviceOptions controls how DeviceLogin talks to the person signing in.
type DeviceOptions struct {
	// Stderr receives the human instructions (URL, code, expiry).
	Stderr io.Writer
	// JSON, when set, receives one JSON line with the verification URI, user
	// code and expiry before polling starts.
	JSON io.Writer
	// OpenBrowser, when set, is called with the verification URL. Leave it
	// nil when no browser can or should be opened.
	OpenBrowser func(url string)
	// Interval overrides the server's polling interval when positive. x/oauth2
	// polls in whole seconds, so it is rounded up to one.
	Interval time.Duration
	// Timeout caps the whole login; zero means ten minutes.
	Timeout time.Duration
}

// deviceLine is the JSON line written to DeviceOptions.JSON.
type deviceLine struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	UserCode                string `json:"user_code"`
	ExpiresIn               int64  `json:"expires_in"`
}

// DeviceLogin runs the RFC 8628 device authorization grant against
// apiEndpoint: it requests a device code, tells the person where to enter it,
// and polls the token endpoint until they approve, deny, or the code expires.
// Ctrl-C while polling cancels the wait. It returns ErrDeviceUnsupported when the server
// answers the device authorization request with 404.
func DeviceLogin(ctx context.Context, apiEndpoint string, opts DeviceOptions) (*oauth2.Token, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDeviceTimeout
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// x/oauth2 sends the device authorization request without ctx, so it
	// gets a client timeout of its own; Ctrl-C during it still exits the
	// process the default way.
	client := useragent.Client()
	client.Timeout = deviceAuthRequestTimeout
	conf := DeviceConfig(apiEndpoint)
	da, err := conf.DeviceAuth(context.WithValue(ctx, oauth2.HTTPClient, client))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil && re.Response.StatusCode == http.StatusNotFound {
			return nil, ErrDeviceUnsupported
		}
		return nil, deviceError("requesting a device code", err)
	}
	if da.DeviceCode == "" || da.UserCode == "" || da.VerificationURI == "" {
		return nil, fmt.Errorf("requesting a device code: the response is missing device_code, user_code or verification_uri")
	}

	// The code lives until the earlier of the server's expiry and our cap.
	// x/oauth2 dates expires_in from when the request was sent, so measuring
	// from start gives back the server's own figure.
	deadline := start.Add(timeout)
	if da.Expiry.IsZero() || da.Expiry.After(deadline) {
		da.Expiry = deadline
	}
	expiresIn := da.Expiry.Sub(start).Round(time.Second)

	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	fmt.Fprintf(stderr, "To sign in, open %s\nand enter code %s\nWaiting for approval (expires in %s)…\n",
		da.VerificationURI, da.UserCode, shortDuration(expiresIn))

	if opts.JSON != nil {
		enc := json.NewEncoder(opts.JSON)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(deviceLine{
			VerificationURI:         da.VerificationURI,
			VerificationURIComplete: da.VerificationURIComplete,
			UserCode:                da.UserCode,
			ExpiresIn:               int64(expiresIn / time.Second),
		}); err != nil {
			return nil, fmt.Errorf("writing the device code: %w", err)
		}
	}

	if opts.OpenBrowser != nil {
		target := da.VerificationURIComplete
		if target == "" {
			target = da.VerificationURI
		}
		// The URL comes from the server and goes to an OS command; open
		// only a web URL.
		if u, err := url.Parse(target); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
			opts.OpenBrowser(target)
		}
	}

	if opts.Interval > 0 {
		da.Interval = int64(math.Ceil(opts.Interval.Seconds()))
	}

	ctx, stop := signal.NotifyContext(Context(ctx), os.Interrupt)
	defer stop()
	tok, err := conf.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, deviceError("waiting for approval", err)
	}
	return tok, nil
}

// deviceError turns a device-grant failure into the message the person sees.
func deviceError(step string, err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		switch re.ErrorCode {
		case "access_denied":
			if re.ErrorDescription == cliLoginDisabledDescription {
				return fmt.Errorf("%w. A personal access token still works: run `omni config init --auth api-key`, or set OMNI_API_TOKEN", ErrCLILoginDisabled)
			}
			return ErrAccessDenied
		case "expired_token":
			return ErrCodeExpired
		}
		if re.Response != nil && re.Response.StatusCode == http.StatusForbidden {
			return ErrForbidden
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrCodeExpired
	}
	return fmt.Errorf("%s: %w", step, err)
}

// shortDuration formats d the way the prompt shows it: 10m, 1m30s, 45s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// DeviceLoginAdvertised reports whether apiEndpoint's RFC 8414 discovery
// document lists a device_authorization_endpoint. A missing or unreadable
// document reads as "not advertised", so the caller falls back to the
// browser login; a 403 is returned as ErrForbidden, since the login requests
// would be refused the same way.
func DeviceLoginAdvertised(ctx context.Context, apiEndpoint string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	docURL := strings.TrimRight(apiEndpoint, "/") + discoveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := useragent.Client().Do(req)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", discoveryPath, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return false, ErrForbidden
	case resp.StatusCode != http.StatusOK:
		return false, nil
	}
	var doc struct {
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return false, nil
	}
	return doc.DeviceAuthorizationEndpoint != "", nil
}

// LoginOptions selects and configures the login flow for LoginDefault.
type LoginOptions struct {
	// Browser skips the device grant and runs the browser login.
	Browser bool
	// Device configures the device grant.
	Device DeviceOptions
	// BrowserLogin runs the browser login; nil means Login.
	BrowserLogin func(apiEndpoint string) (*oauth2.Token, error)
}

// LoginDefault logs in with the device grant when the server offers it, and
// with the browser login (localhost callback + PKCE) when it doesn't, when
// the device authorization endpoint answers 404, or when opts.Browser is set.
func LoginDefault(ctx context.Context, apiEndpoint string, opts LoginOptions) (*oauth2.Token, error) {
	browserLogin := opts.BrowserLogin
	if browserLogin == nil {
		browserLogin = Login
	}
	if opts.Browser {
		return browserLogin(apiEndpoint)
	}

	stderr := opts.Device.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	advertised, err := DeviceLoginAdvertised(ctx, apiEndpoint)
	if err != nil {
		return nil, err
	}
	if advertised {
		tok, err := DeviceLogin(ctx, apiEndpoint, opts.Device)
		if !errors.Is(err, ErrDeviceUnsupported) {
			return tok, err
		}
	}
	fmt.Fprintln(stderr, "This Omni instance doesn't offer device login; using browser login.")
	return browserLogin(apiEndpoint)
}

// BrowserOpener returns a function that opens a URL in the default browser,
// or nil when there is no opener to run: the platform's opener command isn't
// installed, or, on Linux and the BSDs, there is no display to open it on.
func BrowserOpener() func(string) {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "cmd"
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
	}
	if _, err := exec.LookPath(name); err != nil {
		return nil
	}
	return openBrowser
}
