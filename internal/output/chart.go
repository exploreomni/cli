package output

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const ChartKindBar = "bar"

// ChartOptions describes a requested chart; zero values mean the defaults.
type ChartOptions struct {
	Kind    string
	Label   string
	Value   string
	Width   int
	MaxRows int
	Style   string
}

// Bar styles. "bar" (▇) leaves a hairline between rows so they don't fuse
// on tightly leaded terminals; "block" (█) gains eighth-cell end precision;
// "fill" paints the value inside the bar, and its rows touch.
const (
	StyleBar   = "bar"
	StyleBlock = "block"
	StyleLine  = "line"
	StyleFill  = "fill"
)

var styles = map[string]struct {
	fill   string
	tips   []string // partial end cells by eighths; nil rounds to whole cells
	inside bool     // value printed inside the bar
}{
	StyleBar:   {"▇", nil, false},
	StyleBlock: {"█", eighths[:], false},
	StyleLine:  {"━", nil, false},
	StyleFill:  {" ", nil, true},
}

// ValidStyle reports whether s names a bar style.
func ValidStyle(s string) bool {
	_, ok := styles[s]
	return ok
}

const DefaultChartRows = 50

const (
	defaultChartWidth = 80
	maxLabelWidth     = 28
	minLabelWidth     = 6
	minBarWidth       = 12
)

var eighths = [...]string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Beside other bar columns a bar can be short: the value is printed next to
// it, so the bar only has to show proportion.
const minCellBarWidth = 4

type chartRow struct {
	text    string // formatted value
	value   float64
	present bool
}

// grid is a chart laid out as rows of labels followed by columns of bars.
type grid struct {
	groupLabel   string     // what pivot values are, over the label columns; "" unpivoted
	labelHeaders []string   // one per label column
	labels       [][]string // [row][label column]
	cols         []gridCol
	omittedRows  int
	omittedCols  int
}

type gridCol struct {
	group  string // the pivot value over this column; "" unpivoted
	header string // the measure's label
	scale  int    // columns with the same scale share one axis (one measure)
	items  []chartRow
}

