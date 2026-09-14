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
}

// barGlyph (▇) leaves a hairline between rows so they don't fuse on tightly
// leaded terminals.
const barGlyph = "▇"

const DefaultChartRows = 50

const (
	defaultChartWidth = 80
	maxLabelWidth     = 28
	minLabelWidth     = 6
	minBarWidth       = 12
)

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
		valueW[c] = max(textW[c], lipgloss.Width(col.header))
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
		return minCellBarWidth
	}
	avail := func() int {
		used := len(labelW) - 1 + 1 + 2*(n-1) // label gaps, the gap after labels, column gaps
		for _, lw := range labelW {
			used += lw
		}
		for c := range n {
			used += valueW[c] + 1
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
		for c := 0; c < n; c++ {
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
		b.WriteString(lipgloss.NewStyle().Width(valueW[c]).Align(lipgloss.Right).Render(g.cols[c].header))
		b.WriteString(strings.Repeat(" ", 1+barW))
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
			case !it.present:
			case sc.lo < 0:
				drawn = twoSidedBar(it.value, sc.lo, sc.hi, barW)
			default:
				drawn = styleBar.Render(blocks(it.value, sc.hi, barW))
			}
			line.WriteString(lipgloss.NewStyle().Width(valueW[c]).Align(lipgloss.Right).Render(it.text) + " ")
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

// blocks renders v/scale of width; a non-zero value is always at least one cell.
func blocks(v, scale float64, width int) string {
	if scale <= 0 || v <= 0 || width <= 0 {
		return ""
	}
	n := int(math.Round((v / scale) * float64(width)))
	return strings.Repeat(barGlyph, min(max(n, 1), width))
}

// twoSidedBar draws around a zero axis.
func twoSidedBar(v, lo, hi float64, width int) string {
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
		b := blocks(-v, -lo, left)
		return strings.Repeat(" ", left-lipgloss.Width(b)) + styleNeg.Render(b) + styleDim.Render("│")
	}
	return strings.Repeat(" ", left) + styleDim.Render("│") + styleBar.Render(blocks(v, hi, right))
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
