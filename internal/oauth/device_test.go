package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fixture returns a server response body from testdata. The files hold the
// Omni server's bodies verbatim so they can be checked against it.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return b
}

// tokenPoll is one request the fake server saw at its token endpoint.
type tokenPoll struct {
	at   time.Time
	form url.Values
	auth string
}

// deviceServer is a fake Omni org host for the device grant.
type deviceServer struct {
	*httptest.Server

	// Set before start.
	discovery    string   // fixture for the discovery document; "" answers 404, "503" answers 503, "garbage" answers non-JSON
	deviceStatus int      // device authorization status; 0 answers 200 with the fixture
	tokens       []string // fixtures for successive token polls; the last repeats; "403" answers 403
	deviceBody   string   // replaces the device authorization fixture when set; BASE becomes the server's URL
	deviceDelay  time.Duration
	onPoll       func(n int)

	mu          sync.Mutex
	discoveries int
	deviceForms []url.Values
	polls       []tokenPoll
}

func (s *deviceServer) start(t *testing.T) *deviceServer {
	t.Helper()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		switch r.URL.Path {
		case discoveryPath:
			s.discoveries++
			s.mu.Unlock()
			switch s.discovery {
			case "":
				http.NotFound(w, r)
				return
			case "503":
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			case "garbage":
				_, _ = w.Write([]byte("<html>maintenance</html>"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, s.discovery))
		case deviceAuthorizationPath:
			s.deviceForms = append(s.deviceForms, r.PostForm)
			s.mu.Unlock()
			time.Sleep(s.deviceDelay)
			if s.deviceStatus != 0 {
				http.Error(w, http.StatusText(s.deviceStatus), s.deviceStatus)
				return
			}
			body := fixture(t, "device_authorization")
			if s.deviceBody != "" {
				body = []byte(strings.ReplaceAll(s.deviceBody, "BASE", s.URL))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case "/oauth/token":
			s.polls = append(s.polls, tokenPoll{at: time.Now(), form: r.PostForm, auth: r.Header.Get("Authorization")})
			n := len(s.polls)
			s.mu.Unlock()
			if s.onPoll != nil {
				s.onPoll(n)
			}
			name := s.tokens[min(n, len(s.tokens))-1]
			switch {
			case name == "403":
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			case name != "device_token_success":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
			default:
				w.Header().Set("Content-Type", "application/json")
			}
			_, _ = w.Write(fixture(t, name))
		default:
			s.mu.Unlock()
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *deviceServer) snapshot() (discoveries int, deviceForms []url.Values, polls []tokenPoll) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.discoveries, append([]url.Values(nil), s.deviceForms...), append([]tokenPoll(nil), s.polls...)
}

// syncBuffer is a bytes.Buffer safe to read from the server's goroutine
// while DeviceLogin writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestDeviceConfig_BuildsExpectedURLs(t *testing.T) {
	c := DeviceConfig("https://acme.omniapp.co/")
	if got, want := c.Endpoint.DeviceAuthURL, "https://acme.omniapp.co/oauth/device_authorization"; got != want {
		t.Errorf("DeviceAuthURL = %q, want %q", got, want)
	}
	if got, want := c.Endpoint.TokenURL, "https://acme.omniapp.co/oauth/token"; got != want {
		t.Errorf("TokenURL = %q, want %q", got, want)
	}
	if got, want := c.Endpoint.AuthURL, "https://acme.omniapp.co/oauth/authorize"; got != want {
		t.Errorf("AuthURL = %q, want %q", got, want)
	}
	if c.ClientID != "omni-cli" {
		t.Errorf("ClientID = %q, want omni-cli", c.ClientID)
	}
	if c.Endpoint.AuthStyle != oauth2.AuthStyleInParams {
		t.Errorf("AuthStyle = %v, want AuthStyleInParams", c.Endpoint.AuthStyle)
	}
}

// Pending, then slow_down, then success: the token comes back, every poll is
// a single well-formed device_code grant, and slow_down stretches the gap
// before the next poll by 5 seconds (RFC 8628 §3.5). Takes about 8s: x/oauth2
// polls in whole seconds and adds a fixed 5s on slow_down.
func TestDeviceLogin_PendingSlowDownSuccess(t *testing.T) {
	t.Parallel()
	srv := (&deviceServer{tokens: []string{
		"device_token_authorization_pending",
		"device_token_slow_down",
		"device_token_success",
	}}).start(t)

	var stderr syncBuffer
	tok, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Stderr: &stderr, Interval: time.Second})
	if err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}
	if tok.AccessToken != "device-access-token" || tok.RefreshToken != "device-refresh-token" {
		t.Errorf("token = %q/%q, want the fixture's access and refresh tokens", tok.AccessToken, tok.RefreshToken)
	}
	if tok.Expiry.IsZero() {
		t.Error("token expiry not set from expires_in")
	}

	// The fixture's URLs are on another host than the test server, so the
	// prompt asks for the code to be entered.
	wantPrompt := "To sign in, open https://acme.omniapp.co/oauth/device\nand enter code BDKF-QRTW\nWaiting for approval (expires in 10m)…\n"
	if got := stderr.String(); got != wantPrompt {
		t.Errorf("stderr = %q, want %q", got, wantPrompt)
	}

	_, deviceForms, polls := srv.snapshot()
	if len(deviceForms) != 1 {
		t.Fatalf("device authorization requests = %d, want 1", len(deviceForms))
	}
	if got := deviceForms[0].Get("client_id"); got != "omni-cli" {
		t.Errorf("device authorization client_id = %q, want omni-cli", got)
	}
	if got := deviceForms[0].Get("scope"); got != "user:default" {
		t.Errorf("device authorization scope = %q, want user:default", got)
	}
	if len(polls) != 3 {
		t.Fatalf("token polls = %d, want 3 (one request per poll, no auth-style retry)", len(polls))
	}
	for i, p := range polls {
		if got := p.form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("poll %d grant_type = %q", i, got)
		}
		if got := p.form.Get("device_code"); got != "GmRhmhcxhwAzkoEqiMEg_DnyEysNkuNhszIySk9eS" {
			t.Errorf("poll %d device_code = %q", i, got)
		}
		if got := p.form.Get("client_id"); got != "omni-cli" {
			t.Errorf("poll %d client_id = %q, want omni-cli", i, got)
		}
		if p.auth != "" {
			t.Errorf("poll %d sent an Authorization header %q; a public client sends client_id in the body", i, p.auth)
		}
	}
	before := polls[1].at.Sub(polls[0].at)
	after := polls[2].at.Sub(polls[1].at)
	if before > 3*time.Second {
		t.Errorf("gap before slow_down = %v, want the 1s override", before)
	}
	if after < before+4500*time.Millisecond {
		t.Errorf("gap after slow_down = %v, want at least 5s more than %v", after, before)
	}
}

