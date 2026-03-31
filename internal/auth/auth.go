// Package auth handles authenticated HTTP requests to the Omni API.
package auth

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/exploreomni/omni-cli/internal/config"
)

// Do executes an authenticated HTTP request against the Omni API.
func Do(cfg *config.ResolvedConfig, method, path string, body []byte) (*http.Response, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	url := baseURL + path

	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, url, bodyReader)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}

	return resp, nil
}
