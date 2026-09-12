package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/exploreomni/omni-cli/internal/result"
)

func col(name, label string, dim bool, format string) result.Column {
	return result.Column{Name: name, Label: label, IsDimension: dim, DataType: "NUMBER", Format: format}
}

// The Ireland/US query as the stream decodes it.
func sessionsSet() *result.Set {
	return &result.Set{
		Columns: []result.Column{
			{Name: "events_ext.country", Label: "Country", IsDimension: true, DataType: "STRING"},
			col("events_ext.sessions", "Sessions", false, "NUMBER_0"),
			col("events_ext.engaged_sessions_percent", "Engaged Sessions %", false, "percent"),
		},
		Rows: [][]any{
			{"United States", int64(12526), 0.3992495609133003},
			{"Ireland", int64(838), 0.4486873508353222},
		},
	}
}

// headerIs reports whether the first line carries the two column headers,
// label left and value right, in that order.
func headerIs(out, label, value string) bool {
	first, _, _ := strings.Cut(out, "\n")
	fields := strings.Fields(first)
	return len(fields) >= 2 && strings.HasPrefix(first, label) && strings.HasSuffix(strings.TrimRight(first, " "), value)
}

// The headers sit over their columns: the label header flush left, the
// value header right-aligned to the numbers beneath it.
func TestChart_HeadersAlignToColumns(t *testing.T) {
	out := chart(t, sessionsSet(), ChartOptions{})
	lines := strings.Split(out, "\n")
	header, row := lines[0], lines[1]
	hEnd := strings.LastIndex(header, "Sessions") + len("Sessions")
	vEnd := strings.Index(row, "12,526") + len("12,526")
	if lipgloss.Width(header[:hEnd]) != lipgloss.Width(row[:vEnd]) {
		t.Errorf("value header should end where the values end:\n%s", out)
	}
}

func chart(t *testing.T, set *result.Set, opts ChartOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if opts.Width == 0 {
		opts.Width = 60
	}
	if err := ResultChart(&buf, set, opts); err != nil {
		t.Fatalf("ResultChart: %v", err)
	}
	return buf.String()
}

func chartErr(t *testing.T, set *result.Set, opts ChartOptions) string {
	t.Helper()
	var buf bytes.Buffer
	err := ResultChart(&buf, set, opts)
	if err == nil {
		t.Fatalf("expected an error, got output:\n%s", buf.String())
	}
	return err.Error()
}

// The model says which column is the dimension and which the measure; the
// chart doesn't guess.
func TestChart_ColumnsFromModel(t *testing.T) {
	out := chart(t, sessionsSet(), ChartOptions{})
	if !headerIs(out, "Country", "Sessions") {
		t.Fatalf("expected the first dimension and first measure, got:\n%s", out)
	}
	if !strings.Contains(out, "12,526") {
		t.Errorf("NUMBER_0 should group digits:\n%s", out)
	}
}

func TestChart_ValueFormattedByModel(t *testing.T) {
	out := chart(t, sessionsSet(), ChartOptions{Value: "engaged_sessions_percent"})
	if !headerIs(out, "Country", "Engaged Sessions %") {
		t.Fatalf("expected the percent measure, got:\n%s", out)
	}
	if !strings.Contains(out, "39.9%") || !strings.Contains(out, "44.9%") {
		t.Errorf("percent format should apply:\n%s", out)
	}
}

// Rows are keyed by field name, the model by label; either spelling should
// find the column.
func TestChart_ColumnSpellings(t *testing.T) {
	for _, spelling := range []string{
		"Engaged Sessions %",
		"engaged_sessions_percent",
		"events_ext.engaged_sessions_percent",
		"ENGAGED SESSIONS PERCENT",
	} {
		out := chart(t, sessionsSet(), ChartOptions{Value: spelling})
		if !headerIs(out, "Country", "Engaged Sessions %") {
			t.Errorf("%q did not select the column:\n%s", spelling, out)
		}
	}
	if _, ok := matchColumn(sessionsSet().Columns, "session"); ok {
		t.Error("a near miss should not match")
	}
}