func TestDeviceLogin_TokenErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		token   string
		want    error
		message []string
	}{
		{"expired", "device_token_expired_token", ErrCodeExpired, []string{"code expired, run login again"}},
		{"denied", "device_token_access_denied", ErrAccessDenied, []string{"login was denied"}},
		{"cli login disabled", "device_token_cli_login_disabled", ErrCLILoginDisabled, []string{
			"CLI login turned off", "personal access token", "omni config init --auth api-key", "OMNI_API_TOKEN",
		}},
		{"forbidden", "403", ErrForbidden, []string{"IP allowlist"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := (&deviceServer{tokens: []string{tc.token}}).start(t)
			_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Interval: time.Second})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			for _, m := range tc.message {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not mention %q", err, m)
				}
			}
			if tc.want != ErrCLILoginDisabled && strings.Contains(err.Error(), "CLI login") {
				t.Errorf("error %q blames the org's CLI login setting", err)
			}
		})
	}
}

// A 403 on the device authorization request is the org's IP allowlist, not
// a missing feature: no fallback, and no polling.
func TestDeviceLogin_DeviceAuthorizationForbidden(t *testing.T) {
	t.Parallel()
	srv := (&deviceServer{deviceStatus: http.StatusForbidden}).start(t)
	_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Interval: time.Second})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// The code running out on our side (the server never answering anything but
