// Package auth handles authenticated HTTP requests to the Omni API.
package auth

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/useragent"
)

// Do executes an authenticated HTTP request against the Omni API.
func Do(cfg *config.ResolvedConfig, method, path string, body []byte) (*http.Response, error) {
	return DoWithContentType(cfg, method, path, body, "application/json")
}

// DoWithContentType executes an authenticated request using the caller's
// declared request media type. Multipart callers must include the boundary in
// contentType (for example, multipart/form-data; boundary=...).
func DoWithContentType(cfg *config.ResolvedConfig, method, path string, body []byte, contentType string) (*http.Response, error) {
	// Paths can come from the embedded API spec. A path without a leading slash
	// can change the destination when concatenated with the configured base URL.
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("API path must start with /")
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid API base URL")
	}
	requestURL := baseURL + path

	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	var req *http.Request
	if bodyReader != nil {
		req, err = http.NewRequest(method, requestURL, bodyReader)
	} else {
		req, err = http.NewRequest(method, requestURL, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
		return nil, fmt.Errorf("API path changes request origin")
	}

	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", useragent.String())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", requestURL, err)
	}

	return resp, nil
}
