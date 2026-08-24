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

func outputResponse(resp *http.Response, format string, compact bool) error {
	return outputResponseTo(os.Stdout, os.Stderr, resp, format, compact)
}

// outputResponseTo writes a response to explicit streams. Successful payloads
// go to stdout; anything about a failure (the API's error body included) goes
// to stderr, so a pipe consumer either gets well-formed data or nothing at all.
func outputResponseTo(stdout, stderr io.Writer, resp *http.Response, format string, compact bool) error {
	if resp.StatusCode >= 400 {
		if format == config.FormatHuman {
			body, _ := io.ReadAll(resp.Body)
			output.HumanErrorTo(stderr, resp.StatusCode, extractErrorDetail(body, resp.StatusCode))
		} else {
			if err := output.JSONTo(stderr, resp.Body, compact); err != nil {
				output.ErrorTo(stderr, resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode))
			}
		}
		return fmt.Errorf("API returned HTTP %d", resp.StatusCode)
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
		return output.HumanTo(stdout, resp.Body)
	}
	return output.JSONTo(stdout, resp.Body, compact)
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
