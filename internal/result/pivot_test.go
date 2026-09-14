package result

import (
	"reflect"
	"testing"
)

// Region × stage, total amount pivoted on stage, as the stream sends it:
// long form, sorted by region then stage, EMEA with no Negotiation deals.
func pipelineSet() *Set {
	return &Set{
		Columns: []Column{
			{Name: "deals.region", Label: "Region", IsDimension: true},
			{Name: "deals.stage", Label: "Stage", IsDimension: true},
			{Name: "deals.total_amount", Label: "Total amount", DataType: "NUMBER"},
		},
		Rows: [][]any{
			{"AMER", "Lost", int64(13)},
			{"AMER", "Negotiation", int64(1)},
			{"AMER", "Won", int64(3)},
			{"EMEA", "Lost", int64(8)},
			{"EMEA", "Won", int64(2)},
		},
		Pivots: []string{"deals.stage"},
	}
}

func TestPivot_Reshapes(t *testing.T) {
	p := pipelineSet().Pivot()
	if p == nil {
		t.Fatal("expected a pivot")
	}
	if !reflect.DeepEqual(p.RowDims, []int{0}) || !reflect.DeepEqual(p.PivotDims, []int{1}) || !reflect.DeepEqual(p.Measures, []int{2}) {
		t.Fatalf("roles: rows %v pivots %v measures %v", p.RowDims, p.PivotDims, p.Measures)
	}
	if want := [][]any{{"Lost"}, {"Negotiation"}, {"Won"}}; !reflect.DeepEqual(p.Keys, want) {
		t.Errorf("keys = %v, want %v", p.Keys, want)
	}
	if len(p.Rows) != 2 || p.Rows[1].Dims[0] != "EMEA" {
		t.Fatalf("rows = %+v", p.Rows)
	}
	if p.Rows[1].Cells[1] != nil {
		t.Errorf("EMEA has no Negotiation row, got %v", p.Rows[1].Cells[1])
	}
	if p.Rows[1].Cells[2][0] != int64(2) {
		t.Errorf("EMEA won = %v", p.Rows[1].Cells[2])
	}
}

// A value the first row group lacks lands where the later group puts it,
// not at the end.
func TestPivot_OrderMergesAcrossGroups(t *testing.T) {
	set := pipelineSet()
	set.Rows = [][]any{
		{"AMER", "Lost", int64(13)},
		{"AMER", "Won", int64(3)},
		{"EMEA", "Lost", int64(8)},
		{"EMEA", "Negotiation", int64(1)},
		{"EMEA", "Won", int64(2)},
		{"APAC", "Early", int64(1)},
		{"APAC", "Lost", int64(1)},
	}
	want := [][]any{{"Early"}, {"Lost"}, {"Negotiation"}, {"Won"}}
	if got := set.Pivot().Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func TestPivot_ColumnLimit(t *testing.T) {
	set := pipelineSet()
	set.ColumnLimit = 2
	p := set.Pivot()
	if len(p.Keys) != 2 || p.Omitted != 1 || len(p.Rows[0].Cells) != 2 {
		t.Errorf("keys %v omitted %d", p.Keys, p.Omitted)
	}
}

func TestPivot_NotPivoted(t *testing.T) {
	set := pipelineSet()
	set.Pivots = nil
	if set.Pivot() != nil {
		t.Error("no pivots: no pivot")
	}
	set.Pivots = []string{"deals.nope"}
	if set.Pivot() != nil {
		t.Error("a pivot on a field not in the result: no pivot")
	}
	set = pipelineSet()
	set.Columns = set.Columns[:2]
	for i := range set.Rows {
		set.Rows[i] = set.Rows[i][:2]
	}
	if set.Pivot() != nil {
		t.Error("nothing to spread across the columns: no pivot")
	}
}

// Values no row group relates fall back to their own order: row A has Q2
// and Q4, row B Q1 and Q3.
func TestPivot_UnrelatedValuesUseValueOrder(t *testing.T) {
	set := pipelineSet()
	set.Rows = [][]any{
		{"A", "Q2", int64(1)},
		{"A", "Q4", int64(1)},
		{"B", "Q1", int64(1)},
		{"B", "Q3", int64(1)},
	}
	want := [][]any{{"Q1"}, {"Q2"}, {"Q3"}, {"Q4"}}
	if got := set.Pivot().Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	set.Descending = map[string]bool{"deals.stage": true}
	set.Rows = [][]any{
		{"A", "Q4", int64(1)},
		{"A", "Q2", int64(1)},
		{"B", "Q3", int64(1)},
		{"B", "Q1", int64(1)},
	}
	want = [][]any{{"Q4"}, {"Q3"}, {"Q2"}, {"Q1"}}
	if got := set.Pivot().Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("descending: keys = %v, want %v", got, want)
	}
}
