package output

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
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

// Null scalar fields are hidden in human mode so a successful response with
// `"error": null` doesn't render as a scary `Error: -` line.
func TestHumanTo_SkipsNullScalars(t *testing.T) {
	body := strings.NewReader(`{
		"error": null,
		"topic": "Order Items",
		"result": [{"month":"Jan 2023","count":1577}]
	}`)
	var buf bytes.Buffer
	if err := HumanTo(&buf, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Error") {
		t.Errorf("expected null error field to be hidden, got:\n%s", out)
	}
	if !strings.Contains(out, "Topic") || !strings.Contains(out, "Order Items") {
		t.Errorf("expected topic to render:\n%s", out)
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

func TestFormatNumber(t *testing.T) {
	tests := map[float64]string{
		0:    "0",
		999:  "999",
		1000: "1000",
		// Separators start at five digits, so a year is left alone.
		2026:     "2026",
		10000:    "10,000",
		-1234567: "-1,234,567",
		// No exponent, and every digit kept: model formats decide decimals
		// for query results; the generic table shows what the API sent.
		1284220.5:          "1,284,220.5",
		1602513.8052352013: "1,602,513.8052352013",
		1.005:              "1.005",
		37.774929:          "37.774929",
		// Below 1 the digits are the whole story, so they all survive.
		0.123456: "0.123456",
		-0.25:    "-0.25",
		1e9:      "1,000,000,000",
		// Whole numbers stay readable well past the float64 integer range;
		// a warehouse ID is read, not skimmed.
		9007199254740992: "9,007,199,254,740,992",
		// Beyond that, fall back to the compact form.
		1e18: "1e+18",
		// A tiny fraction is a magnitude, not a number anyone reads digit by
		// digit, so it keeps the compact form.
		1e-9: "1e-09",
	}
	for in, want := range tests {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", in, got, want)
		}
	}
}

// Large floats used to reach the table as scientific notation.
func TestHumanBytes_LargeFloatInTable(t *testing.T) {
	var buf bytes.Buffer
	body := []byte(`[{"region":"east","revenue":1284220.5}]`)
	if err := HumanBytes(&buf, body); err != nil {
		t.Fatalf("HumanBytes: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "1,284,220.5") {
		t.Errorf("expected a readable number, got:\n%s", out)
	}
	if strings.Contains(out, "e+") {
		t.Errorf("expected no scientific notation, got:\n%s", out)
	}
}

// An id is a value someone copies back into a command, so it keeps its
// digits; the measure beside it still reads as a magnitude.
func TestHumanBytes_IdentifiersAreNotGrouped(t *testing.T) {
	var buf bytes.Buffer
	body := []byte(`[{"id":123456,"connectionId":987654,"revenue":1284220.5}]`)
	if err := HumanBytes(&buf, body); err != nil {
		t.Fatalf("HumanBytes: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"123456", "987654", "1,284,220.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}

func TestTruncate_DoesNotSplitRunes(t *testing.T) {
	s := "متجر إلكتروني - لوحة المبيعات"
	got := truncateCells(s, 10)
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 10 {
		t.Errorf("expected 10 runes, got %d (%q)", n, got)
	}
	if truncateCells("short", 10) != "short" {
		t.Error("a string under the limit should pass through")
	}
}
