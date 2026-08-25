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

// apiError reports a failed invocation whose message has already been written
// to stderr in full. The caller silences cobra's own one-line report off the
// back of this type, so the failure isn't announced twice.
type apiError struct {
	status int
	detail string
}

func (e *apiError) Error() string {
	if e.detail != "" {
		return e.detail
	}
	return fmt.Sprintf("API returned HTTP %d", e.status)
}

func outputResponse(resp *http.Response, format string, compact bool) error {
	return outputResponseTo(os.Stdout, os.Stderr, resp, format, compact)
}

// outputResponseTo writes a response to explicit streams, upholding two
// contracts: a failure writes nothing to stdout and leaves exactly one JSON
// document on stderr (in JSON mode), and a 2xx body that isn't JSON is data,
// not an error, so it passes through to stdout byte for byte. The body is read
// in full before anything is written, so a truncated read can't leave half a
// payload on stdout ahead of a non-zero exit.
func outputResponseTo(stdout, stderr io.Writer, resp *http.Response, format string, compact bool) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		// Still an envelope: JSON-mode stderr has to stay parseable even when
		// the failure is ours rather than the API's.
		detail := fmt.Sprintf("reading response: %v", err)
		writeError(stderr, format, resp.StatusCode, detail, nil, compact)
		return &apiError{status: resp.StatusCode, detail: detail}
	}

	if resp.StatusCode >= 400 {
		body := jsonBody(data)
		detail := extractErrorDetail(body, data, resp.StatusCode)
		writeError(stderr, format, resp.StatusCode, detail, body, compact)
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

	// Non-JSON payloads (`query run`'s text/ndjson stream, CSV/XLSX with
	// --result-type) go out unchanged: no re-indenting, no appended newline,
	// so a redirect to a file reproduces the response byte for byte.
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && !json.Valid(trimmed) {
		_, err := stdout.Write(data)
		return err
	}

	if format == config.FormatHuman {
		return output.HumanBytes(stdout, data)
	}
	return output.JSONBytes(stdout, data, compact)
}

// writeError reports a failed call on stderr in the requested format.
func writeError(stderr io.Writer, format string, status int, detail string, body json.RawMessage, compact bool) {
	if format == config.FormatHuman {
		output.HumanErrorTo(stderr, status, detail)
		return
	}
	output.APIErrorTo(stderr, status, detail, body, compact)
}

// jsonBody returns the body as raw JSON for embedding in an error envelope, or
// nil when it isn't JSON (an HTML error page from a proxy, say) or is a literal
// null, which carries no more information than omitting the field.
func jsonBody(body []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || !json.Valid(trimmed) || string(trimmed) == "null" {
		return nil
	}
	return json.RawMessage(trimmed)
}

// extractErrorDetail pulls a readable message out of a JSON error body, given
// the same body already validated by jsonBody. It falls back to the raw body,
// or to an HTTP status string when the body is empty or carries no message.
func extractErrorDetail(body json.RawMessage, raw []byte, status int) string {
	if body != nil {
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err == nil {
			for _, key := range []string{"detail", "message", "error"} {
				if s, ok := obj[key].(string); ok && s != "" {
					return s
				}
			}
		}
		return string(body)
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && string(trimmed) != "null" {
		return string(trimmed)
	}
	return fmt.Sprintf("HTTP %d", status)
}