func renderGrid(w io.Writer, g *grid, opts ChartOptions) {
	width := opts.Width
	if width <= 0 {
		width = defaultChartWidth
	}

	st, ok := styles[opts.Style]
	if !ok {
		st = styles[StyleBar]
	}
	// "fill" is a background with no glyph of its own, so without color it
	// draws rows of blank space — which is what a pipe or a dumb terminal
	// gets. Solid blocks say the same thing without needing SGR support.
	if st.inside && !colorEnabled() {
		st = styles[StyleBlock]
	}

	// Each measure is scaled on its own, across every column it fills.
	type bounds struct{ lo, hi float64 }
	scales := map[int]bounds{}
	valueW := make([]int, len(g.cols))
	textW := make([]int, len(g.cols))
	for c := range g.cols {
		col := &g.cols[c]
		b := scales[col.scale]
		for i := range col.items {
			it := &col.items[i]
			if !it.present {
				it.text = "-"
			} else if !math.IsNaN(it.value) && !math.IsInf(it.value, 0) {
				b.lo, b.hi = math.Min(b.lo, it.value), math.Max(b.hi, it.value)
			}
			textW[c] = max(textW[c], lipgloss.Width(it.text))
		}
		scales[col.scale] = b
		if !st.inside {
			valueW[c] = max(textW[c], lipgloss.Width(col.header))
		}
	}

	labelW := make([]int, len(g.labelHeaders))
	for i, h := range g.labelHeaders {
		labelW[i] = lipgloss.Width(h)
		for _, row := range g.labels {
			labelW[i] = max(labelW[i], lipgloss.Width(row[i]))
		}
		labelW[i] = min(labelW[i], maxLabelWidth)
	}

	n := len(g.cols)
	minBar := func() int {
		if n == 1 {
			return minBarWidth
		}
		m := minCellBarWidth
		if st.inside {
			// A value too long for its bar prints after it, inside the cell.
			for c := range n {
				m = max(m, 2*textW[c]+3)
			}
		}
		return m
	}
	avail := func() int {
		used := len(labelW) - 1 + 1 + 2*(n-1) // label gaps, the gap after labels, column gaps
		for _, lw := range labelW {
			used += lw
		}
		for c := range n {
			used += valueW[c]
			if !st.inside {
				used++
			}
		}
		return width - used
	}
	var barW int
	for {
		// Labels yield to the bars first, so a row never wraps; then columns go.
		for avail() < n*minBar() {
			widest := 0
			for i := range labelW {
				if labelW[i] > labelW[widest] {
					widest = i
				}
			}
			if len(labelW) > 0 && labelW[widest] > minLabelWidth {
				labelW[widest]--
				continue
			}
			if n == 1 {
				break
			}
			n--
			g.omittedCols++
		}
		barW = max(avail()/n, minBar())
		// A pivot value heading a single column widens it rather than being
		// cut off; bars stay one width so a shared scale stays comparable.
		grew := false
		for c := 0; c < n && !st.inside; c++ {
			if spansOne(g.cols, c) {
				if need := lipgloss.Width(g.cols[c].group) - 1 - barW; need > valueW[c] {
					valueW[c], grew = need, true
				}
			}
		}
		if !grew {
			break
		}
	}

	pad := func(s string, cells int) string {
		return s + strings.Repeat(" ", max(cells-lipgloss.Width(s), 0))
	}
	colW := func(c int) int {
		if st.inside {
			return barW
		}
		return valueW[c] + 1 + barW
	}
	labelArea := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, s := range cells {
			parts[i] = pad(truncateCells(s, labelW[i]), labelW[i])
		}
		return strings.Join(parts, " ")
	}
	labelAreaW := len(labelW) - 1
	for _, lw := range labelW {
		labelAreaW += lw
	}
	gap := func(c int) string {
		if c == 0 {
			return " "
		}
		return "  "
	}

	// A pivot's values head the columns they span, above the measure labels.
	if g.groupLabel != "" {
		var b strings.Builder
		b.WriteString(pad(truncateCells(g.groupLabel, labelAreaW), labelAreaW))
		for c := 0; c < n; {
			span := colW(c)
			end := c + 1
			for end < n && g.cols[end].group == g.cols[c].group {
				span += 2 + colW(end)
				end++
			}
			b.WriteString(gap(c) + pad(truncateCells(g.cols[c].group, span), span))
			c = end
		}
		fmt.Fprintln(w, styleDim.Render(strings.TrimRight(b.String(), " ")))
	}

	var b strings.Builder
	b.WriteString(labelArea(g.labelHeaders))
	for c := range n {
		b.WriteString(gap(c))
		if st.inside {
			b.WriteString(pad(truncateCells(g.cols[c].header, barW), barW))
		} else {
			b.WriteString(lipgloss.NewStyle().Width(valueW[c]).Align(lipgloss.Right).Render(g.cols[c].header))
			b.WriteString(strings.Repeat(" ", 1+barW))
		}
	}
	fmt.Fprintln(w, styleDim.Render(strings.TrimRight(b.String(), " ")))

	for r, row := range g.labels {
		var line strings.Builder
		line.WriteString(labelArea(row))
		for c := range n {
			it := g.cols[c].items[r]
			sc := scales[g.cols[c].scale]
			last := c == n-1
			line.WriteString(gap(c))
			var drawn string
			switch {
			case st.inside:
				drawn = filledBar(it, sc.lo, sc.hi, barW)
			case !it.present:
			case sc.lo < 0:
				drawn = twoSidedBar(it.value, sc.lo, sc.hi, barW, st.fill, st.tips)
			default:
				drawn = styleBar.Render(blocks(it.value, sc.hi, barW, st.fill, st.tips))
			}
			if !st.inside {
				line.WriteString(lipgloss.NewStyle().Width(valueW[c]).Align(lipgloss.Right).Render(it.text) + " ")
			}
			if !last {
				drawn = pad(drawn, barW)
			}
			line.WriteString(drawn)
		}
		fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
	}

	var notes []string
	if g.omittedRows > 0 {
		notes = append(notes, fmt.Sprintf("%d more row%s", g.omittedRows, plural(g.omittedRows)))
	}
	if g.omittedCols > 0 {
		notes = append(notes, fmt.Sprintf("%d more column%s", g.omittedCols, plural(g.omittedCols)))
	}
	if len(notes) > 0 {
		fmt.Fprintln(w, styleDim.Render("… and "+strings.Join(notes, ", ")))
	}
}

