// Package output handles formatting API responses for stdout/stderr.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSON reads a response body and writes it as JSON to stdout.
func JSON(body io.Reader, compact bool) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if compact {
		_, err = os.Stdout.Write(data)
		if err != nil {
			return err
		}
		fmt.Println()
		return nil
	}

	// Pretty-print
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Not JSON — print raw
		_, err = os.Stdout.Write(data)
		fmt.Println()
		return err
	}

	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		_, err = os.Stdout.Write(data)
		fmt.Println()
		return err
	}

	_, err = os.Stdout.Write(pretty)
	if err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// Error prints a JSON error to stderr.
func Error(statusCode int, detail string) {
	msg := struct {
		Error  string `json:"error"`
		Status int    `json:"status,omitempty"`
	}{
		Error:  detail,
		Status: statusCode,
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stderr, string(data))
}
