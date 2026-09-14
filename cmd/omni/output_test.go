package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/exploreomni/omni-cli/internal/output"
	"github.com/spf13/cobra"
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
	err := outputResponse(resp, "json", true, nil)
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
	err := outputResponse(resp, "json", false, nil)
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
	err := outputResponse(resp, "human", false, nil)
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

		err := outputResponseTo(&stdout, &stderr, resp, "json", compact, nil)
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

	if err := outputResponseTo(&stdout, &stderr, resp, "human", false, nil); err == nil {
		t.Fatal("expected error for 404 status")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not found") || !strings.Contains(stderr.String(), "404") {
		t.Errorf("stderr = %q, want detail and status", stderr.String())
	}
}

// The whole stderr capture must be one valid JSON document in JSON mode —
// `omni ... 2>err.json` has to produce a parseable file, which it doesn't if
// anything (like cobra's own "Error: ..." line) is appended to the envelope.
func TestOutputResponseTo_StderrIsSingleJSONDocument(t *testing.T) {
	for _, compact := range []bool{true, false} {
		var stdout, stderr bytes.Buffer
		resp := &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"detail":"bad model id","code":"INVALID"}`)),
		}

		err := outputResponseTo(&stdout, &stderr, resp, "json", compact, nil)

		// The caller silences cobra's duplicate line off the back of this type.
		var apiErr *apiError
		if !errors.As(err, &apiErr) {
			t.Fatalf("compact=%v: error = %v, want *apiError", compact, err)
		}
		if apiErr.status != 400 {
			t.Errorf("compact=%v: status = %d, want 400", compact, apiErr.status)
		}

		var envelope struct {
			Error  string          `json:"error"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
			t.Fatalf("compact=%v: stderr is not a single JSON document (%v): %q", compact, err, stderr.String())
		}
		if envelope.Error != "bad model id" {
			t.Errorf("compact=%v: error = %q, want the API's detail", compact, envelope.Error)
		}
		if envelope.Status != 400 {
			t.Errorf("compact=%v: status = %d, want 400", compact, envelope.Status)
		}
		if !strings.Contains(string(envelope.Body), `"INVALID"`) {
			t.Errorf("compact=%v: body = %q, want the API's payload verbatim", compact, string(envelope.Body))
		}
	}
}

// A non-JSON error body (an HTML error page from a proxy, say) must still
// leave valid JSON on stderr.
func TestOutputResponseTo_NonJSONErrorBodyStillJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 502,
		Body:       io.NopCloser(strings.NewReader("<html><body>Bad Gateway</body></html>")),
	}

	if err := outputResponseTo(&stdout, &stderr, resp, "json", true, nil); err == nil {
		t.Fatal("expected error for 502 status")
	}
	var envelope struct {
		Error  string          `json:"error"`
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON (%v): %q", err, stderr.String())
	}
	if !strings.Contains(envelope.Error, "Bad Gateway") {
		t.Errorf("error = %q, want the raw body as the detail", envelope.Error)
	}
	if envelope.Body != nil {
		t.Errorf("body = %q, want it omitted when the payload isn't JSON", string(envelope.Body))
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
}

// Not every 2xx body is JSON: `query run` streams text/ndjson by default and
// returns CSV/XLSX with a result type. Those pass through to stdout byte for
// byte — no re-indenting, no appended newline — and count as success; an
// un-parseable payload is data, not an error.
func TestOutputResponseTo_NonJSONSuccessPassesThrough(t *testing.T) {
	bodies := []struct {
		name string
		body string
	}{
		{"ndjson", "{\"kind\":\"jobs_submitted\"}\n{\"kind\":\"job\"}\n"},
		{"csv", "id,name\n1,widget\n"},
		// A CSV whose last row has no trailing newline: appending one would
		// change the file the user redirected to disk.
		{"csv without trailing newline", "id,name\n1,widget"},
		// Binary payloads (XLSX from a query run body with resultType) must survive verbatim too.
		{"binary", "PK\x03\x04\x14\x00\x00\x00\x08\x00"},
	}
	for _, tc := range bodies {
		for _, compact := range []bool{true, false} {
			var stdout, stderr bytes.Buffer
			resp := &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}

			if err := outputResponseTo(&stdout, &stderr, resp, "json", compact, nil); err != nil {
				t.Fatalf("%s compact=%v: non-JSON 2xx body should succeed, got %v", tc.name, compact, err)
			}
			if stdout.String() != tc.body {
				t.Errorf("%s compact=%v: stdout = %q, want the body byte for byte (%q)", tc.name, compact, stdout.String(), tc.body)
			}
			if stderr.Len() != 0 {
				t.Errorf("%s compact=%v: stderr should be empty, got %q", tc.name, compact, stderr.String())
			}
		}
	}
}