// authorization_pending) reads the same as the server's expired_token.
func TestDeviceLogin_TimeoutReadsAsExpired(t *testing.T) {
	t.Parallel()
	srv := (&deviceServer{tokens: []string{"device_token_authorization_pending"}}).start(t)
	_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Interval: time.Second, Timeout: 1500 * time.Millisecond})
	if !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("err = %v, want ErrCodeExpired", err)
	}
}

func TestDeviceLogin_CancelStopsPolling(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := (&deviceServer{
		tokens: []string{"device_token_authorization_pending"},
		onPoll: func(int) { cancel() },
	}).start(t)
	_, err := DeviceLogin(ctx, srv.URL, DeviceOptions{Interval: time.Second})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if _, _, polls := srv.snapshot(); len(polls) != 1 {
		t.Errorf("token polls = %d, want 1 (none after cancel)", len(polls))
	}
}

// The JSON line is written, whole and alone, before the first poll, and the
// fixture's URL, on another host than the test server, is not opened.
func TestDeviceLogin_JSONLineBeforePolling(t *testing.T) {
	t.Parallel()
	var jsonOut syncBuffer
	var atFirstPoll string
	srv := (&deviceServer{
		tokens: []string{"device_token_success"},
		onPoll: func(n int) {
			if n == 1 {
				atFirstPoll = jsonOut.String()
			}
		},
	}).start(t)

	var opened []string
	_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{
		JSON:        &jsonOut,
		OpenBrowser: func(u string) { opened = append(opened, u) },
		Interval:    time.Second,
	})
	if err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}

	out := jsonOut.String()
	if atFirstPoll != out {
		t.Errorf("JSON output at first poll = %q, want the complete line %q", atFirstPoll, out)
	}
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("JSON output = %q, want exactly one line", out)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(out), &line); err != nil {
		t.Fatalf("JSON line %q: %v", out, err)
	}
	want := map[string]any{
		"verification_uri":          "https://acme.omniapp.co/oauth/device",
		"verification_uri_complete": "https://acme.omniapp.co/oauth/device?user_code=BDKF-QRTW",
		"user_code":                 "BDKF-QRTW",
		"expires_in":                float64(600),
	}
	if len(line) != len(want) {
		t.Errorf("JSON line keys = %v, want exactly %v", line, want)
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("JSON %s = %v, want %v", k, line[k], v)
		}
	}
	if len(opened) != 0 {
		t.Errorf("opened %v, want nothing: the URL is not on the endpoint's host", opened)
	}
}

// A slow device authorization response doesn't eat into the expiry the
// person is shown: it is the server's expires_in.
func TestDeviceLogin_ExpiryIgnoresRequestLatency(t *testing.T) {
	t.Parallel()
	var jsonOut, stderr syncBuffer
	srv := (&deviceServer{tokens: []string{"device_token_success"}, deviceDelay: 700 * time.Millisecond}).start(t)
	if _, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Stderr: &stderr, JSON: &jsonOut, Interval: time.Second}); err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}
	if !strings.Contains(stderr.String(), "(expires in 10m)") {
		t.Errorf("stderr = %q, want expires in 10m", stderr.String())
	}
	if !strings.Contains(jsonOut.String(), `"expires_in":600`) {
		t.Errorf("JSON = %q, want expires_in 600", jsonOut.String())
	}
}

