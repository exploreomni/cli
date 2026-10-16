package output

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/exploreomni/omni-cli/internal/result"
)

// ResultTable renders a decoded query result with the model's labels and
// formats, pivoted when the query pivots.
func ResultTable(w io.Writer, set *result.Set) {
	if len(set.Rows) == 0 {
		fmt.Fprintln(w, "No results.")
		return
	}
	if p := set.Pivot(); p != nil {
		pivotTable(w, set, p)
		return
	}
	headers := make([]string, len(set.Columns))
	for i, c := range set.Columns {
		headers[i] = singleLine(c.Label)
	}

	t := resultTable(headers, func(col int) bool {
		return col < len(set.Columns) && !set.Columns[col].IsDimension
	})
	for _, r := range set.Rows {
		cells := make([]string, len(set.Columns))
		for i, c := range set.Columns {
			cells[i] = truncateCells(FormatValue(r[i], c), 60)
		}
		t.Row(cells...)
	}
	fmt.Fprintln(w, t.Render())
}

// pivotTable lays a pivot out as the Omni app does: the row dimensions on
// the left, then a column per pivot value and measure, headed by the pivot
// value over the measure's label.
func pivotTable(w io.Writer, set *result.Set, p *result.Pivoted) {
	group := pivotLabel(set, p)
	var headers []string
	for i, c := range p.RowDims {
		top := ""
		if i == len(p.RowDims)-1 {
			top = group
		}
		headers = append(headers, top+"\n"+singleLine(set.Columns[c].Label))
	}
	for _, key := range p.Keys {
		for _, m := range p.Measures {
			headers = append(headers, pivotKey(set, p, key)+"\n"+singleLine(set.Columns[m].Label))
		}
	}

	// Table headers are one line, so the two-line header is the first row,
	// and of the row borders only the one beneath it is kept.
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(styleBorder).
		BorderRow(true).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 {
				return styleHeader
			}
			if col >= len(p.RowDims) {
				return styleNum
			}
			return styleCell
		}).
		Row(headers...)
	for _, r := range p.Rows {
		var cells []string
		for i, c := range p.RowDims {
			cells = append(cells, truncateCells(FormatValue(r.Dims[i], set.Columns[c]), 60))
		}
		for k := range p.Keys {
			for j, m := range p.Measures {
				var v any
				if r.Cells[k] != nil {
					v = r.Cells[k][j]
				}
				cells = append(cells, FormatValue(v, set.Columns[m]))
			}
		}
		t.Row(cells...)
	}
	lines := strings.Split(t.Render(), "\n")
	kept := lines[:0]
	separators := 0
	for i, line := range lines {
		if i > 0 && i < len(lines)-1 && strings.HasPrefix(ansi.Strip(line), "├") {
			if separators++; separators > 1 {
				continue
			}
		}
		kept = append(kept, line)
	}
	fmt.Fprintln(w, strings.Join(kept, "\n"))
	if p.Omitted > 0 {
		fmt.Fprintln(w, styleDim.Render(fmt.Sprintf("… and %d more pivot column%s past the query's column limit", p.Omitted, plural(p.Omitted))))
	}
}

func resultTable(headers []string, numeric func(col int) bool) *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(styleBorder).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return styleHeader
			}
			if col >= 0 && numeric(col) {
				return styleNum
			}
			return styleCell
		})
}

// pivotLabel names what the pivot columns are values of, e.g. "Stage".
func pivotLabel(set *result.Set, p *result.Pivoted) string {
	labels := make([]string, len(p.PivotDims))
	for i, c := range p.PivotDims {
		labels[i] = singleLine(set.Columns[c].Label)
	}
	return strings.Join(labels, " · ")
}

// pivotKey renders one pivot column's values, e.g. "Closed Won".
func pivotKey(set *result.Set, p *result.Pivoted, key []any) string {
	parts := make([]string, len(key))
	for i, v := range key {
		parts[i] = FormatValue(v, set.Columns[p.PivotDims[i]])
	}
	return strings.Join(parts, " · ")
}

// ResultChart draws the result as the Omni app's bar table: each dimension
// a column, each measure a column of bars on its own scale. A pivoted query
// spreads its measures across the pivot values, which share that scale.
// --chart-value narrows the bars to the measures it names.
func ResultChart(w io.Writer, set *result.Set, opts ChartOptions) error {
	if len(set.Rows) == 0 {
		fmt.Fprintln(w, "No results.")
		return nil
	}
	var g *grid
	var err error
	if p := set.Pivot(); p != nil {
		g, err = pivotGrid(set, p, opts)
	} else {
		g, err = flatGrid(set, opts)
	}
	if err != nil {
		return err
	}
	renderGrid(w, g, opts)
	return nil
}

func flatGrid(set *result.Set, opts ChartOptions) (*grid, error) {
	values, err := pickValues(set, opts.Values)
	if err != nil {
		return nil, err
	}
	var labels []int
	for i, c := range set.Columns {
		if c.IsDimension && indexOfInt(values, i) < 0 {
			labels = append(labels, i)
		}
	}

	rows, omitted := capRows(len(set.Rows), opts)
	g := &grid{omittedRows: omitted}
	for _, c := range labels {
		g.labelHeaders = append(g.labelHeaders, singleLine(set.Columns[c].Label))
	}
	for s, v := range values {
		g.cols = append(g.cols, gridCol{header: singleLine(set.Columns[v].Label), scale: s})
	}
	for i, r := range set.Rows[:rows] {
		var ls []string
		for _, c := range labels {
			ls = append(ls, FormatValue(r[c], set.Columns[c]))
		}
		if len(labels) == 0 {
			ls = []string{fmt.Sprintf("%d", i+1)}
		}
		g.labels = append(g.labels, ls)
		for s, v := range values {
			g.cols[s].items = append(g.cols[s].items, chartItem(r[v], set.Columns[v]))
		}
	}
	if len(labels) == 0 {
		g.labelHeaders = []string{"#"}
	}
	return g, nil
}

