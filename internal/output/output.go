// Package output handles formatting API responses for stdout/stderr.
package output

import (
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

	if compact {
		_, err = w.Write(data)
		if err != nil {
			return err
		}
		fmt.Fprintln(w)
		return nil
	}

	// Pretty-print
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Not JSON — print raw
		_, err = w.Write(data)
		fmt.Fprintln(w)
		return err
	}

	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		_, err = w.Write(data)
		fmt.Fprintln(w)
		return err
	}

	_, err = w.Write(pretty)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
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