// The prompt shows verification_uri_complete, so a copied URL opens with the
// code filled in, and the opener gets it unchanged, only when it is on the
// endpoint's own host and free of whitespace and control characters.
// Otherwise the prompt shows verification_uri and asks for the code to be
// entered; the opener gets verification_uri only when there is no complete
// URL at all.
func TestDeviceLogin_ShowsAndOpensOnlySameHostURLs(t *testing.T) {
	t.Parallel()
	body := func(uri, complete string) string {
		b, _ := json.Marshal(map[string]any{
			"device_code": "d", "user_code": "BDKF-QRTW", "expires_in": 600, "interval": 5,
			"verification_uri": uri, "verification_uri_complete": complete,
		})
		return string(b)
	}
	const (
		checkCode = "\nand check the page shows code BDKF-QRTW\nWaiting for approval (expires in 10m)…\n"
		enterCode = "\nand enter code BDKF-QRTW\nWaiting for approval (expires in 10m)…\n"
	)
	cases := []struct {
		name, uri, complete, wantOpened, wantPrompt string
	}{
		{"same host", "BASE/oauth/device", "BASE/oauth/device?user_code=BDKF-QRTW", "BASE/oauth/device?user_code=BDKF-QRTW", "To sign in, open BASE/oauth/device?user_code=BDKF-QRTW" + checkCode},
		{"same host, query with &", "BASE/oauth/device", `BASE/oauth/device?user_code=X&\\evil\share\p.exe`, `BASE/oauth/device?user_code=X&\\evil\share\p.exe`, `To sign in, open BASE/oauth/device?user_code=X&\\evil\share\p.exe` + checkCode},
		{"falls back to verification_uri", "BASE/oauth/device", "", "BASE/oauth/device", "To sign in, open BASE/oauth/device" + enterCode},
		{"foreign host", "BASE/oauth/device", "https://evil.example/oauth/device?user_code=X", "", "To sign in, open BASE/oauth/device" + enterCode},
		{"control character", "BASE/oauth/device", "BASE/oauth/device?user_code=X\u001b[2J", "", "To sign in, open BASE/oauth/device" + enterCode},
		{"newline", "BASE/oauth/device", "BASE/oauth/device?user_code=X\nfoo", "", "To sign in, open BASE/oauth/device" + enterCode},
		{"space", "BASE/oauth/device", "BASE/oauth/device?user_code=X Y", "", "To sign in, open BASE/oauth/device" + enterCode},
		{"not a web URL", "BASE/oauth/device", "calc.exe & whoami", "", "To sign in, open BASE/oauth/device" + enterCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := (&deviceServer{tokens: []string{"device_token_success"}, deviceBody: body(tc.uri, tc.complete)}).start(t)
			var mu sync.Mutex
			var opened []string
			var stderr syncBuffer
			if _, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{
				Stderr:      &stderr,
				OpenBrowser: func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() },
				Interval:    time.Second,
			}); err != nil {
				t.Fatalf("DeviceLogin: %v", err)
			}
			if got, want := stderr.String(), strings.ReplaceAll(tc.wantPrompt, "BASE", srv.URL); got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
			want := []string(nil)
			if tc.wantOpened != "" {
				want = []string{strings.ReplaceAll(tc.wantOpened, "BASE", srv.URL)}
			}
			if strings.Join(opened, "\n") != strings.Join(want, "\n") || len(opened) != len(want) {
				t.Errorf("opened %q, want %q", opened, want)
			}
		})
	}
}

func TestOpenableVerificationURL(t *testing.T) {
	const endpoint = "https://acme.omniapp.co"
	for target, want := range map[string]bool{
		"https://acme.omniapp.co/oauth/device?user_code=X":                    true,
		"https://ACME.omniapp.co/oauth/device":                                true,
		"https://acme.omniapp.co:443/oauth/device":                            true,
		`https://acme.omniapp.co/oauth/device?user_code=X&\\evil\share\p.exe`: true,
		"https://acme.omniapp.co:8443/oauth/device":                           false,
		"http://acme.omniapp.co/oauth/device":                                 false,
		"https://acme.omniapp.co.evil.example/oauth/device":                   false,
		"https://evil.example/oauth/device":                                   false,
		"https://user@acme.omniapp.co/oauth/device":                           false,
		"https://acme.omniapp.co/oauth/device?a=b c":                          false,
		"https://acme.omniapp.co/oauth/device?a=\tb":                          false,
		"https://acme.omniapp.co/oauth/device\u0085":                          false,
		"javascript:alert(1)":                                                 false,
	} {
		if got := openableVerificationURL(target, endpoint); got != want {
			t.Errorf("openableVerificationURL(%q) = %v, want %v", target, got, want)
		}
	}
	// A local http endpoint may open http URLs on its own host and port.
	if !openableVerificationURL("http://localhost:3000/oauth/device", "http://localhost:3000") {
		t.Error("http URL on an http endpoint's host refused")
	}
	if openableVerificationURL("http://localhost:3001/oauth/device", "http://localhost:3000") {
		t.Error("http URL on another port accepted")
	}
}

