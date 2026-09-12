package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
)

func completedJob(t *testing.T, id string) string {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "e.country", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "e.sessions", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"United States", "Ireland"}, nil)
	b.Field(1).(*array.Int64Builder).AppendValues([]int64{12526, 838}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	w.Close()

	line, _ := json.Marshal(map[string]any{
		"job_id": id, "status": "COMPLETE",
		"summary": map[string]any{"fields": map[string]any{
			"e.country":  map[string]any{"label": "Country", "is_dimension": true, "data_type": "STRING"},
			"e.sessions": map[string]any{"label": "Sessions", "data_type": "NUMBER", "format": map[string]any{"value": "NUMBER_0"}},
		}},
		"result": base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
	return string(line)
}

func streamResp(body string, hdr map[string]string) *http.Response {
	h := http.Header{"Content-Type": []string{"text/ndjson"}}
	for k, v := range hdr {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

// A stream whose wait window elapsed is followed up on query/wait until
// every job has finished, and the result renders as if it had come at once.
func TestRenderStream_PollsUntilComplete(t *testing.T) {
	var waits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query/wait" {
			t.Errorf("unexpected request %s", r.URL)
		}
		waits = append(waits, r.URL.Query().Get("jobIds"))
		w.Header().Set("Content-Type", "text/ndjson")
		if len(waits) == 1 {
			fmt.Fprintln(w, `{"remaining_job_ids":["j1"],"timed_out":"true"}`)
			return
		}
		fmt.Fprintln(w, completedJob(t, "j1"))
		fmt.Fprintln(w, `{"remaining_job_ids":[],"timed_out":"false"}`)
	}))
	defer srv.Close()
	cfg := &config.ResolvedConfig{BaseURL: srv.URL, Token: "t"}

	first := `{"jobs_submitted":{"j1":"r1"}}` + "\n" + `{"remaining_job_ids":["j1"],"timed_out":"true"}` + "\n"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := renderStream(cfg, streamResp(first, map[string]string{"X-Omni-Workbook-Url": "https://acme.omniapp.co/e/1:abc/1"}), "human", false, &output.ChartOptions{Width: 60}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("renderStream: %v", err)
	}
	if len(waits) != 2 || waits[0] != "j1" {
		t.Errorf("expected two waits on j1, got %v", waits)
	}
	out := stdout.String()
	for _, want := range []string{"United States", "12,526", "▇", "Open in Omni: https://acme.omniapp.co/e/1:abc/1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Without --chart, a stream in human mode is the model's table.
func TestRenderStream_TableWhenNotCharting(t *testing.T) {
	body := `{"jobs_submitted":{"j1":"r1"}}` + "\n" + completedJob(t, "j1") + "\n" + `{"remaining_job_ids":[],"timed_out":"false"}` + "\n"
	var stdout bytes.Buffer
	if err := renderStream(&config.ResolvedConfig{}, streamResp(body, nil), "human", false, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Country") || !strings.Contains(out, "12,526") || strings.Contains(out, "▇") {
		t.Errorf("expected a table, got:\n%s", out)
	}
	if strings.Contains(out, "Open in Omni") {
		t.Errorf("no link header, no link line:\n%s", out)
	}
}

func TestRenderStream_FailedJob(t *testing.T) {
	body := `{"job_id":"j1","status":"ERROR","error":{"message":"No such field \"e.nope\""}}` + "\n"
	var stdout bytes.Buffer
	err := renderStream(&config.ResolvedConfig{}, streamResp(body, nil), "human", false, nil, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `No such field "e.nope"`) {
		t.Fatalf("expected the API's message, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing should reach stdout, got %q", stdout.String())
	}
}

// A failing query/wait poll reports like any failed API call: one JSON
// envelope on stderr in JSON mode, nothing on stdout.
func TestRenderStream_WaitErrorUsesTheEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"code":401,"message":"token expired"}}`)
	}))
	defer srv.Close()
	first := `{"jobs_submitted":{"j1":"r1"}}` + "\n" + `{"remaining_job_ids":["j1"],"timed_out":"true"}` + "\n"
	var stdout, stderr bytes.Buffer
	err := renderStream(&config.ResolvedConfig{BaseURL: srv.URL, Token: "t"}, streamResp(first, nil), "json", true, &output.ChartOptions{Width: 60}, &stdout, &stderr)
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.status != 401 {
		t.Fatalf("expected an apiError 401, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing should reach stdout, got %q", stdout.String())
	}
	var env map[string]any
	if json.Unmarshal(stderr.Bytes(), &env) != nil || env["status"] != float64(401) || env["error"] != "token expired" {
		t.Errorf("stderr should be one JSON envelope, got %q", stderr.String())
	}
}

// A failure on a later job leaves nothing on stdout.
func TestRenderStream_LaterSetErrorWritesNothing(t *testing.T) {
	body := `{"jobs_submitted":{"j1":"r1","j2":"r2"}}` + "\n" + completedJob(t, "j1") + "\n" +
		`{"job_id":"j2","status":"ERROR","error_type":"PLAN","error_message":"No such view"}` + "\n"
	var stdout bytes.Buffer
	err := renderStream(&config.ResolvedConfig{}, streamResp(body, nil), "human", false, &output.ChartOptions{Width: 60}, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "No such view") {
		t.Fatalf("expected the second job's error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing should reach stdout, got %q", stdout.String())
	}
}

func TestIsQueryStream(t *testing.T) {
	if !isQueryStream(streamResp("", nil)) {
		t.Error("text/ndjson 200 is a stream")
	}
	if isQueryStream(&http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"text/ndjson"}}}) {
		t.Error("a failure is not a stream to render")
	}
	if isQueryStream(&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}}) {
		t.Error("JSON is not a stream")
	}
}
