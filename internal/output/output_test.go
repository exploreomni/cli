package output

import (
	"bytes"
	"strings"
	"testing"
)

// These test the JSON formatting functions that handle all CLI output.
// The CLI always outputs JSON — either pretty-printed (default) or compact
// (with --compact flag, useful for piping to jq).

// Default mode: JSON should be indented with 2 spaces for readability.
func TestJSONTo_PrettyPrint(t *testing.T) {
	var buf bytes.Buffer
	body := strings.NewReader(`{"name":"test","count":42}`)
	if err := JSONTo(&buf, body, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "  \"name\": \"test\"") {
		t.Errorf("expected indented JSON, got:\n%s", got)
	}
	if !strings.Contains(got, "  \"count\": 42") {
		t.Errorf("expected indented count field, got:\n%s", got)
	}
}

// Compact mode (--compact flag): JSON should be output as-is, no whitespace added.
func TestJSONTo_Compact(t *testing.T) {
	var buf bytes.Buffer
	input := `{"name":"test","count":42}`
	body := strings.NewReader(input)
	if err := JSONTo(&buf, body, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

// If the API returns something that isn't valid JSON (shouldn't happen, but
// could with proxy errors etc.), the output function should print it raw
// rather than crashing.
func TestJSONTo_InvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	input := "this is not json"
	body := strings.NewReader(input)
	_ = JSONTo(&buf, body, false)
	got := strings.TrimSpace(buf.String())
	if got != input {
		t.Errorf("expected raw output %q, got %q", input, got)
	}
}

// Error responses are written to stderr as JSON with "error" and "status" fields.
func TestErrorTo_Format(t *testing.T) {
	var buf bytes.Buffer
	ErrorTo(&buf, 404, "not found")
	got := buf.String()
	if !strings.Contains(got, `"error":"not found"`) {
		t.Errorf("expected error field, got: %s", got)
	}
	if !strings.Contains(got, `"status":404`) {
		t.Errorf("expected status field, got: %s", got)
	}
}
