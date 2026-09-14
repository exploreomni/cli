package result

import (
	"fmt"
	"strings"
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
	order := mergeOrder(rowKeys, seqs)

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

// mergeOrder lays out pivot values in the stream's order. Each row group lists
// its values in the order the API sorted them, but a group may lack some; a
// value first seen in a later group goes after its predecessor in that group
// (or before its successor), so a gap early on doesn't push it to the end.
func mergeOrder(rowKeys []string, seqs map[string][]string) []string {
	var order []string
	placed := map[string]bool{}
	for _, rk := range rowKeys {
		seq := seqs[rk]
		for j, pk := range seq {
			if placed[pk] {
				continue
			}
			at := len(order)
			if j > 0 {
				at = indexOf(order, seq[j-1]) + 1
			} else {
				for _, next := range seq[1:] {
					if placed[next] {
						at = indexOf(order, next)
						break
					}
				}
			}
			order = append(order[:at], append([]string{pk}, order[at:]...)...)
			placed[pk] = true
		}
	}
	return order
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
