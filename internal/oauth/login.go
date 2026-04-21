// Package oauth implements OAuth 2.1 browser-based login with PKCE for the Omni CLI.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const clientID = "omni-cli"

// TokenResponse represents the response from the OAuth token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// Login performs the full OAuth 2.1 authorization code flow with PKCE.
// It starts a local HTTP server, opens the browser to the authorization endpoint,
// waits for the callback, and exchanges the code for tokens.
func Login(apiEndpoint string) (*TokenResponse, error) {
	// Generate PKCE code verifier (32 random bytes, base64url-encoded)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Generate code challenge (SHA-256 of verifier, base64url-encoded)
	challengeHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])

	// Generate state parameter (16 random bytes, hex-encoded)
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	// Start local HTTP server on ephemeral port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Channel to receive the authorization result
	type authResult struct {
		code string
		err  error
	}
	resultCh := make(chan authResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Check for errors from the authorization server
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			errDesc := r.URL.Query().Get("error_description")
			msg := fmt.Sprintf("Authorization error: %s", errParam)
			if errDesc != "" {
				msg = fmt.Sprintf("%s — %s", msg, errDesc)
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h2>Login failed</h2><p>%s</p></body></html>", msg)
			resultCh <- authResult{err: fmt.Errorf("%s", msg)}
			return
		}

		// Validate state
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

	// Build authorization URL
	authURL := strings.TrimRight(apiEndpoint, "/") + "/oauth/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"user:default"},
		"state":                 {state},
	}.Encode()

	// Print URL for manual copy-paste (headless environments)
	fmt.Printf("Opening browser to log in...\n")
	fmt.Printf("If the browser doesn't open, visit this URL:\n%s\n\n", authURL)

	// Open browser (best-effort)
	openBrowser(authURL)

	// Wait for callback with timeout
	var result authResult
	select {
	case result = <-resultCh:
	case <-time.After(2 * time.Minute):
		_ = server.Shutdown(context.Background())
		return nil, fmt.Errorf("timed out waiting for browser login (2 minutes)")
	}

	// Shut down the local server
	_ = server.Shutdown(context.Background())

	if result.err != nil {
		return nil, result.err
	}

	// Exchange authorization code for tokens
	return exchangeCode(apiEndpoint, result.code, verifier, redirectURI)
}

// exchangeCode exchanges an authorization code for tokens at the token endpoint.
func exchangeCode(apiEndpoint, code, verifier, redirectURI string) (*TokenResponse, error) {
	tokenURL := strings.TrimRight(apiEndpoint, "/") + "/oauth/token"

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}

	return postTokenRequest(tokenURL, data)
}

// openBrowser opens the given URL in the user's default browser.
// This is best-effort — if it fails, the URL is already printed to the terminal.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