func TestChart_BarLengthsScaleToMax(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "r", Label: "Region", IsDimension: true}, col("v", "Revenue", false, "")},
		Rows:    [][]any{{"east", int64(1000)}, {"west", int64(500)}, {"north", int64(0)}},
	}
	lines := strings.Split(strings.TrimRight(chart(t, set, ChartOptions{Style: StyleBlock}), "\n"), "\n")
	full := strings.Count(lines[1], "█")
	half := strings.Count(lines[2], "█")
	if full == 0 || half == 0 {
		t.Fatalf("expected drawn bars:\n%s", strings.Join(lines, "\n"))
	}
	if got, want := half, full/2; got < want-1 || got > want+1 {
		t.Errorf("half-value bar is %d blocks, expected about %d", got, want)
	}
	if strings.ContainsAny(lines[3], "█▏▎▍▌▋▊▉") {
		t.Errorf("zero row should have no bar: %q", lines[3])
	}
}

// The default style leaves a hairline between rows; block fills the cell and
// gains partial end cells; line is plain rules. Whatever the glyph, a tiny
// value still shows as at least one cell.
// The fill style carries the value inside the bar, so there is no value
// column; a bar too short for its number shows the number after it.
func TestChart_FillStyle(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "r", Label: "Region", IsDimension: true}, col("v", "Sessions", false, "NUMBER_0")},
		Rows:    [][]any{{"east", int64(12526)}, {"west", int64(1)}, {"north", nil}},
	}
	out := chart(t, set, ChartOptions{Style: StyleFill, Width: 60})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[1], "12,526") || strings.ContainsAny(out, "▇█━") {
		t.Errorf("expected the value inside a painted bar, no glyphs:\n%s", out)
	}
	// The big bar holds its value; the tiny one can't and shows it after.
	if strings.Index(lines[1], "12,526") > strings.Index(lines[2], "1") {
		t.Errorf("a value inside a bar starts before one shown after a one-cell bar:\n%s", out)
	}
	if !strings.Contains(lines[3], "-") {
		t.Errorf("null row should show a dash:\n%s", out)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 60 {
			t.Errorf("line is %d cells: %q", got, line)
		}
	}
}

func TestChart_Styles(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "r", Label: "Region", IsDimension: true}, col("v", "Revenue", false, "")},
		Rows:    [][]any{{"east", int64(1000)}, {"west", int64(1)}},
	}
	for style, glyph := range map[string]string{"": "▇", StyleBar: "▇", StyleBlock: "█", StyleLine: "━"} {
		out := chart(t, set, ChartOptions{Style: style})
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if !strings.Contains(lines[1], glyph) {
			t.Errorf("style %q: expected %q bars:\n%s", style, glyph, out)
		}
		if style != StyleBlock && strings.ContainsAny(out, "▏▎▍▌▋▊▉") {
			t.Errorf("style %q should round to whole cells:\n%s", style, out)
		}
		if got := strings.Count(lines[2], glyph) + strings.Count(lines[2], "▏"); got < 1 {
			t.Errorf("style %q: a tiny value should still draw one cell:\n%s", style, out)
		}
	}
}

func TestChart_NullValueRendersAsDash(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "a", Label: "A", IsDimension: true}, col("b", "B", false, "")},
		Rows:    [][]any{{"x", int64(5)}, {"y", nil}},
	}
	out := chart(t, set, ChartOptions{})
	if !strings.Contains(out, "y") || !strings.Contains(out, " - ") {
		t.Errorf("expected a dash for the null row:\n%s", out)
	}
}

func TestChart_MixedSignsDrawAnAxis(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "m", Label: "Month", IsDimension: true}, col("d", "Change", false, "")},
		Rows:    [][]any{{"jan", 400.0}, {"feb", -200.0}},
	}
	if out := chart(t, set, ChartOptions{}); strings.Count(out, "│") != 2 {
		t.Errorf("expected an axis on each bar:\n%s", out)
	}
}

