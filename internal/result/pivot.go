package result

import (
	"cmp"
	"fmt"
	"strings"
	"time"
)

// Pivoted is a Set reshaped as the query's pivots ask: one row per distinct
// value of the remaining dimensions, one column per pivot value and measure.
type Pivoted struct {
	RowDims   []int   // column indices of the dimensions that stay as rows
	PivotDims []int   // column indices pivoted into column headers
	Measures  []int   // column indices filling the cells
	Keys      [][]any // pivot values, one tuple per pivot column, in display order
	Rows      []PivotRow
	// Omitted counts pivot values dropped by the query's column limit.
	Omitted int
}

// PivotRow is one row of a pivoted result.
type PivotRow struct {
	Dims  []any   // values of RowDims
	Cells [][]any // [key][measure]; nil where the combination has no row
}

// Pivot reshapes the set, or returns nil when it isn't pivoted: no pivot
// field among its columns, or no measure to spread across them.
func (s *Set) Pivot() *Pivoted {
	if len(s.Pivots) == 0 {
		return nil
	}
	pivoted := map[string]bool{}
	for _, name := range s.Pivots {
		pivoted[name] = true
	}
	p := &Pivoted{}
	for i, c := range s.Columns {
		switch {
		case pivoted[c.Name]:
			p.PivotDims = append(p.PivotDims, i)
		case c.IsDimension:
			p.RowDims = append(p.RowDims, i)
		default:
			p.Measures = append(p.Measures, i)
		}
	}
	if len(p.PivotDims) == 0 || len(p.Measures) == 0 {
		return nil
	}

	tuples := map[string][]any{}
	rowIndex := map[string]int{}
	var rowKeys []string
	seqs := map[string][]string{} // row key -> its pivot keys, in stream order
	type cell struct{ row, key string }
	values := map[cell][]any{}

	for _, r := range s.Rows {
		rk := tupleKey(r, p.RowDims)
		pk := tupleKey(r, p.PivotDims)
		if _, ok := rowIndex[rk]; !ok {
			rowIndex[rk] = len(p.Rows)
			rowKeys = append(rowKeys, rk)
			p.Rows = append(p.Rows, PivotRow{Dims: pick(r, p.RowDims)})
		}
		if _, ok := tuples[pk]; !ok {
			tuples[pk] = pick(r, p.PivotDims)
		}
		seqs[rk] = append(seqs[rk], pk)
		values[cell{rk, pk}] = pick(r, p.Measures)
	}
	desc := make([]bool, len(p.PivotDims))
	for i, c := range p.PivotDims {
		desc[i] = s.Descending[s.Columns[c].Name]
	}
	order := mergeOrder(rowKeys, seqs, func(a, b string) bool {
		return compareTuples(tuples[a], tuples[b], desc) < 0
	})

	if s.ColumnLimit > 0 && len(order) > s.ColumnLimit {
		p.Omitted = len(order) - s.ColumnLimit
		order = order[:s.ColumnLimit]
	}
	for _, pk := range order {
		p.Keys = append(p.Keys, tuples[pk])
	}
	for i := range p.Rows {
		p.Rows[i].Cells = make([][]any, len(order))
		for k, pk := range order {
			p.Rows[i].Cells[k] = values[cell{rowKeys[i], pk}]
		}
	}
	return p
}

func pick(row []any, idx []int) []any {
	out := make([]any, len(idx))
	for i, c := range idx {
		out[i] = row[c]
	}
	return out
}

// tupleKey identifies a tuple of cell values; the type is part of the key so
// the string "1" and the number 1 stay distinct.
func tupleKey(row []any, idx []int) string {
	var b strings.Builder
	for _, c := range idx {
		fmt.Fprintf(&b, "%T:%v\x00", row[c], row[c])
	}
	return b.String()
}

// mergeOrder lays out pivot values. Each row group lists its values in the
// order the API sorted them; those orders are merged, and values they don't
// relate (never in the same group) fall back to the pivot fields' own order.
func mergeOrder(rowKeys []string, seqs map[string][]string, less func(a, b string) bool) []string {
	var nodes []string
	indegree := map[string]int{}
	next := map[string]map[string]bool{}
	for _, rk := range rowKeys {
		seq := seqs[rk]
		for j, pk := range seq {
			if _, ok := indegree[pk]; !ok {
				indegree[pk] = 0
				nodes = append(nodes, pk)
			}
			if j == 0 || seq[j-1] == pk || next[seq[j-1]][pk] {
				continue
			}
			if next[seq[j-1]] == nil {
				next[seq[j-1]] = map[string]bool{}
			}
			next[seq[j-1]][pk] = true
			indegree[pk]++
		}
	}

	order := make([]string, 0, len(nodes))
	done := map[string]bool{}
	for len(order) < len(nodes) {
		// The least ready value; if the groups' orders conflict, none is
		// ready, and the least remaining value breaks the cycle.
		best := ""
		for _, ready := range []bool{true, false} {
			for _, n := range nodes {
				if done[n] || (ready && indegree[n] > 0) {
					continue
				}
				if best == "" || less(n, best) {
					best = n
				}
			}
			if best != "" {
				break
			}
		}
		done[best] = true
		order = append(order, best)
		for n := range next[best] {
			indegree[n]--
		}
	}
	return order
}

// compareTuples orders pivot values as their fields sort: numbers and times
// by value, text lexically, nulls last; desc flips a field.
func compareTuples(a, b []any, desc []bool) int {
	for i := range a {
		c := compareValues(a[i], b[i])
		if desc[i] {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return 0
}

func compareValues(a, b any) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			return cmp.Compare(x, y)
		}
	}
	if x, ok := a.(time.Time); ok {
		if y, ok := b.(time.Time); ok {
			return x.Compare(y)
		}
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
