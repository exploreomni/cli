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
	discovery    string   // fixture for the discovery document; "" answers 404
	deviceStatus int      // device authorization status; 0 answers 200 with the fixture
	tokens       []string // fixtures for successive token polls; the last repeats; "403" answers 403
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
			if s.discovery == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, s.discovery))
		case deviceAuthorizationPath:
			s.deviceForms = append(s.deviceForms, r.PostForm)
			s.mu.Unlock()
			if s.deviceStatus != 0 {
				http.Error(w, http.StatusText(s.deviceStatus), s.deviceStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, "device_authorization"))
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
// browser opens the URL that carries the code.
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
	if len(opened) != 1 || opened[0] != "https://acme.omniapp.co/oauth/device?user_code=BDKF-QRTW" {
		t.Errorf("opened = %v, want verification_uri_complete once", opened)
	}
}

// browserStub records whether LoginDefault fell back to the browser login.
type browserStub struct {
	mu    sync.Mutex
	calls int
}

func (b *browserStub) login(string) (*oauth2.Token, error) {
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
	}{
		{"device when advertised", "discovery_with_device", 0, false, false, 1, 1},
		{"browser when discovery lacks the endpoint", "discovery_without_device", 0, false, true, 1, 0},
		{"browser when there is no discovery document", "", 0, false, true, 1, 0},
		{"browser when device authorization is 404", "discovery_with_device", http.StatusNotFound, false, true, 1, 1},
		{"--browser skips device login entirely", "discovery_with_device", 0, true, true, 0, 0},
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
			tok, err := LoginDefault(context.Background(), srv.URL, LoginOptions{
				Browser:      tc.browser,
				Device:       DeviceOptions{Interval: time.Second},
				BrowserLogin: stub.login,
			})
			if err != nil {
				t.Fatalf("LoginDefault: %v", err)
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