// Rows have to fit the terminal: the label column yields to the bar rather
// than pushing the line past the width and wrapping.
func TestChart_RowsFitTheWidth(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "c", Label: "Category", IsDimension: true}, col("t", "Total", false, "NUMBER_2")},
		Rows:    [][]any{{"Fashion Hoodies & Sweatshirts Extra Long", 1602513.81}, {"Accessories", 955617.3}},
	}
	for _, width := range []int{40, 50, 80, 160} {
		out := chart(t, set, ChartOptions{Width: width})
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line is %d cells: %q", width, got, line)
			}
		}
	}
}

func TestChart_WideRuneLabelsFitTheWidth(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "n", Label: "名前", IsDimension: true}, col("v", "値", false, "")},
		Rows:    [][]any{{"東京都渋谷区神宮前一丁目二番三号", int64(100)}, {"大阪", int64(50)}},
	}
	out := chart(t, set, ChartOptions{Width: 60})
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := lipgloss.Width(line); got > 60 {
			t.Errorf("line is %d cells: %q", got, line)
		}
	}
}

func TestChart_RowCapReportsTheRemainder(t *testing.T) {
	set := &result.Set{Columns: []result.Column{{Name: "a", Label: "A", IsDimension: true}, col("b", "B", false, "")}}
	for range 10 {
		set.Rows = append(set.Rows, []any{"x", int64(1)})
	}
	if out := chart(t, set, ChartOptions{MaxRows: 4}); !strings.Contains(out, "… and 6 more rows") {
		t.Errorf("expected a remainder note:\n%s", out)
	}
}

// Two dimensions and no measure: the first numeric column stands in, and
// only a result with nothing numeric is refused.
func TestChart_NoMeasure(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{
			{Name: "a", Label: "A", IsDimension: true},
			{Name: "year", Label: "Year", IsDimension: true, DataType: "NUMBER"},
		},
		Rows: [][]any{{"x", int64(2026)}},
	}
	// With no measure, the first numeric column stands in.
	if out := chart(t, set, ChartOptions{}); !headerIs(out, "A", "Year") {
		t.Errorf("expected the numeric dimension to be plotted:\n%s", out)
	}
	set.Columns[1].DataType = "STRING"
	set.Rows = [][]any{{"x", "twenty"}}
	if got := chartErr(t, set, ChartOptions{}); !strings.Contains(got, "nothing numeric") {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestChart_Errors(t *testing.T) {
	set := sessionsSet()
	tests := []struct {
		name string
		opts ChartOptions
		want string
	}{
		{"unknown value column", ChartOptions{Value: "nope"}, "is not a column"},
		{"non-numeric value column", ChartOptions{Value: "country"}, "holds no numbers"},
		{"unknown label column", ChartOptions{Label: "nope"}, "is not a column"},
		{"unknown kind", ChartOptions{Kind: "pie"}, "unknown chart kind"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chartErr(t, set, tc.opts); !strings.Contains(got, tc.want) {
				t.Errorf("error %q does not mention %q", got, tc.want)
			}
		})
	}
}

func TestChart_EmptySet(t *testing.T) {
	set := &result.Set{Columns: sessionsSet().Columns}
	if out := chart(t, set, ChartOptions{}); !strings.Contains(out, "No results.") {
		t.Errorf("expected the empty-result line, got %q", out)
	}
}

func TestResultTable(t *testing.T) {
	var buf bytes.Buffer
	ResultTable(&buf, sessionsSet())
	out := buf.String()
	for _, want := range []string{"Country", "Sessions", "Engaged Sessions %", "12,526", "39.9%", "44.9%"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	// Query order, not alphabetical: Country before Engaged before Sessions
	// would be alphabetical; the model's order has Sessions second.
	if strings.Index(out, "Sessions") > strings.Index(out, "Engaged") {
		t.Errorf("columns should keep query order:\n%s", out)
	}
}
