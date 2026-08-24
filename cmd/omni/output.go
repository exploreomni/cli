package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
)

// apiError reports that the API itself returned a failure status. The response
// writer has already emitted a complete error message, so the caller silences
// cobra's own one-line report rather than printing the failure twice.
type apiError struct {
	status int
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API returned HTTP %d", e.status)
}

func outputResponse(resp *http.Response, format string, compact bool) error {
	return outputResponseTo(os.Stdout, os.Stderr, resp, format, compact)
}

// outputResponseTo writes a response to explicit streams. Two contracts hold:
//
//   - Failures write nothing to stdout. The API's error body goes to stderr,
//     as exactly one JSON document in JSON mode, so `2>err.json` stays valid.
//   - A 2xx body that isn't JSON is passed through to stdout unchanged and
//     counts as success. Several endpoints legitimately return non-JSON —
//     `query run` streams text/ndjson and returns CSV/XLSX with --result-type
//     — so an un-parseable payload is data, not an error.
//
// The body is read in full before anything is written, so a truncated read
// can't leave half a payload on stdout ahead of a non-zero exit.
func outputResponseTo(stdout, stderr io.Writer, resp *http.Response, format string, compact bool) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		detail := extractErrorDetail(data, resp.StatusCode)
		if format == config.FormatHuman {
			output.HumanErrorTo(stderr, resp.StatusCode, detail)
		} else {
			output.APIErrorTo(stderr, resp.StatusCode, detail, jsonBody(data), compact)
		}
		return &apiError{status: resp.StatusCode}
	}

	// 204 No Content
	if resp.StatusCode == 204 {
		if format == config.FormatHuman {
			fmt.Fprintln(stdout, "✓ ok")
		} else {
			fmt.Fprintln(stdout, "{}")
		}
		return nil
	}

	if format == config.FormatHuman {
		return output.HumanTo(stdout, bytes.NewReader(data))
	}
	return output.JSONTo(stdout, bytes.NewReader(data), compact)
}

// jsonBody returns the body as raw JSON for embedding in an error envelope,
// or nil when it isn't JSON (an HTML error page from a proxy, say).
func jsonBody(body []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil
	}
	return json.RawMessage(trimmed)
}

// extractErrorDetail pulls a readable message out of a JSON error body.
// Falls back to the raw body or an HTTP status string.
func extractErrorDetail(body []byte, status int) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Sprintf("HTTP %d", status)
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err == nil {
		for _, key := range []string{"detail", "message", "error"} {
			if s, ok := obj[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return string(trimmed)
}