// Windows opens URLs through rundll32's URL handler, which takes the URL as
// one argument; cmd /c start would run an & in it as a second command.
func TestBrowserCommand(t *testing.T) {
	const u = `https://acme.omniapp.co/oauth/device?user_code=X&\\evil\share\p.exe`
	for goos, want := range map[string][]string{
		"windows": {"rundll32", "url.dll,FileProtocolHandler", u},
		"darwin":  {"open", u},
		"linux":   {"xdg-open", u},
	} {
		if got := browserCommand(goos, u).Args; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("browserCommand(%s) args = %q, want %q", goos, got, want)
		}
	}
}

// expires_in beyond ten minutes is cut to ten, in the prompt and the JSON.
func TestDeviceLogin_ExpiryCappedAtTenMinutes(t *testing.T) {
	t.Parallel()
	var jsonOut, stderr syncBuffer
	srv := (&deviceServer{
		tokens:     []string{"device_token_success"},
		deviceBody: `{"device_code":"d","user_code":"BDKF-QRTW","verification_uri":"BASE/oauth/device","expires_in":900,"interval":5}`,
	}).start(t)
	if _, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Stderr: &stderr, JSON: &jsonOut, Interval: time.Second}); err != nil {
		t.Fatalf("DeviceLogin: %v", err)
	}
	if !strings.Contains(stderr.String(), "(expires in 10m)") {
		t.Errorf("stderr = %q, want expires in 10m", stderr.String())
	}
	if !strings.Contains(jsonOut.String(), `"expires_in":600`) {
		t.Errorf("JSON = %q, want expires_in 600", jsonOut.String())
	}
}

// Server text is printed without control characters, and a user code that
// isn't printable ASCII is refused.
func TestDeviceLogin_SanitizesServerText(t *testing.T) {
	t.Parallel()
	t.Run("verification_uri", func(t *testing.T) {
		t.Parallel()
		var stderr syncBuffer
		srv := (&deviceServer{
			tokens:     []string{"device_token_success"},
			deviceBody: `{"device_code":"d","user_code":"BDKF-QRTW","verification_uri":"https://acme.omniapp.co/oauth/\u001b[2Jdevice\u0007","expires_in":600,"interval":5}`,
		}).start(t)
		if _, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Stderr: &stderr, Interval: time.Second}); err != nil {
			t.Fatalf("DeviceLogin: %v", err)
		}
		if got := stderr.String(); strings.ContainsAny(got, "\x1b\x07") || !strings.Contains(got, "open https://acme.omniapp.co/oauth/[2Jdevice\n") {
			t.Errorf("stderr = %q, want the URL with its control characters removed", got)
		}
	})
	t.Run("user_code", func(t *testing.T) {
		t.Parallel()
		var stderr syncBuffer
		srv := (&deviceServer{
			tokens:     []string{"device_token_success"},
			deviceBody: `{"device_code":"d","user_code":"BDKF\u001b[2J","verification_uri":"https://acme.omniapp.co/oauth/device","expires_in":600,"interval":5}`,
		}).start(t)
		_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{Stderr: &stderr, Interval: time.Second})
		if err == nil || !strings.Contains(err.Error(), "isn't printable ASCII") {
			t.Fatalf("err = %v, want the unprintable user code refused", err)
		}
		if stderr.String() != "" {
			t.Errorf("stderr = %q, want nothing printed", stderr.String())
		}
		if _, _, polls := srv.snapshot(); len(polls) != 0 {
			t.Errorf("token polls = %d, want 0", len(polls))
		}
	})
}

