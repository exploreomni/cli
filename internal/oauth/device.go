package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"
	"unicode"

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

// defaultDeviceAuthRequestTimeout bounds the device authorization request.
const defaultDeviceAuthRequestTimeout = 30 * time.Second

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
	// OpenBrowser, when set, is called with the verification URL, and by
	// LoginDefault with the browser login's authorization URL. Leave it nil
	// when no browser can or should be opened.
	OpenBrowser func(url string)
	// Interval overrides the server's polling interval when positive. x/oauth2
	// polls in whole seconds, so it is rounded up to one.
	Interval time.Duration
	// Timeout caps the whole login; zero means ten minutes.
	Timeout time.Duration
	// RequestTimeout bounds the device authorization request; zero means
	// 30 seconds.
	RequestTimeout time.Duration
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
	client.Timeout = opts.RequestTimeout
	if client.Timeout <= 0 {
		client.Timeout = defaultDeviceAuthRequestTimeout
	}
	conf := DeviceConfig(apiEndpoint)
	da, err := conf.DeviceAuth(context.WithValue(ctx, oauth2.HTTPClient, client))
	if err != nil {
		return nil, deviceAuthError(apiEndpoint, err)
	}
	if da.DeviceCode == "" || da.UserCode == "" || da.VerificationURI == "" {
		return nil, fmt.Errorf("requesting a device code: the response is missing device_code, user_code or verification_uri")
	}
	if !printableASCII(da.UserCode) {
		return nil, fmt.Errorf("requesting a device code: the server sent a user code that isn't printable ASCII")
	}
	// Server text bound for the terminal can't carry escape sequences.
	verificationURI := stripControl(da.VerificationURI)
	verificationURIComplete := stripControl(da.VerificationURIComplete)

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
	// The complete URL carries the code, so a copied one (over SSH, where
	// nothing is opened) lands with the code filled in; the person checks it
	// against the one shown here. It is shown only on the terms it would be
	// opened, checked before stripping: a URL that loses control characters,
	// or breaks at whitespace when copied, carries a different code.
	link, step := verificationURI, "enter code"
	if openableVerificationURL(da.VerificationURIComplete, apiEndpoint) {
		link, step = verificationURIComplete, "check the page shows code"
	}
	fmt.Fprintf(stderr, "To sign in, open %s\nand %s %s\nWaiting for approval (expires in %s)…\n",
		link, step, da.UserCode, shortDuration(expiresIn))

	if opts.JSON != nil {
		enc := json.NewEncoder(opts.JSON)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(deviceLine{
			VerificationURI:         verificationURI,
			VerificationURIComplete: verificationURIComplete,
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
		// The URL comes from the server and goes to an OS command.
		if openableVerificationURL(target, apiEndpoint) {
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

// openableVerificationURL reports whether target, a URL the server sent, may
// be handed to the OS browser opener: it must be on apiEndpoint's own host
// and port, over https (or http when apiEndpoint itself is http, as in local
// development), with no userinfo and no whitespace or control characters.
func openableVerificationURL(target, apiEndpoint string) bool {
	for _, r := range target {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Opaque != "" {
		return false
	}
	base, err := url.Parse(apiEndpoint)
	if err != nil {
		return false
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && base.Scheme == "http":
	default:
		return false
	}
	return u.Hostname() != "" && strings.EqualFold(u.Hostname(), base.Hostname()) && effectivePort(u) == effectivePort(base)
}

// effectivePort is u's port, or its scheme's default.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "http" {
		return "80"
	}
	return "443"
}

// stripControl removes control characters (escape sequences start with one)
// from server text before it reaches the terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// printableASCII reports whether s is non-empty printable ASCII.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return s != ""
}

// deviceAuthError turns a failed device authorization request into the
// message the person sees. No code has been shown yet, so a timeout here is
// the request's, not the code's.
func deviceAuthError(apiEndpoint string, err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.Response != nil && re.Response.StatusCode == http.StatusNotFound {
			return ErrDeviceUnsupported
		}
		return deviceError("requesting a device code", err)
	}
	host := apiEndpoint
	if u, perr := url.Parse(apiEndpoint); perr == nil && u.Host != "" {
		host = u.Host
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return fmt.Errorf("timed out requesting a device code from %s", host)
	}
	return fmt.Errorf("requesting a device code from %s: %w", host, err)
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

// notOffered is the fallback note when the org doesn't offer the device grant.
const notOffered = "This Omni instance doesn't offer device login; using browser login."

// discoverDeviceLogin reports whether apiEndpoint's RFC 8414 discovery
// document lists a device_authorization_endpoint. When it doesn't, fallback
// is the note to show before the browser login: the instance doesn't offer
// device login (no field, or no document), or the document couldn't be read
// (another status, or not JSON). A 403 is returned as ErrForbidden, since the
// login requests would be refused the same way.
func discoverDeviceLogin(ctx context.Context, apiEndpoint string) (advertised bool, fallback string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	docURL := strings.TrimRight(apiEndpoint, "/") + discoveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := useragent.Client().Do(req)
	if err != nil {
		return false, "", fmt.Errorf("reading %s: %w", discoveryPath, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return false, "", ErrForbidden
	case http.StatusNotFound:
		return false, notOffered, nil
	default:
		return false, fmt.Sprintf("Couldn't read the login settings at %s (HTTP %d); falling back to browser login.", discoveryPath, resp.StatusCode), nil
	}
	var doc struct {
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return false, fmt.Sprintf("Couldn't read the login settings at %s (not valid JSON); falling back to browser login.", discoveryPath), nil
	}
	if doc.DeviceAuthorizationEndpoint == "" {
		return false, notOffered, nil
	}
	return true, "", nil
}

// LoginOptions selects and configures the login flow for LoginDefault.
type LoginOptions struct {
	// Browser skips the device grant and runs the browser login.
	Browser bool
	// Device configures the device grant.
	Device DeviceOptions
	// BrowserLogin runs the browser login with Device.OpenBrowser as its
	// opener; nil means Login.
	BrowserLogin func(apiEndpoint string, open func(url string)) (*oauth2.Token, error)
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
		return browserLogin(apiEndpoint, opts.Device.OpenBrowser)
	}

	stderr := opts.Device.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	advertised, fallback, err := discoverDeviceLogin(ctx, apiEndpoint)
	if err != nil {
		return nil, err
	}
	if advertised {
		tok, err := DeviceLogin(ctx, apiEndpoint, opts.Device)
		if !errors.Is(err, ErrDeviceUnsupported) {
			return tok, err
		}
		fallback = notOffered
	}
	fmt.Fprintln(stderr, fallback)
	return browserLogin(apiEndpoint, opts.Device.OpenBrowser)
}

// BrowserOpener returns a function that opens a URL in the default browser,
// or nil when there is no opener to run: the platform's opener command isn't
// installed, or, on Linux and the BSDs, there is no display to open it on.
func BrowserOpener() func(string) {
	switch runtime.GOOS {
	case "darwin", "windows":
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
	}
	// exec.Command records a failed PATH lookup in Err.
	if browserCommand(runtime.GOOS, "").Err != nil {
		return nil
	}
	return openBrowser
}