func spansOne(cols []gridCol, c int) bool {
	g := cols[c].group
	return g != "" && (c == 0 || cols[c-1].group != g) && (c == len(cols)-1 || cols[c+1].group != g)
}

// filledBar paints the bar as a background with the value inside it, or
// after it when the bar is too short.
func filledBar(it chartRow, lo, hi float64, width int) string {
	if !it.present {
		return styleDim.Render("-")
	}
	paint := func(cells int, text string, st lipgloss.Style) string {
		if cells <= 0 {
			return text
		}
		if lipgloss.Width(text)+2 <= cells {
			return st.Render(" " + text + strings.Repeat(" ", cells-lipgloss.Width(text)-1))
		}
		return st.Render(strings.Repeat(" ", cells)) + " " + text
	}
	if lo >= 0 {
		return paint(cellsFor(it.value, hi, width), it.text, styleFill)
	}
	span := hi - lo
	usable := width - 1
	if span <= 0 || usable < 2 {
		return it.text
	}
	left := min(max(int(math.Round((-lo/span)*float64(usable))), 1), usable-1)
	right := usable - left
	if it.value < 0 {
		cells := cellsFor(-it.value, -lo, left)
		bar := paint(cells, it.text, styleFillNeg)
		return strings.Repeat(" ", max(left-lipgloss.Width(bar), 0)) + bar + styleDim.Render("│")
	}
	return strings.Repeat(" ", left) + styleDim.Render("│") + paint(cellsFor(it.value, hi, right), it.text, styleFill)
}

func cellsFor(v, scale float64, width int) int {
	if scale <= 0 || v <= 0 || width <= 0 {
		return 0
	}
	return min(max(int(math.Round((v/scale)*float64(width))), 1), width)
}

// blocks renders v/scale of width; a non-zero value is always at least one cell.
func blocks(v, scale float64, width int, fill string, tips []string) string {
	if scale <= 0 || v <= 0 || width <= 0 {
		return ""
	}
	if tips == nil {
		n := int(math.Round((v / scale) * float64(width)))
		return strings.Repeat(fill, min(max(n, 1), width))
	}
	total := int(math.Round((v / scale) * float64(width) * 8))
	total = min(max(total, 1), width*8)
	return strings.Repeat(fill, total/8) + tips[total%8]
}

// twoSidedBar draws around a zero axis.
func twoSidedBar(v, lo, hi float64, width int, fill string, tips []string) string {
	span := hi - lo
	if span <= 0 {
		return ""
	}
	usable := width - 1
	if usable < 2 {
		return ""
	}
	left := int(math.Round((-lo / span) * float64(usable)))
	left = min(max(left, 1), usable-1)
	right := usable - left

	if v < 0 {
		// No right-filling partial blocks exist, so negatives round to whole cells.
		b := blocks(-v, -lo, left, fill, nil)
		return strings.Repeat(" ", left-lipgloss.Width(b)) + styleNeg.Render(b) + styleDim.Render("│")
	}
	return strings.Repeat(" ", left) + styleDim.Render("│") + styleBar.Render(blocks(v, hi, right, fill, tips))
}

func ChartLink(w io.Writer, url string) {
	fmt.Fprintf(w, "%s %s\n", styleDim.Render("Open in Omni:"), singleLine(url))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
