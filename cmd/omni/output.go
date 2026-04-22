package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
)

func outputResponse(resp *http.Response, format string, compact bool) error {
	if resp.StatusCode >= 400 {
		if format == config.FormatHuman {
			body, _ := io.ReadAll(resp.Body)
			output.HumanError(resp.StatusCode, extractErrorDetail(body, resp.StatusCode))
		} else {
			if err := output.JSON(resp.Body, compact); err != nil {
				output.Error(resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode))
			}
		}
		return fmt.Errorf("API returned HTTP %d", resp.StatusCode)
	}

	// 204 No Content
	if resp.StatusCode == 204 {
		if format == config.FormatHuman {
			fmt.Println("✓ ok")
		} else {
			fmt.Println("{}")
		}
		return nil
	}

	if format == config.FormatHuman {
		return output.Human(resp.Body)
	}
	return output.JSON(resp.Body, compact)
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
