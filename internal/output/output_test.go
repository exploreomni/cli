package output

import (
	"bytes"
	"encoding/json"
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

// A failed API call leaves exactly one JSON document: the detail, the status,
// and the API's own payload verbatim under "body".
func TestAPIErrorTo_SingleJSONDocument(t *testing.T) {
	for _, compact := range []bool{true, false} {
		var buf bytes.Buffer
		APIErrorTo(&buf, 400, "bad request", json.RawMessage(`{"detail":"bad request","code":"X"}`), compact)

		var got struct {
			Error  string          `json:"error"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("compact=%v: output is not a single JSON document (%v): %q", compact, err, buf.String())
		}
		if got.Error != "bad request" || got.Status != 400 {
			t.Errorf("compact=%v: got %+v, want detail and status", compact, got)
		}
		var body map[string]any
		if err := json.Unmarshal(got.Body, &body); err != nil {
			t.Fatalf("compact=%v: body is not JSON (%v): %q", compact, err, string(got.Body))
		}
		if body["code"] != "X" {
			t.Errorf("compact=%v: body = %q, want the API's payload", compact, string(got.Body))
		}
		// Pretty mode indents; compact mode stays on one line.
		if indented := strings.Contains(buf.String(), "\n  "); indented == compact {
			t.Errorf("compact=%v: unexpected formatting: %q", compact, buf.String())
		}
	}
}

// With no JSON payload to embed, "body" is omitted rather than emitted as null.
func TestAPIErrorTo_OmitsMissingBody(t *testing.T) {
	var buf bytes.Buffer
	APIErrorTo(&buf, 502, "Bad Gateway", nil, true)
	if strings.Contains(buf.String(), "body") {
		t.Errorf("expected no body field, got %q", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON (%v): %q", err, buf.String())
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
