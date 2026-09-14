package output

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/exploreomni/omni-cli/internal/result"
	"github.com/muesli/termenv"
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
	hEnd := strings.Index(header, "Sessions") + len("Sessions")
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

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// withColor turns color on for a test: the fill style paints a background
// rather than drawing a glyph, so with the profile a test process actually
// gets (no TTY) there would be nothing to see.
func withColor(t *testing.T) func() {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(prev) }
}

// Without color the fill style has no glyph of its own, so it falls back to
// solid blocks rather than printing rows of blank space down a pipe.
func TestChart_FillWithoutColorFallsBackToBlocks(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "r", Label: "Region", IsDimension: true}, col("v", "Sessions", false, "NUMBER_0")},
		Rows:    [][]any{{"east", int64(12526)}, {"west", int64(1)}},
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	out := chart(t, set, ChartOptions{Style: StyleFill, Width: 60})
	if !strings.Contains(out, "█") {
		t.Errorf("expected visible bars without color:\n%q", out)
	}
	if !strings.Contains(out, "12,526") {
		t.Errorf("expected the value to still show:\n%q", out)
	}
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

// The model says which columns are dimensions and which measures; the chart
// doesn't guess. Every measure gets bars, in query order.
func TestChart_ColumnsFromModel(t *testing.T) {
	out := chart(t, sessionsSet(), ChartOptions{Width: 80})
	if !headerIs(out, "Country", "Engaged Sessions %") {
		t.Fatalf("expected the dimension and every measure, got:\n%s", out)
	}
	header, _, _ := strings.Cut(out, "\n")
	if i := strings.Index(header, "Sessions"); i < 0 || i > strings.Index(header, "Engaged") {
		t.Errorf("measures should keep query order:\n%s", out)
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
	// A bare field name finds a column whose label reads differently.
	if i, ok := matchColumn(pipelineSet().Columns, "count"); !ok || i != 3 {
		t.Errorf(`"count" should find deals.count ("Deals Count"), got %d %v`, i, ok)
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
	defer withColor(t)()
	out := chart(t, set, ChartOptions{Style: StyleFill, Width: 60})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[1], "12,526") || strings.ContainsAny(out, "▇█━") {
		t.Errorf("expected the value inside a painted bar, no glyphs:\n%s", out)
	}
	// The big bar holds its value; the tiny one can't and shows it after.
	// Compared on plain text: the paint's escape codes aren't cells.
	if strings.Index(stripANSI(lines[1]), "12,526") > strings.Index(stripANSI(lines[2]), "1") {
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
	if !strings.Contains(out, "\ny -\n") {
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
		// Bars labelled by their own values say nothing.
		{"label is the value column", ChartOptions{Value: "sessions", Label: "sessions"}, "is the column being plotted"},
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

// Region × stage with two measures, as the stream decodes it.
func pipelineSet() *result.Set {
	return &result.Set{
		Columns: []result.Column{
			{Name: "deals.region", Label: "Region", IsDimension: true, DataType: "STRING"},
			{Name: "deals.stage", Label: "Stage", IsDimension: true, DataType: "STRING"},
			col("deals.total_amount", "Total amount", false, "currency_0"),
			col("deals.count", "Deals Count", false, ""),
		},
		Rows: [][]any{
			{"AMER", "Closed Lost", int64(13966500), int64(223)},
			{"AMER", "Negotiation", int64(167500), int64(3)},
			{"AMER", "Closed Won", int64(3903000), int64(56)},
			{"EMEA", "Closed Lost", int64(8482500), int64(125)},
			{"EMEA", "Closed Won", int64(1949500), int64(36)},
		},
	}
}

func pivoted() *result.Set {
	set := pipelineSet()
	set.Pivots = []string{"deals.stage"}
	return set
}

func barCells(line string) int {
	return strings.Count(line, "▇")
}

// Two dimensions label each bar together, so rows don't read as repeats of
// the first; each measure is scaled to its own maximum.
func TestChart_TwoDimensionsTwoMeasures(t *testing.T) {
	out := chart(t, pipelineSet(), ChartOptions{Width: 100})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if f := strings.Fields(lines[0]); len(f) < 2 || f[0] != "Region" || f[1] != "Stage" {
		t.Fatalf("expected both dimensions as columns:\n%s", out)
	}
	if !strings.Contains(lines[1], "AMER") || !strings.Contains(lines[1], "Closed Lost") {
		t.Errorf("row should carry both dimension values:\n%s", out)
	}
	// Closed Lost is the max of both measures: its two bars are both full.
	amount, count := strings.Split(lines[1], "$13,966,500")[1], strings.Split(lines[1], "223")[1]
	if barCells(amount)-barCells(count) != barCells(strings.Split(amount, "223")[0]) {
		t.Errorf("each measure should fill its own column at its max:\n%s", out)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 100 {
			t.Errorf("line is %d cells: %q", got, line)
		}
	}
}

// A pivot spreads the measure across its values like the Omni app's bar
// table: the pivot values head the columns, and they share one scale.
func TestChart_Pivot(t *testing.T) {
	out := chart(t, pivoted(), ChartOptions{Value: "Total amount", Width: 120, Style: StyleBlock})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, want := range []string{"Stage", "Closed Lost", "Negotiation", "Closed Won"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("group header missing %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(lines[1], "Region") || strings.Count(lines[1], "Total amount") != 1 {
		t.Errorf("a lone measure should be named once:\n%s", out)
	}
	if narrow := chart(t, pivoted(), ChartOptions{Value: "count", Width: 60}); strings.Contains(strings.Split(narrow, "\n")[0], "…") {
		t.Errorf("pivot values should widen their column, not be cut off:\n%s", narrow)
	}
	both := chart(t, pivoted(), ChartOptions{Width: 160})
	if header := strings.Split(both, "\n")[1]; strings.Count(header, "Total amount") < 2 || strings.Count(header, "Deals Count") < 2 {
		t.Errorf("with two measures each column should say which:\n%s", both)
	}
	if len(lines) != 4 {
		t.Fatalf("expected two header lines and a row per region:\n%s", out)
	}
	if !strings.HasPrefix(lines[3], "EMEA") || !strings.Contains(lines[3], " - ") {
		t.Errorf("EMEA has no Negotiation deals and should show a dash:\n%s", out)
	}
	// Shared scale: AMER Closed Lost ($13.97M) is the longest bar, and EMEA
	// Closed Lost ($8.48M) is shorter than it though it's EMEA's largest.
	full := strings.Count(strings.Split(lines[2], "$3,903,000")[0], "█")
	emea := strings.Count(strings.Split(lines[3], "$1,949,500")[0], "█")
	if emea >= full || emea == 0 {
		t.Errorf("pivot columns should share the measure's scale (full %d, emea %d):\n%s", full, emea, out)
	}
}

// A pivot too wide for the terminal drops trailing columns and says so.
func TestChart_PivotFitsTheWidth(t *testing.T) {
	out := chart(t, pivoted(), ChartOptions{Width: 50})
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := lipgloss.Width(line); got > 50 {
			t.Errorf("line is %d cells: %q", got, line)
		}
	}
	if !strings.Contains(out, "more column") {
		t.Errorf("expected a note about dropped columns:\n%s", out)
	}
}

func TestChart_PivotErrors(t *testing.T) {
	for _, tc := range []struct {
		opts ChartOptions
		want string
	}{
		{ChartOptions{Label: "stage"}, "not a row dimension"},
		{ChartOptions{Value: "region"}, "not a measure"},
	} {
		if got := chartErr(t, pivoted(), tc.opts); !strings.Contains(got, tc.want) {
			t.Errorf("%+v: error %q does not mention %q", tc.opts, got, tc.want)
		}
	}
}

func TestResultTable_Pivot(t *testing.T) {
	var buf bytes.Buffer
	ResultTable(&buf, pivoted())
	out := buf.String()
	lines := strings.Split(out, "\n")
	if strings.Count(out, "├") != 1 {
		t.Errorf("expected one rule, under the header:\n%s", out)
	}
	// Two header lines: pivot values over measure labels.
	if !strings.Contains(lines[1], "Stage") || !strings.Contains(lines[1], "Closed Lost") || !strings.Contains(lines[2], "Region") || strings.Count(lines[2], "Deals Count") != 3 {
		t.Fatalf("expected pivot values over measure labels:\n%s", out)
	}
	if strings.Count(out, "AMER") != 1 || strings.Count(out, "EMEA") != 1 {
		t.Errorf("expected one row per region:\n%s", out)
	}
}

func TestResult_StripsControlCharacters(t *testing.T) {
	set := &result.Set{
		Columns: []result.Column{{Name: "r", Label: "Region\x1b[31m", IsDimension: true}, col("v", "Total", false, "")},
		Rows:    [][]any{{"east\x1b]0;pwned\x07\nwest", int64(5)}},
	}
	var table, link bytes.Buffer
	ResultTable(&table, set)
	ChartLink(&link, "https://x/e/1\x1b[2J")
	for _, out := range []string{table.String(), chart(t, set, ChartOptions{}), link.String()} {
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("control characters reached the output:\n%q", out)
		}
	}
	// A newline in a value would break the layout; it reads as a space.
	if out := chart(t, set, ChartOptions{}); strings.Count(out, "\n") != 2 {
		t.Errorf("expected a header and one row:\n%q", out)
	}
}

// Three measures, and a column limit that drops two pivot values: six
// columns are missing, not two.
func TestChart_PivotOmittedCountsColumns(t *testing.T) {
	set := pivoted()
	set.Columns = append(set.Columns, col("deals.won", "Won", false, ""))
	for i := range set.Rows {
		set.Rows[i] = append(set.Rows[i], int64(1))
	}
	set.ColumnLimit = 1
	out := chart(t, set, ChartOptions{Width: 160})
	if !strings.Contains(out, "… and 6 more columns") {
		t.Errorf("expected six omitted columns:\n%s", out)
	}
}