// Human mode passes non-JSON payloads through unchanged as well — the same
// bytes, just on a terminal.
func TestOutputResponseTo_NonJSONSuccessPassesThroughHuman(t *testing.T) {
	const body = "id,name\n1,widget"
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	if err := outputResponseTo(&stdout, &stderr, resp, "human", false, nil); err != nil {
		t.Fatalf("non-JSON 2xx body should succeed, got %v", err)
	}
	if stdout.String() != body {
		t.Errorf("stdout = %q, want the body byte for byte", stdout.String())
	}
}

// A literal `null` error body is valid JSON but says nothing, so the envelope
// omits "body" rather than carrying a JSON null that means the same thing.
func TestOutputResponseTo_NullErrorBodyOmitted(t *testing.T) {
	for _, compact := range []bool{true, false} {
		var stdout, stderr bytes.Buffer
		resp := &http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader("null")),
		}

		if err := outputResponseTo(&stdout, &stderr, resp, "json", compact, nil); err == nil {
			t.Fatalf("compact=%v: expected error for 500 status", compact)
		}
		var envelope map[string]any
		if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
			t.Fatalf("compact=%v: stderr is not valid JSON (%v): %q", compact, err, stderr.String())
		}
		if _, ok := envelope["body"]; ok {
			t.Errorf("compact=%v: envelope = %q, want no \"body\" field for a literal null", compact, stderr.String())
		}
		if envelope["error"] != "HTTP 500" {
			t.Errorf("compact=%v: error = %v, want the status fallback", compact, envelope["error"])
		}
	}
}

// A body that fails mid-read (a truncated response) must not leave a partial
// payload on stdout ahead of the non-zero exit.
func TestOutputResponseTo_ReadFailureWritesNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(&truncatedReader{data: []byte(`{"records":[`)}),
	}

	err := outputResponseTo(&stdout, &stderr, resp, "json", false, nil)
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *apiError so cobra's duplicate line is silenced", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
	// The one-JSON-document contract holds for our own failures too.
	var envelope map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not a single JSON document (%v): %q", err, stderr.String())
	}
	detail, _ := envelope["error"].(string)
	if !strings.Contains(detail, "unexpected EOF") {
		t.Errorf("error = %q, want the read failure", detail)
	}
	if _, ok := envelope["body"]; ok {
		t.Errorf("envelope = %q, want no body when the body couldn't be read", stderr.String())
	}
}

// truncatedReader yields some bytes and then fails, like a connection dropped
// mid-response.
type truncatedReader struct {
	data []byte
	done bool
}

