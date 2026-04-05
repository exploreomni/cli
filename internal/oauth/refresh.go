package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RefreshedTokens represents the response from a token refresh request.
type RefreshedTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// tokenErrorResponse represents an OAuth error response.
type tokenErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// RefreshAccessToken exchanges a refresh token for a new access token.
func RefreshAccessToken(apiEndpoint, refreshToken string) (*RefreshedTokens, error) {
	tokenURL := strings.TrimRight(apiEndpoint, "/") + "/oauth/token"

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}

	resp, err := postTokenRequest(tokenURL, data)
	if err != nil {
		return nil, err
	}

	return &RefreshedTokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresIn:    resp.ExpiresIn,
	}, nil
}

// postTokenRequest makes a POST request to the OAuth token endpoint.
func postTokenRequest(tokenURL string, data url.Values) (*TokenResponse, error) {
	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var tokenErr tokenErrorResponse
		if json.Unmarshal(body, &tokenErr) == nil && tokenErr.Error != "" {
			if tokenErr.Error == "invalid_grant" {
				return nil, fmt.Errorf("session expired — run `omni config login` to re-authenticate")
			}
			return nil, fmt.Errorf("token error: %s — %s", tokenErr.Error, tokenErr.Description)
		}
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}
