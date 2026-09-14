// Package output handles formatting API responses for stdout/stderr.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSON reads a response body and writes it as formatted JSON to stdout.
func JSON(body io.Reader, compact bool) error {
	return JSONTo(os.Stdout, body, compact)
}

// JSONTo reads a response body and writes it as formatted JSON to w.
func JSONTo(w io.Writer, body io.Reader, compact bool) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	return JSONBytes(w, data, compact)
}

// JSONBytes writes an already-read body as formatted JSON to w. Callers that
// have the bytes in hand use this so a large payload isn't read — and
// allocated — a second time.
func JSONBytes(w io.Writer, data []byte, compact bool) error {
	if compact {
		if _, err := w.Write(data); err != nil {
			return err
		}
		fmt.Fprintln(w)
		return nil
	}

	// Pretty-print. json.Indent reformats the bytes in place rather than
	// building a value first, so the body is parsed once.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		// Not JSON — print raw.
		_, err := w.Write(data)
		fmt.Fprintln(w)
		return err
	}

	if _, err := w.Write(bytes.TrimRight(pretty.Bytes(), "\n")); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

// APIErrorTo writes a single JSON document describing a failed API call to w.
// The envelope carries the human-readable detail and the HTTP status, plus the
// API's own error payload verbatim under "body" when it was JSON.
//
// A failed invocation must leave exactly one JSON document on stderr, so
// callers must not print anything else alongside it — a trailing "Error: ..."
// line would make `omni ... 2>err.json` unparseable.
func APIErrorTo(w io.Writer, statusCode int, detail string, body json.RawMessage, compact bool) {
	msg := struct {
		Error  string          `json:"error"`
		Status int             `json:"status,omitempty"`
		Body   json.RawMessage `json:"body,omitempty"`
	}{
		Error:  detail,
		Status: statusCode,
		Body:   body,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		// The detail came from the response body and is always a valid string,
		// so this is unreachable in practice; fall back to the plain envelope.
		ErrorTo(w, statusCode, fmt.Sprintf("HTTP %d", statusCode))
		return
	}
	if !compact {
		if pretty, err := json.MarshalIndent(msg, "", "  "); err == nil {
			data = pretty
		}
	}
	fmt.Fprintln(w, string(data))
}

// Error prints a JSON error to stderr.
func Error(statusCode int, detail string) {
	ErrorTo(os.Stderr, statusCode, detail)
}

// ErrorTo prints a JSON error to w.
func ErrorTo(w io.Writer, statusCode int, detail string) {
	msg := struct {
		Error  string `json:"error"`
		Status int    `json:"status,omitempty"`
	}{
		Error:  detail,
		Status: statusCode,
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(w, string(data))
}
