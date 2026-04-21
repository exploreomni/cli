package output

import (
	"bytes"
	"strings"
	"testing"
)

// List with `records` + `pageInfo` (the models/dashboards shape).
func TestHumanTo_RecordsList(t *testing.T) {
	body := strings.NewReader(`{
		"records": [
			{"id":"m1","name":"orders","modelKind":"SHARED"},
			{"id":"m2","name":"users","modelKind":"SCHEMA"}
		],
		"pageInfo": {"hasMore": true, "cursor": "abc"}
	}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Id") || !strings.Contains(out, "Name") || !strings.Contains(out, "Model Kind") {
		t.Errorf("expected column headers in output:\n%s", out)
	}
	if !strings.Contains(out, "orders") || !strings.Contains(out, "users") {
		t.Errorf("expected record values:\n%s", out)
	}
	if !strings.Contains(out, "Cursor: abc") {
		t.Errorf("expected pagination footer:\n%s", out)
	}
	// Blank line between the header row and the first data row so the table
	// is easier to scan at a glance.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != "" {
		t.Errorf("expected blank separator after header row, got:\n%s", out)
	}
}

// List with resource-named array (connections shape).
func TestHumanTo_ResourceNamedList(t *testing.T) {
	body := strings.NewReader(`{
		"connections": [
			{"id":"c1","name":"warehouse","dialect":"snowflake"}
		]
	}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "warehouse") || !strings.Contains(out, "snowflake") {
		t.Errorf("expected list values:\n%s", out)
	}
}

// Empty list should say "No results."
func TestHumanTo_EmptyList(t *testing.T) {
	body := strings.NewReader(`{"records":[], "pageInfo":{"hasMore":false}}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No results.") {
		t.Errorf("expected empty list message, got:\n%s", buf.String())
	}
}

// Single-resource object rendered as key: value.
func TestHumanTo_SingleObject(t *testing.T) {
	body := strings.NewReader(`{"id":"d1","name":"my dashboard","type":"dashboard"}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Id:") || !strings.Contains(out, "d1") {
		t.Errorf("expected key-value rendering:\n%s", out)
	}
	if !strings.Contains(out, "Name:") || !strings.Contains(out, "my dashboard") {
		t.Errorf("expected name field:\n%s", out)
	}
}

// Success envelope: {success, message, resource}.
func TestHumanTo_SuccessEnvelope(t *testing.T) {
	body := strings.NewReader(`{
		"success": true,
		"message": "Model created",
		"model": {"id":"m1","name":"orders"}
	}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "✓ Model created") {
		t.Errorf("expected success check mark:\n%s", out)
	}
	if !strings.Contains(out, "Id:") || !strings.Contains(out, "m1") {
		t.Errorf("expected inner model details:\n%s", out)
	}
}

// Empty body → "✓ ok".
func TestHumanTo_EmptyBody(t *testing.T) {
	body := strings.NewReader(``)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "✓ ok") {
		t.Errorf("expected ✓ ok, got: %q", buf.String())
	}
}

// Non-JSON body should pass through raw.
func TestHumanTo_NonJSON(t *testing.T) {
	body := strings.NewReader("not json at all")
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "not json at all") {
		t.Errorf("expected raw passthrough, got: %q", buf.String())
	}
}

// Bare JSON array.
func TestHumanTo_BareArray(t *testing.T) {
	body := strings.NewReader(`[{"id":"a","name":"x"},{"id":"b","name":"y"}]`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Id") || !strings.Contains(out, "Name") {
		t.Errorf("expected table headers:\n%s", out)
	}
}

// Mixed object: scalar field + array field (AI search shape).
// Both sections must render — regression: early heuristic dropped `answer`.
func TestHumanTo_MixedScalarAndArray(t *testing.T) {
	body := strings.NewReader(`{
		"answer": "Use the format parameter.\nSee docs for details.",
		"sources": [
			{"title":"Doc A","url":"https://example.com/a"},
			{"title":"Doc B","url":"https://example.com/b"}
		]
	}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Answer") {
		t.Errorf("expected Answer heading:\n%s", out)
	}
	if !strings.Contains(out, "Use the format parameter.") {
		t.Errorf("expected answer body text:\n%s", out)
	}
	if !strings.Contains(out, "Sources") {
		t.Errorf("expected Sources heading:\n%s", out)
	}
	if !strings.Contains(out, "Doc A") || !strings.Contains(out, "Doc B") {
		t.Errorf("expected source rows:\n%s", out)
	}
}

// humanizeKey turns API field names into readable labels.
func TestHumanizeKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"id", "Id"},
		{"name", "Name"},
		{"modelKind", "Model Kind"},
		{"MODEL_KIND", "Model Kind"},
		{"baseModelId", "Base Model Id"},
		{"createdAt", "Created At"},
		{"snake_case_field", "Snake Case Field"},
		{"kebab-case-field", "Kebab Case Field"},
		{"", ""},
	}
	for _, c := range cases {
		if got := humanizeKey(c.in); got != c.want {
			t.Errorf("humanizeKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Error formatter emits plain-text error.
func TestHumanErrorTo(t *testing.T) {
	var buf bytes.Buffer
	HumanErrorTo(&buf, 404, "not found")
	out := buf.String()
	if !strings.Contains(out, "Error: not found") {
		t.Errorf("expected readable error, got: %q", out)
	}
	if !strings.Contains(out, "HTTP 404") {
		t.Errorf("expected status code, got: %q", out)
	}
}
