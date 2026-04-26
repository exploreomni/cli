package oauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfig_BuildsExpectedURLs(t *testing.T) {
	c := Config("https://myorg.omniapp.co", "http://localhost:1234/callback")

	if c.ClientID != "omni-cli" {
		t.Errorf("ClientID = %q, want %q", c.ClientID, "omni-cli")
	}
	if got, want := c.Endpoint.AuthURL, "https://myorg.omniapp.co/oauth/authorize"; got != want {
		t.Errorf("AuthURL = %q, want %q", got, want)
	}
	if got, want := c.Endpoint.TokenURL, "https://myorg.omniapp.co/oauth/token"; got != want {
		t.Errorf("TokenURL = %q, want %q", got, want)
	}
	if c.RedirectURL != "http://localhost:1234/callback" {
		t.Errorf("RedirectURL = %q, want %q", c.RedirectURL, "http://localhost:1234/callback")
	}
	if len(c.Scopes) != 1 || c.Scopes[0] != "user:default" {
		t.Errorf("Scopes = %v, want [user:default]", c.Scopes)
	}
}

// Trailing slashes on the API endpoint must not produce double slashes in the
// auth/token URLs — some servers return 404 for `//oauth/authorize`.
func TestConfig_TrimsTrailingSlash(t *testing.T) {
	c := Config("https://myorg.omniapp.co/", "")
	if got, want := c.Endpoint.AuthURL, "https://myorg.omniapp.co/oauth/authorize"; got != want {
		t.Errorf("AuthURL = %q, want %q (trailing slash should be trimmed)", got, want)
	}
}

// stubBrowser swaps openBrowser for a function that drives the OAuth callback
// directly, simulating what the user's browser would do after consenting.
// The replacement runs in a goroutine because Login() blocks on the callback.
//
// callback receives the authURL the production code passed to openBrowser, so
// tests can read state/redirect_uri from it and craft a matching response.
func stubBrowser(t *testing.T, callback func(authURL string)) {
	t.Helper()
	prev := openBrowser
	openBrowser = func(authURL string) {
		go callback(authURL)
	}
	t.Cleanup(func() { openBrowser = prev })
}

// extractRedirect pulls the redirect_uri and state out of the auth URL the CLI
// would have sent the browser to.
func extractRedirect(t *testing.T, authURL string) (redirect, state string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing auth URL %q: %v", authURL, err)
	}
	q := u.Query()
	return q.Get("redirect_uri"), q.Get("state")
}

// hitCallback issues a GET to the local callback server with the given query
// params, the way the user's browser would after the auth server redirected.
func hitCallback(t *testing.T, redirect string, params url.Values) {
	t.Helper()
	resp, err := http.Get(redirect + "?" + params.Encode())
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestLogin_Success(t *testing.T) {
	// Mock the OAuth token endpoint — return a real-looking token response
	// when the CLI exchanges the auth code.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if got := r.Form.Get("code"); got != "test-code" {
			t.Errorf("code form value = %q, want %q", got, "test-code")
		}
		if r.Form.Get("code_verifier") == "" {
			t.Error("code_verifier missing from token request — PKCE broken")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"good-access","refresh_token":"good-refresh","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer tokenSrv.Close()

	stubBrowser(t, func(authURL string) {
		redirect, state := extractRedirect(t, authURL)
		hitCallback(t, redirect, url.Values{"code": {"test-code"}, "state": {state}})
	})

	tok, err := Login(tokenSrv.URL)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "good-access" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "good-access")
	}
	if tok.RefreshToken != "good-refresh" {
		t.Errorf("RefreshToken = %q, want %q", tok.RefreshToken, "good-refresh")
	}
}

// State mismatch is the CSRF defense — must reject the callback even with a
// valid code. The token endpoint should never be hit.
func TestLogin_StateMismatch(t *testing.T) {
	var tokenHits int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer tokenSrv.Close()

	stubBrowser(t, func(authURL string) {
		redirect, _ := extractRedirect(t, authURL)
		hitCallback(t, redirect, url.Values{"code": {"x"}, "state": {"wrong-state"}})
	})

	_, err := Login(tokenSrv.URL)
	if err == nil {
		t.Fatal("expected error from state mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("error = %q, want it to mention state mismatch", err.Error())
	}
	if tokenHits != 0 {
		t.Errorf("token endpoint hit %d time(s); CSRF defense should prevent any exchange", tokenHits)
	}
}

// When the auth server redirects back with `error=access_denied`, Login must
// surface that to the caller rather than attempting a token exchange.
func TestLogin_AuthorizationError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint should not be hit when authorize step errors")
	}))
	defer tokenSrv.Close()

	stubBrowser(t, func(authURL string) {
		redirect, state := extractRedirect(t, authURL)
		hitCallback(t, redirect, url.Values{
			"error":             {"access_denied"},
			"error_description": {"user said no"},
			"state":             {state},
		})
	})

	_, err := Login(tokenSrv.URL)
	if err == nil {
		t.Fatal("expected error from authorize failure, got nil")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error = %q, want it to include the auth server's error code", err.Error())
	}
}

// The error_description field is reflected back into the HTML page shown to
// the user. The handler html-escapes it (per 891eb3a) — verify that an attacker
// who controls error_description can't inject <script> tags into the page.
func TestLogin_HTMLEscapesErrorDescription(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tokenSrv.Close()

	bodyCh := make(chan string, 1)
	stubBrowser(t, func(authURL string) {
		redirect, state := extractRedirect(t, authURL)
		resp, err := http.Get(redirect + "?" + url.Values{
			"error":             {"server_error"},
			"error_description": {`<script>alert(1)</script>`},
			"state":             {state},
		}.Encode())
		if err != nil {
			bodyCh <- ""
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		bodyCh <- string(b)
	})

	_, _ = Login(tokenSrv.URL)
	pageBody := <-bodyCh

	if strings.Contains(pageBody, "<script>alert(1)</script>") {
		t.Errorf("callback page contains unescaped <script>; body = %q", pageBody)
	}
	if !strings.Contains(pageBody, "&lt;script&gt;") {
		t.Errorf("callback page should contain HTML-escaped angle brackets; body = %q", pageBody)
	}
}

func TestLogin_MissingCode(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint should not be hit when no code arrives")
	}))
	defer tokenSrv.Close()

	stubBrowser(t, func(authURL string) {
		redirect, state := extractRedirect(t, authURL)
		// No "code" param.
		hitCallback(t, redirect, url.Values{"state": {state}})
	})

	_, err := Login(tokenSrv.URL)
	if err == nil {
		t.Fatal("expected error when callback omits code, got nil")
	}
	if !strings.Contains(err.Error(), "no authorization code") {
		t.Errorf("error = %q, want it to mention missing code", err.Error())
	}
}
