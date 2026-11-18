package result

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// StreamOfSize builds a realistic job line: one string dimension, one
// int measure, one float measure, n rows.
func StreamOfSize(n int) []byte {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "e.country", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "e.sessions", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "e.pct", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	for i := 0; i < n; i++ {
		b.Field(0).(*array.StringBuilder).Append(fmt.Sprintf("country-%d", i%200))
		b.Field(1).(*array.Int64Builder).Append(int64(i * 7))
		b.Field(2).(*array.Float64Builder).Append(float64(i%100) / 100)
	}
	rec := b.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	_ = w.Write(rec)
	_ = w.Close()
	line, _ := json.Marshal(map[string]any{
		"job_id": "j", "status": "COMPLETE",
		"summary": map[string]any{"fields": map[string]any{
			"e.country":  map[string]any{"label": "Country", "is_dimension": true},
			"e.sessions": map[string]any{"label": "Sessions", "format": map[string]any{"value": "NUMBER_0"}},
			"e.pct":      map[string]any{"label": "Pct", "format": map[string]any{"value": "percent"}},
		}},
		"result": base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
	return append(append([]byte(`{"jobs_submitted":{"j":"r"}}`+"\n"), line...), []byte("\n"+`{"remaining_job_ids":[]}`+"\n")...)
}

func BenchmarkParse(b *testing.B) {
	for _, n := range []int{100, 1000, 10000, 100000} {
		data := StreamOfSize(n)
		b.Run(fmt.Sprintf("rows=%d/bytes=%d", n, len(data)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Parse(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
