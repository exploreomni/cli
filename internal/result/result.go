// Package result decodes the query/run and query/wait NDJSON streams: rows
// in query order plus the model's metadata for each column.
package result

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// Column is one field of a result set.
type Column struct {
	Name        string // fully qualified, e.g. events_ext.sessions
	Label       string
	IsDimension bool
	DataType    string // NUMBER, STRING, TIMESTAMP, ...
	Format      string // NUMBER_0, percent, ... ; empty when the model sets none
}

// Set is one job's decoded result.
type Set struct {
	JobID   string
	Columns []Column
	Rows    [][]any // string, int64, float64, bool, time.Time, or nil
	// Pivots names the columns the query pivots on. The stream carries rows
	// in long form either way; Pivot reshapes them.
	Pivots []string
	// ColumnLimit caps the pivot columns shown, as the query sets it; 0 means none.
	ColumnLimit int
}

// Stream is one parsed query/run or query/wait response.
type Stream struct {
	Sets []*Set
	// Remaining lists jobs still running; poll query/wait with them until empty.
	Remaining []string
	// Failures lists jobs that did not complete. One bad job doesn't void the
	// others: the caller renders what decoded and reports these alongside.
	Failures []Failure
}

// Failure is one job the API could not complete, or whose result would not decode.
type Failure struct {
	JobID   string
	Status  string
	Message string
}

func (f Failure) Error() string {
	if f.Status == "" {
		return fmt.Sprintf("query job %s: %s", f.JobID, f.Message)
	}
	return fmt.Sprintf("query job %s %s: %s", f.JobID, f.Status, f.Message)
}

// Err reports the stream's failed jobs as a single error, or nil if none failed.
func (s *Stream) Err() error {
	switch len(s.Failures) {
	case 0:
		return nil
	case 1:
		return s.Failures[0]
	}
	msgs := make([]string, len(s.Failures))
	for i, f := range s.Failures {
		msgs[i] = f.Error()
	}
	return errors.New(strings.Join(msgs, "; "))
}

type line struct {
	JobsSubmitted map[string]string `json:"jobs_submitted"`
	JobID         string            `json:"job_id"`
	Status        string            `json:"status"`
	Summary       *summary          `json:"summary"`
	Query         *struct {
		ModelJob struct {
			Fields      []string `json:"fields"`
			Pivots      []string `json:"pivots"`
			ColumnLimit int      `json:"column_limit"`
		} `json:"model_job"`
	} `json:"query"`
	Result          string          `json:"result"`
	ErrorType       string          `json:"error_type"`
	ErrorMessage    string          `json:"error_message"`
	Error           json.RawMessage `json:"error"`
	RemainingJobIDs []string        `json:"remaining_job_ids"`
}

type summary struct {
	Fields map[string]fieldMeta `json:"fields"`
}

type fieldMeta struct {
	FieldName   string `json:"field_name"`
	Label       string `json:"label"`
	IsDimension bool   `json:"is_dimension"`
	DataType    string `json:"data_type"`
	Format      *struct {
		Value string `json:"value"`
	} `json:"format"`
}

// Parse decodes a stream body. A job that failed or would not decode is
// collected in Stream.Failures rather than aborting the whole stream, so the
// jobs that did complete are still rendered.
func Parse(data []byte) (*Stream, error) {
	var st Stream
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<30)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("query stream: %w", err)
		}
		switch {
		case l.RemainingJobIDs != nil:
			st.Remaining = l.RemainingJobIDs
		case l.JobID != "":
			if l.Status != "COMPLETE" {
				st.Failures = append(st.Failures, Failure{JobID: l.JobID, Status: l.Status, Message: jobError(l)})
				continue
			}
			set, err := decodeJob(l)
			if err != nil {
				st.Failures = append(st.Failures, Failure{JobID: l.JobID, Message: err.Error()})
				continue
			}
			st.Sets = append(st.Sets, set)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("query stream: %w", err)
	}
	return &st, nil
}

func jobError(l line) string {
	if l.ErrorMessage != "" {
		if l.ErrorType != "" {
			return l.ErrorType + ": " + l.ErrorMessage
		}
		return l.ErrorMessage
	}
	if len(l.Error) == 0 {
		return "no detail"
	}
	var s string
	if json.Unmarshal(l.Error, &s) == nil {
		return s
	}
	var obj map[string]any
	if json.Unmarshal(l.Error, &obj) == nil {
		for _, k := range []string{"message", "detail", "error"} {
			if v, ok := obj[k].(string); ok && v != "" {
				return v
			}
		}
	}
	return string(l.Error)
}

