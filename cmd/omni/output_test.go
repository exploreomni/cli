package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// These test the outputResponse function which is the last step before the
// user sees output. It routes API responses: errors (4xx/5xx) return a Go
// error so the CLI exits non-zero, 204 No Content prints "{}", and success
// responses get pretty-printed.

// A 400+ status should return an error (so the CLI exits with non-zero code).
func TestOutputResponse_Error(t *testing.T) {
	resp := &http.Response{
		StatusCode: 400,
		Body:       io.NopCloser(strings.NewReader(`{"error":"bad request"}`)),
	}
	err := outputResponse(resp, "json", true)
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

// 204 No Content (e.g. after a successful DELETE) should print "{}" and not error.
func TestOutputResponse_NoContent(t *testing.T) {
	resp := &http.Response{
		StatusCode: 204,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := outputResponse(resp, "json", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Human mode should also return an error for 400+ responses.
func TestOutputResponse_Error_Human(t *testing.T) {
	resp := &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader(`{"detail":"not found"}`)),
	}
	err := outputResponse(resp, "human", false)
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
}

// Stream hygiene: an API error body must never land on stdout. A caller doing
// `omni ... | jq` should see either well-formed data or an empty stream —
// never error JSON mixed into the payload.
func TestOutputResponseTo_ErrorBodyGoesToStderr(t *testing.T) {
	for _, compact := range []bool{true, false} {
		var stdout, stderr bytes.Buffer
		resp := &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"detail":"bad request"}`)),
		}

		err := outputResponseTo(&stdout, &stderr, resp, "json", compact)
		if err == nil {
			t.Fatalf("compact=%v: expected error for 400 status", compact)
		}
		if stdout.Len() != 0 {
			t.Errorf("compact=%v: stdout should be empty, got %q", compact, stdout.String())
		}
		if !strings.Contains(stderr.String(), "bad request") {
			t.Errorf("compact=%v: stderr missing error body, got %q", compact, stderr.String())
		}
	}
}

// Human-mode errors go to stderr too, as a one-line message with the status.
func TestOutputResponseTo_HumanErrorGoesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader(`{"detail":"not found"}`)),
	}

	if err := outputResponseTo(&stdout, &stderr, resp, "human", false); err == nil {
		t.Fatal("expected error for 404 status")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not found") || !strings.Contains(stderr.String(), "404") {
		t.Errorf("stderr = %q, want detail and status", stderr.String())
	}
}

// The mirror image: successful payloads stay on stdout and leave stderr clean.
func TestOutputResponseTo_SuccessGoesToStdout(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		format string
		want   string
	}{
		{"json", 200, `{"records":[]}`, "json", "records"},
		{"no content", 204, "", "json", "{}"},
		{"human", 200, `{"name":"widget"}`, "human", "widget"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			resp := &http.Response{
				StatusCode: tc.status,
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}

			if err := outputResponseTo(&stdout, &stderr, resp, tc.format, true); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tc.want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr should be empty, got %q", stderr.String())
			}
		})
	}
}
