package result

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// arrowPayload builds the base64 Arrow IPC stream the API puts in a job
// line: country, sessions, engaged %, plus a date column.
func arrowPayload(t *testing.T) string {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "events_ext.country", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "events_ext.sessions", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "events_ext.engaged_sessions_percent", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "events_ext.event_timestamp[date]", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
	}, nil)

	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"United States", "Ireland"}, nil)
	b.Field(1).(*array.Int64Builder).AppendValues([]int64{12526, 838}, nil)
	b.Field(2).(*array.Float64Builder).AppendValues([]float64{0.3992, 0}, []bool{true, false})
	d := arrow.Date32FromTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	b.Field(3).(*array.Date32Builder).AppendValues([]arrow.Date32{d, d}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func jobLine(t *testing.T, id, status string, extra map[string]any) string {
	t.Helper()
	l := map[string]any{
		"job_id": id,
		"status": status,
		"summary": map[string]any{
			"fields": map[string]any{
				"events_ext.country":                  map[string]any{"label": "Country", "is_dimension": true, "data_type": "STRING"},
				"events_ext.sessions":                 map[string]any{"label": "Sessions", "data_type": "NUMBER", "format": map[string]any{"value": "NUMBER_0"}},
				"events_ext.engaged_sessions_percent": map[string]any{"label": "Engaged Sessions %", "data_type": "NUMBER", "format": map[string]any{"value": "percent"}},
				"events_ext.event_timestamp[date]":    map[string]any{"label": "Date", "is_dimension": true, "data_type": "DATE"},
			},
		},
	}
	if status == "COMPLETE" {
		l["result"] = arrowPayload(t)
	}
	for k, v := range extra {
		l[k] = v
	}
	raw, _ := json.Marshal(l)
	return string(raw)
}

func TestParse_DecodesAJob(t *testing.T) {
	body := strings.Join([]string{
		`{"jobs_submitted":{"j1":"r1"}}`,
		jobLine(t, "j1", "COMPLETE", nil),
		`{"remaining_job_ids":[],"timed_out":"false"}`,
	}, "\n")
	st, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(st.Sets) != 1 || len(st.Remaining) != 0 {
		t.Fatalf("expected one set and nothing remaining, got %d / %v", len(st.Sets), st.Remaining)
	}
	set := st.Sets[0]

	// Column order is the schema's (the query's), with the model's metadata
	// looked up by name.
	wantCols := []Column{
		{Name: "events_ext.country", Label: "Country", IsDimension: true, DataType: "STRING"},
		{Name: "events_ext.sessions", Label: "Sessions", DataType: "NUMBER", Format: "NUMBER_0"},
		{Name: "events_ext.engaged_sessions_percent", Label: "Engaged Sessions %", DataType: "NUMBER", Format: "percent"},
		{Name: "events_ext.event_timestamp[date]", Label: "Date", IsDimension: true, DataType: "DATE"},
	}
	for i, want := range wantCols {
		if set.Columns[i] != want {
			t.Errorf("column %d = %+v, want %+v", i, set.Columns[i], want)
		}
	}

	if len(set.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(set.Rows))
	}
	if set.Rows[0][0] != "United States" || set.Rows[0][1] != int64(12526) || set.Rows[0][2] != 0.3992 {
		t.Errorf("row 0 = %v", set.Rows[0])
	}
	if set.Rows[1][2] != nil {
		t.Errorf("null cell should decode as nil, got %v", set.Rows[1][2])
	}
	if d, ok := set.Rows[0][3].(time.Time); !ok || d.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("date cell = %v", set.Rows[0][3])
	}
}

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{"avg_scroll_depth": "Avg Scroll Depth", "amount": "Amount", "is_won": "Is Won", "état_civil": "État Civil"} {
		if got := Humanize(in); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParse_TimedOutStreamReportsRemaining(t *testing.T) {
	body := `{"jobs_submitted":{"j1":"r1"}}` + "\n" + `{"remaining_job_ids":["j1"],"timed_out":"true"}` + "\n"
	st, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Sets) != 0 || len(st.Remaining) != 1 || st.Remaining[0] != "j1" {
		t.Errorf("got sets=%d remaining=%v", len(st.Sets), st.Remaining)
	}
}

func TestParse_FailedJobIsAnError(t *testing.T) {
	// The documented shape: error_type and error_message on the job line.
	body := jobLine(t, "j1", "ERROR", map[string]any{"error_type": "PLAN", "error_message": `No such view "order_items"`})
	_, err := Parse([]byte(body))
	if err == nil || !strings.Contains(err.Error(), `PLAN: No such view "order_items"`) {
		t.Errorf("expected the API's message, got %v", err)
	}
	body = jobLine(t, "j1", "ERROR", map[string]any{"error": map[string]any{"message": "token expired"}})
	if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), "token expired") {
		t.Errorf("structured error should still surface, got %v", err)
	}
}

func TestParse_UnknownFieldKeepsItsName(t *testing.T) {
	// A column the summary doesn't describe still renders, labelled by name.
	body := jobLine(t, "j1", "COMPLETE", map[string]any{"summary": map[string]any{"fields": map[string]any{}}})
	st, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if c := st.Sets[0].Columns[1]; c.Label != "events_ext.sessions" || c.Format != "" {
		t.Errorf("undescribed column = %+v", c)
	}
}