// A device authorization request that times out says so; no code was ever
// shown, so it isn't "code expired".
func TestDeviceLogin_RequestTimeout(t *testing.T) {
	t.Parallel()
	srv := (&deviceServer{deviceDelay: 500 * time.Millisecond}).start(t)
	_, err := DeviceLogin(context.Background(), srv.URL, DeviceOptions{RequestTimeout: 100 * time.Millisecond})
	host := strings.TrimPrefix(srv.URL, "http://")
	if err == nil || err.Error() != "timed out requesting a device code from "+host {
		t.Fatalf("err = %v, want a request timeout naming %s", err, host)
	}
	if errors.Is(err, ErrCodeExpired) {
		t.Errorf("err = %v reads as an expired code", err)
	}
}

// browserStub records whether LoginDefault fell back to the browser login.
type browserStub struct {
	mu    sync.Mutex
	calls int
}

func (b *browserStub) login(string, func(string)) (*oauth2.Token, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	return &oauth2.Token{AccessToken: "browser-access"}, nil
}

func TestLoginDefault_ChoosesFlow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		discovery    string
		deviceStatus int
		browser      bool
		wantBrowser  bool
		wantDiscover int
		wantDevice   int
		wantNote     string
	}{
		{"device when advertised", "discovery_with_device", 0, false, false, 1, 1, ""},
		{"browser when discovery lacks the endpoint", "discovery_without_device", 0, false, true, 1, 0, notOffered},
		{"browser when there is no discovery document", "", 0, false, true, 1, 0, notOffered},
		{"browser when device authorization is 404", "discovery_with_device", http.StatusNotFound, false, true, 1, 1, notOffered},
		{"browser when discovery fails", "503", 0, false, true, 1, 0, "Couldn't read the login settings at /.well-known/oauth-authorization-server (HTTP 503); falling back to browser login."},
		{"browser when discovery isn't JSON", "garbage", 0, false, true, 1, 0, "Couldn't read the login settings at /.well-known/oauth-authorization-server (not valid JSON); falling back to browser login."},
		{"--browser skips device login entirely", "discovery_with_device", 0, true, true, 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := (&deviceServer{
				discovery:    tc.discovery,
				deviceStatus: tc.deviceStatus,
				tokens:       []string{"device_token_success"},
			}).start(t)
			var stub browserStub
			var stderr syncBuffer
			tok, err := LoginDefault(context.Background(), srv.URL, LoginOptions{
				Browser:      tc.browser,
				Device:       DeviceOptions{Stderr: &stderr, Interval: time.Second},
				BrowserLogin: stub.login,
			})
			if err != nil {
				t.Fatalf("LoginDefault: %v", err)
			}
			if tc.wantNote != "" && !strings.Contains(stderr.String(), tc.wantNote+"\n") {
				t.Errorf("stderr = %q, want the note %q", stderr.String(), tc.wantNote)
			}
			if tc.wantNote != notOffered && strings.Contains(stderr.String(), notOffered) {
				t.Errorf("stderr = %q, says the instance doesn't offer device login", stderr.String())
			}
			wantToken := "device-access-token"
			if tc.wantBrowser {
				wantToken = "browser-access"
			}
			if tok.AccessToken != wantToken {
				t.Errorf("access token = %q, want %q", tok.AccessToken, wantToken)
			}
			if got := map[bool]int{false: 0, true: 1}[tc.wantBrowser]; stub.calls != got {
				t.Errorf("browser logins = %d, want %d", stub.calls, got)
			}
			discoveries, deviceForms, _ := srv.snapshot()
			if discoveries != tc.wantDiscover {
				t.Errorf("discovery requests = %d, want %d", discoveries, tc.wantDiscover)
			}
			if len(deviceForms) != tc.wantDevice {
				t.Errorf("device authorization requests = %d, want %d", len(deviceForms), tc.wantDevice)
			}
		})
	}
}

// A discovery document behind the org's IP allowlist fails with the
// allowlist message instead of falling back to a browser login that the
// allowlist would refuse too.
func TestLoginDefault_DiscoveryForbidden(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	var stub browserStub
	_, err := LoginDefault(context.Background(), srv.URL, LoginOptions{BrowserLogin: stub.login})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if stub.calls != 0 {
		t.Errorf("browser logins = %d, want 0", stub.calls)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Minute: "10m",
		90 * time.Second: "1m30s",
		45 * time.Second: "45s",
		time.Hour:        "1h",
		61 * time.Minute: "1h1m",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
