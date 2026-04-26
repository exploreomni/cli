// Package oauth implements OAuth 2.1 browser-based login with PKCE for the Omni CLI.
// It wraps golang.org/x/oauth2 with the local-server + browser-opener glue needed
// for a CLI-initiated authorization code flow.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const clientID = "omni-cli"

// Config builds an oauth2.Config for the given Omni API endpoint.
// redirectURI may be empty when only refreshing tokens (no auth URL needed).
func Config(apiEndpoint, redirectURI string) *oauth2.Config {
	base := strings.TrimRight(apiEndpoint, "/")
	return &oauth2.Config{
		ClientID: clientID,
		Scopes:   []string{"user:default"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  base + "/oauth/authorize",
			TokenURL: base + "/oauth/token",
		},
		RedirectURL: redirectURI,
	}
}

// Login performs the full OAuth 2.1 authorization code flow with PKCE.
// It starts a local HTTP server, opens the browser to the authorization endpoint,
// waits for the callback, and exchanges the code for tokens.
func Login(apiEndpoint string) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	conf := Config(apiEndpoint, redirectURI)

	verifier := oauth2.GenerateVerifier()
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	type authResult struct {
		code string
		err  error
	}
	resultCh := make(chan authResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			errDesc := r.URL.Query().Get("error_description")
			msg := fmt.Sprintf("Authorization error: %s", errParam)
			if errDesc != "" {
				msg = fmt.Sprintf("%s — %s", msg, errDesc)
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h2>Login failed</h2><p>%s</p></body></html>", html.EscapeString(msg))
			resultCh <- authResult{err: fmt.Errorf("%s", msg)}
			return
		}

		returnedState := r.URL.Query().Get("state")
		if returnedState != state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Login failed</h2><p>State mismatch — possible CSRF attack.</p></body></html>")
			resultCh <- authResult{err: fmt.Errorf("state mismatch: expected %q, got %q", state, returnedState)}
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Login failed</h2><p>No authorization code received.</p></body></html>")
			resultCh <- authResult{err: fmt.Errorf("no authorization code in callback")}
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h2>Login successful!</h2><p>You can close this tab.</p></body></html>")
		resultCh <- authResult{code: code}
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()

	authURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	fmt.Printf("Opening browser to log in...\n")
	fmt.Printf("If the browser doesn't open, visit this URL:\n%s\n\n", authURL)
	openBrowser(authURL)

	var result authResult
	select {
	case result = <-resultCh:
	case <-time.After(2 * time.Minute):
		_ = server.Shutdown(context.Background())
		return nil, fmt.Errorf("timed out waiting for browser login (2 minutes)")
	}
	_ = server.Shutdown(context.Background())

	if result.err != nil {
		return nil, result.err
	}

	return conf.Exchange(context.Background(), result.code, oauth2.VerifierOption(verifier))
}

// openBrowser opens the given URL in the user's default browser.
// Best-effort — if it fails, the URL is already printed to the terminal.
// It's a var (not a func) so tests can replace it to drive the callback flow.
var openBrowser = func(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