func decodeJob(l line) (*Set, error) {
	if l.Result == "" {
		return nil, fmt.Errorf("job carries no result (a planOnly query has none to render; use --format json)")
	}
	raw, err := base64.StdEncoding.DecodeString(l.Result)
	if err != nil {
		return nil, fmt.Errorf("result is not base64: %w", err)
	}
	rd, err := ipc.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("result is not an Arrow stream: %w", err)
	}
	defer rd.Release()

	// The batch carries extra columns (sort keys, primary key, "__raw" twins),
	// so the query's own field list decides which show and in what order.
	schema := rd.Schema()
	var picked []int
	if l.Query != nil && len(l.Query.ModelJob.Fields) > 0 {
		for _, name := range l.Query.ModelJob.Fields {
			idx := schema.FieldIndices(name)
			if len(idx) == 0 {
				// A partial match would silently drop columns; fall back to
				// the summary's field set, which describes what arrived.
				picked = nil
				break
			}
			picked = append(picked, idx[0])
		}
	}
	if len(picked) == 0 {
		described := l.Summary != nil && len(l.Summary.Fields) > 0
		for i, f := range schema.Fields() {
			if !described {
				picked = append(picked, i)
			} else if _, ok := l.Summary.Fields[f.Name]; ok {
				picked = append(picked, i)
			}
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("result has no columns the query asked for")
	}

	set := &Set{JobID: l.JobID}
	if l.Query != nil {
		set.Pivots = l.Query.ModelJob.Pivots
		set.ColumnLimit = l.Query.ModelJob.ColumnLimit
	}
	for _, i := range picked {
		f := schema.Field(i)
		col := Column{Name: f.Name, Label: f.Name}
		if l.Summary != nil {
			if m, ok := l.Summary.Fields[f.Name]; ok {
				col.Label = m.Label
				if col.Label == "" && m.FieldName != "" {
					col.Label = Humanize(m.FieldName)
				}
				col.IsDimension = m.IsDimension
				col.DataType = m.DataType
				if m.Format != nil {
					col.Format = m.Format.Value
				}
			}
		}
		set.Columns = append(set.Columns, col)
	}

	for rd.Next() {
		rec := rd.RecordBatch()
		n := int(rec.NumRows())
		for r := 0; r < n; r++ {
			row := make([]any, len(picked))
			for j, c := range picked {
				row[j] = value(rec.Column(c), r)
			}
			set.Rows = append(set.Rows, row)
		}
	}
	if err := rd.Err(); err != nil {
		return nil, fmt.Errorf("reading Arrow batches: %w", err)
	}
	return set, nil
}

// Humanize turns "avg_scroll_depth" into "Avg Scroll Depth", as Omni labels unlabelled fields.
func Humanize(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// value converts one Arrow cell: ints to int64, floats and decimals to
// float64, anything unhandled to Arrow's string form.
func value(col arrow.Array, i int) any {
	if col.IsNull(i) {
		return nil
	}
	switch a := col.(type) {
	case *array.String:
		return a.Value(i)
	case *array.LargeString:
		return a.Value(i)
	case *array.Boolean:
		return a.Value(i)
	case *array.Int8:
		return int64(a.Value(i))
	case *array.Int16:
		return int64(a.Value(i))
	case *array.Int32:
		return int64(a.Value(i))
	case *array.Int64:
		return a.Value(i)
	case *array.Uint8:
		return int64(a.Value(i))
	case *array.Uint16:
		return int64(a.Value(i))
	case *array.Uint32:
		return int64(a.Value(i))
	case *array.Uint64:
		return int64(a.Value(i))
	case *array.Float32:
		return float64(a.Value(i))
	case *array.Float64:
		return a.Value(i)
	case *array.Decimal128:
		return a.Value(i).ToFloat64(a.DataType().(*arrow.Decimal128Type).Scale)
	case *array.Decimal256:
		return a.Value(i).ToFloat64(a.DataType().(*arrow.Decimal256Type).Scale)
	case *array.Timestamp:
		unit := a.DataType().(*arrow.TimestampType).Unit
		return a.Value(i).ToTime(unit).UTC()
	case *array.Date32:
		return a.Value(i).ToTime().UTC()
	case *array.Date64:
		return a.Value(i).ToTime().UTC()
	}
	return col.ValueStr(i)
}
