package output

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/exploreomni/omni-cli/internal/result"
)

// ResultTable renders a decoded query result with the model's labels and formats.
func ResultTable(w io.Writer, set *result.Set) {
	if len(set.Rows) == 0 {
		fmt.Fprintln(w, "No results.")
		return
	}
	headers := make([]string, len(set.Columns))
	for i, c := range set.Columns {
		headers[i] = c.Label
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(styleBorder).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return styleHeader
			}
			if col >= 0 && col < len(set.Columns) && !set.Columns[col].IsDimension {
				return styleNum
			}
			return styleCell
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

// ResultChart draws one measure as bars, labelled by one dimension; both
// default to the model's first and can be named by field name or label.
func ResultChart(w io.Writer, set *result.Set, opts ChartOptions) error {
	if opts.Kind != "" && opts.Kind != ChartKindBar {
		return fmt.Errorf("unknown chart kind %q (supported: %s)", opts.Kind, ChartKindBar)
	}
	if len(set.Rows) == 0 {
		fmt.Fprintln(w, "No results.")
		return nil
	}

	valueIdx, err := pickValue(set, opts.Value)
	if err != nil {
		return err
	}
	labelIdx, err := pickLabel(set, opts.Label, valueIdx)
	if err != nil {
		return err
	}

	maxRows := opts.MaxRows
	if maxRows <= 0 {
		maxRows = DefaultChartRows
	}
	rows, omitted := set.Rows, 0
	if len(rows) > maxRows {
		rows, omitted = rows[:maxRows], len(rows)-maxRows
	}

	items := make([]chartRow, 0, len(rows))
	for i, r := range rows {
		it := chartRow{label: fmt.Sprintf("%d", i+1)}
		if labelIdx >= 0 {
			it.label = FormatValue(r[labelIdx], set.Columns[labelIdx])
		}
		if f, ok := asFloat(r[valueIdx]); ok {
			it.value, it.present = f, true
			it.text = FormatValue(r[valueIdx], set.Columns[valueIdx])
		}
		items = append(items, it)
	}

	labelHeader := "#"
	if labelIdx >= 0 {
		labelHeader = set.Columns[labelIdx].Label
	}
	renderBars(w, labelHeader, set.Columns[valueIdx].Label, items, omitted, opts)
	return nil
}

func pickValue(set *result.Set, want string) (int, error) {
	if want != "" {
		i, ok := matchColumn(set.Columns, want)
		if !ok {
			return 0, fmt.Errorf("--chart-value %q is not a column (have: %s)", want, columnNames(set))
		}
		if !numericColumn(set, i) {
			return 0, fmt.Errorf("--chart-value %q holds no numbers", set.Columns[i].Label)
		}
		return i, nil
	}
	for i, c := range set.Columns {
		if !c.IsDimension && numericColumn(set, i) {
			return i, nil
		}
	}
	// No measures at all: the first numeric column stands in.
	for i := range set.Columns {
		if numericColumn(set, i) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("--chart found nothing numeric to plot (have: %s)", columnNames(set))
}

func pickLabel(set *result.Set, want string, valueIdx int) (int, error) {
	if want != "" {
		i, ok := matchColumn(set.Columns, want)
		if !ok {
			return 0, fmt.Errorf("--chart-label %q is not a column (have: %s)", want, columnNames(set))
		}
		return i, nil
	}
	for i, c := range set.Columns {
		if c.IsDimension && i != valueIdx {
			return i, nil
		}
	}
	return -1, nil
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
// ("engaged_sessions_percent" ~ "Engaged Sessions %"), then without a view prefix.
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
