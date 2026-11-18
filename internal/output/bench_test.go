package output

import (
	"fmt"
	"io"
	"testing"

	"github.com/exploreomni/omni-cli/internal/result"
)

func setOfSize(n int) *result.Set {
	set := &result.Set{Columns: []result.Column{
		{Name: "e.country", Label: "Country", IsDimension: true, DataType: "STRING"},
		{Name: "e.sessions", Label: "Sessions", DataType: "NUMBER", Format: "NUMBER_0"},
		{Name: "e.pct", Label: "Pct", DataType: "NUMBER", Format: "percent"},
	}}
	for i := 0; i < n; i++ {
		set.Rows = append(set.Rows, []any{fmt.Sprintf("country-%d", i%200), int64(i * 7), float64(i%100) / 100})
	}
	return set
}

func BenchmarkRender(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		set := setOfSize(n)
		b.Run(fmt.Sprintf("table/rows=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ResultTable(io.Discard, set)
			}
		})
		b.Run(fmt.Sprintf("chart/rows=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ResultChart(io.Discard, set, ChartOptions{Width: 100, MaxRows: n})
			}
		})
	}
}
