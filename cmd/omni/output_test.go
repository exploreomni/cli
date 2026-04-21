package main

import (
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