func pivotGrid(set *result.Set, p *result.Pivoted, opts ChartOptions) (*grid, error) {
	measures := p.Measures
	if len(opts.Values) > 0 {
		measures = nil
		for _, want := range opts.Values {
			i, ok := matchColumn(set.Columns, want)
			if !ok {
				return nil, fmt.Errorf("--chart-value %q is not a column (have: %s)", want, columnNames(set))
			}
			if indexOfInt(p.Measures, i) < 0 || !numericColumn(set, i) {
				return nil, fmt.Errorf("--chart-value %q is not a measure this pivot spreads across its columns", set.Columns[i].Label)
			}
			if indexOfInt(measures, i) < 0 {
				measures = append(measures, i)
			}
		}
	}
	var numeric []int
	for _, m := range measures {
		if numericColumn(set, m) {
			numeric = append(numeric, m)
		}
	}
	if len(numeric) == 0 {
		return nil, fmt.Errorf("--chart found nothing numeric to plot (have: %s)", columnNames(set))
	}

	labels := p.RowDims
	rows, omitted := capRows(len(p.Rows), opts)
	g := &grid{groupLabel: pivotLabel(set, p), omittedRows: omitted, omittedCols: p.Omitted * len(numeric)}
	for _, c := range labels {
		g.labelHeaders = append(g.labelHeaders, singleLine(set.Columns[c].Label))
	}
	if len(labels) == 0 {
		g.labelHeaders = []string{"#"}
	}
	for k, key := range p.Keys {
		for s, m := range numeric {
			header := singleLine(set.Columns[m].Label)
			// One measure needs naming once; with several, each column says which.
			if len(numeric) == 1 && k > 0 {
				header = ""
			}
			g.cols = append(g.cols, gridCol{group: pivotKey(set, p, key), header: header, scale: s})
		}
	}
	for i, r := range p.Rows[:rows] {
		var ls []string
		for _, c := range labels {
			ls = append(ls, FormatValue(r.Dims[indexOfInt(p.RowDims, c)], set.Columns[c]))
		}
		if len(labels) == 0 {
			ls = []string{fmt.Sprintf("%d", i+1)}
		}
		g.labels = append(g.labels, ls)
		col := 0
		for k := range p.Keys {
			for _, m := range numeric {
				var v any
				if r.Cells[k] != nil {
					v = r.Cells[k][indexOfInt(p.Measures, m)]
				}
				g.cols[col].items = append(g.cols[col].items, chartItem(v, set.Columns[m]))
				col++
			}
		}
	}
	return g, nil
}

func chartItem(v any, c result.Column) chartRow {
	var it chartRow
	if f, ok := asFloat(v); ok {
		it.value, it.present = f, true
		it.text = FormatValue(v, c)
	}
	return it
}

func capRows(n int, opts ChartOptions) (rows, omitted int) {
	maxRows := opts.MaxRows
	if maxRows <= 0 {
		maxRows = DefaultChartRows
	}
	if n > maxRows {
		return maxRows, n - maxRows
	}
	return n, 0
}

// pickValues chooses the columns to draw bars for: the ones --chart-value
// names, in that order, else every measure, else the first numeric column.
func pickValues(set *result.Set, wants []string) ([]int, error) {
	if len(wants) > 0 {
		var picked []int
		for _, want := range wants {
			i, ok := matchColumn(set.Columns, want)
			if !ok {
				return nil, fmt.Errorf("--chart-value %q is not a column (have: %s)", want, columnNames(set))
			}
			if !numericColumn(set, i) {
				return nil, fmt.Errorf("--chart-value %q holds no numbers", set.Columns[i].Label)
			}
			if indexOfInt(picked, i) < 0 {
				picked = append(picked, i)
			}
		}
		return picked, nil
	}
	var measures []int
	for i, c := range set.Columns {
		if !c.IsDimension && numericColumn(set, i) {
			measures = append(measures, i)
		}
	}
	if len(measures) > 0 {
		return measures, nil
	}
	for i := range set.Columns {
		if numericColumn(set, i) {
			return []int{i}, nil
		}
	}
	return nil, fmt.Errorf("--chart found nothing numeric to plot (have: %s)", columnNames(set))
}

func indexOfInt(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func numericColumn(set *result.Set, i int) bool {
	for _, r := range set.Rows {
		if r[i] == nil {
			continue
		}
		if _, ok := asFloat(r[i]); ok {
			return true
		}
		return false
	}
	return false
}

func columnNames(set *result.Set) string {
	names := make([]string, len(set.Columns))
	for i, c := range set.Columns {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// matchColumn finds a column by field name or label: exact, then normalized
// ("engaged_sessions_percent" ~ "Engaged Sessions %"), then without a view
// prefix on either side ("count" ~ "deals.count" labelled "Deals Count").
func matchColumn(columns []result.Column, want string) (int, bool) {
	for i, c := range columns {
		if c.Name == want || c.Label == want {
			return i, true
		}
	}
	norm := normalizeColumn(want)
	for i, c := range columns {
		if normalizeColumn(c.Name) == norm || normalizeColumn(c.Label) == norm {
			return i, true
		}
	}
	for i, c := range columns {
		if j := strings.LastIndexByte(c.Name, '.'); j >= 0 && normalizeColumn(c.Name[j+1:]) == norm {
			return i, true
		}
	}
	if i := strings.LastIndexByte(want, '.'); i >= 0 {
		return matchColumn(columns, want[i+1:])
	}
	return 0, false
}

func normalizeColumn(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "%", "percent"))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