func (r *truncatedReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, fmt.Errorf("unexpected EOF")
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
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

			if err := outputResponseTo(&stdout, &stderr, resp, tc.format, true, nil); err != nil {
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

func TestExtractErrorDetail_NestedErrorObject(t *testing.T) {
	body := json.RawMessage(`{"error":{"code":403,"message":"Invalid bearer token"}}`)
	if got := extractErrorDetail(body, []byte(body), 403); got != "Invalid bearer token" {
		t.Errorf("extractErrorDetail = %q, want the nested message", got)
	}
	// An object without a recognisable message still falls back to the raw body.
	body = json.RawMessage(`{"error":{"code":403}}`)
	if got := extractErrorDetail(body, []byte(body), 403); got != string(body) {
		t.Errorf("extractErrorDetail = %q, want raw body fallback", got)
	}
}

func TestChartOptions(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		format string
		want   *output.ChartOptions
		errs   bool
	}{
		{name: "absent", args: nil},
		{name: "flag", args: []string{"--chart"}, want: &output.ChartOptions{}},
		{
			name: "columns",
			args: []string{"--chart", "--chart-label", "region", "--chart-value", "revenue"},
			want: &output.ChartOptions{Label: "region", Value: "revenue"},
		},
		{name: "rejected with json", args: []string{"--chart"}, format: "json", errs: true},
		{name: "allowed with human", args: []string{"--chart"}, format: "human", want: &output.ChartOptions{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			group := &cobra.Command{Use: "query"}
			addResultFlags(group)
			if err := group.ParseFlags(tc.args); err != nil {
				t.Fatalf("parsing %v: %v", tc.args, err)
			}
			got, err := chartOptions(group, tc.format)
			if tc.errs {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected no chart, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected chart options, got nil")
			}
			// Width comes from the terminal; the rest is the flags.
			got.Width = 0
			got.MaxRows = 0
			if *got != *tc.want {
				t.Errorf("got %+v, want %+v", *got, *tc.want)
			}
		})
	}
}

func TestPrepareBody(t *testing.T) {
	queryRun := map[string]string{openapi.BodyPropsAnnotation: "query,resultType,workbookUrl,planOnly"}
	generate := map[string]string{openapi.BodyPropsAnnotation: "modelId,prompt"}
	tests := []struct {
		name            string
		chart, workbook bool
		props           map[string]string
		body            string
		want            string
		err             string
	}{
		{name: "nothing asked", props: queryRun, body: `{"query":{}}`, want: `{"query":{}}`},
		{name: "chart leaves the body alone", chart: true, props: queryRun, body: `{"query":{"limit":5}}`, want: `{"query":{"limit":5}}`},
		{name: "chart drops a json resultType", chart: true, props: queryRun, body: `{"query":{},"resultType":"json"}`, want: `{"query":{}}`},
		{name: "chart drops a csv resultType", chart: true, props: queryRun, body: `{"query":{},"resultType":"csv"}`, want: `{"query":{}}`},
		{name: "workbook sets the field", workbook: true, props: queryRun, body: `{"query":{}}`, want: `{"query":{},"workbookUrl":true}`},
		{name: "workbook flag wins over a false in the body", workbook: true, props: queryRun, body: `{"query":{},"workbookUrl":false}`, want: `{"query":{},"workbookUrl":true}`},
		{name: "workbook with a resultType is fine", workbook: true, props: queryRun, body: `{"query":{},"resultType":"csv"}`, want: `{"query":{},"resultType":"csv","workbookUrl":true}`},
		{name: "workbook and planOnly conflict", workbook: true, props: queryRun, body: `{"query":{},"planOnly":true}`, err: "planOnly"},
		{name: "workbook on a command without the field", workbook: true, props: generate, body: `{"modelId":"x"}`, err: "not supported"},
		{name: "chart on a command without resultType", chart: true, props: generate, body: `{"modelId":"x"}`, want: `{"modelId":"x"}`},
		{name: "no body", chart: true, props: queryRun, want: ``},
		{name: "not JSON", chart: true, props: queryRun, body: `not json`, want: `not json`},
		// --workbook has nowhere to put workbookUrl: say so rather than
		// sending the request and losing the link silently.
		{name: "workbook with no body", workbook: true, props: queryRun, err: "needs a JSON request body"},
		{name: "workbook with a non-JSON body", workbook: true, props: queryRun, body: `not json`, err: "needs a JSON object"},
		{name: "workbook with null", workbook: true, props: queryRun, body: `null`, err: "needs a JSON object"},
		{name: "chart and workbook with null", chart: true, workbook: true, props: queryRun, body: `null`, err: "needs a JSON object"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "run", Annotations: tc.props}
			got, err := prepareBody(tc.chart, tc.workbook, "human", cmd, []byte(tc.body))
			if tc.err != "" {
				if err == nil {
					t.Fatalf("expected an error, got body %s", got)
				}
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("error %q does not mention %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sameJSON(t, got, []byte(tc.want)) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// The workbook link rides on a header: under the output for a person, on
// stderr as JSON for a machine, nowhere when the API didn't send one.
func TestPrintWorkbookLink(t *testing.T) {
	mk := func(u, ct string) *http.Response {
		h := http.Header{"Content-Type": []string{ct}}
		if u != "" {
			h.Set("X-Omni-Workbook-Url", u)
		}
		return &http.Response{Header: h}
	}
	var stdout, stderr bytes.Buffer
	printWorkbookLink(mk("https://x/e/1", "application/json"), "human", false, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "Open in Omni: https://x/e/1") || stderr.Len() != 0 {
		t.Errorf("human: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	// A passed-through download keeps stdout as the file.
	stdout.Reset()
	stderr.Reset()
	printWorkbookLink(mk("https://x/e/1", "text/csv"), "human", false, &stdout, &stderr)
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Open in Omni: https://x/e/1") {
		t.Errorf("csv: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	printWorkbookLink(mk("https://x/e/1", "application/json"), "json", true, &stdout, &stderr)
	if stdout.Len() != 0 || strings.TrimSpace(stderr.String()) != `{"workbookUrl":"https://x/e/1"}` {
		t.Errorf("json: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	printWorkbookLink(mk("", "application/json"), "human", false, &stdout, &stderr)
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("no header: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// --chart on anything that isn't a query stream is refused: there is no
// field metadata to draw from.
func TestOutputResponse_ChartNeedsAStream(t *testing.T) {
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`[{"region":"east","revenue":10}]`)),
	}
	err := outputResponseTo(&stdout, &stderr, resp, "human", false, &output.ChartOptions{Width: 60})
	if err == nil || !strings.Contains(err.Error(), "not a query stream") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing should reach stdout, got %q", stdout.String())
	}
}

// The same refusal holds for a non-JSON 2xx body: a CSV isn't a stream
// either, so it is refused rather than written out with --chart ignored.
func TestOutputResponse_ChartRefusedBeforePassthrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/csv"}},
		Body:       io.NopCloser(strings.NewReader("region,revenue\neast,10\n")),
	}
	err := outputResponseTo(&stdout, &stderr, resp, "human", false, &output.ChartOptions{Width: 60})
	if err == nil || !strings.Contains(err.Error(), "not a query stream") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing should reach stdout, got %q", stdout.String())
	}
}

// sameJSON compares two bodies structurally, falling back to bytes when
// either isn't JSON.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}
