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

// DefaultChartRows is the default --chart-rows.
const DefaultChartRows = 50

const (
	defaultChartWidth = 80
	maxLabelWidth     = 28
	minLabelWidth     = 6
	minBarWidth       = 12
)

var eighths = [...]string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

type chartRow struct {
	label   string
	text    string // formatted value
	value   float64
	present bool
}

func renderBars(w io.Writer, labelHeader, valueHeader string, items []chartRow, omitted int, opts ChartOptions) {

	width := opts.Width
	if width <= 0 {
		width = defaultChartWidth
	}

	st, ok := styles[opts.Style]
	if !ok {
		st = styles[StyleBar]
	}

	valueW := lipgloss.Width(valueHeader)
	for i := range items {
		if !items[i].present {
			items[i].text = "-"
		}
		valueW = max(valueW, lipgloss.Width(items[i].text))
	}
	if st.inside {
		valueW = 0
	}

	// Labels yield to the bar so a row never wraps; measured in cells, not runes.
	budget := min(maxLabelWidth, width-valueW-2-minBarWidth)
	budget = max(budget, minLabelWidth)

	labelHeader = truncateCells(labelHeader, budget)
	labelW := lipgloss.Width(labelHeader)
	for i := range items {
		items[i].label = truncateCells(items[i].label, budget)
		labelW = max(labelW, lipgloss.Width(items[i].label))
	}

	gutters := 2
	if st.inside {
		gutters = 1
	}
	barW := max(width-labelW-valueW-gutters, minBarWidth)

	lo, hi := 0.0, 0.0
	for _, it := range items {
		if !it.present || math.IsNaN(it.value) || math.IsInf(it.value, 0) {
			continue
		}
		lo = math.Min(lo, it.value)
		hi = math.Max(hi, it.value)
	}

	label := lipgloss.NewStyle().Width(labelW)
	value := lipgloss.NewStyle().Width(valueW).Align(lipgloss.Right)

	if st.inside {
		fmt.Fprintf(w, "%s %s\n", styleDim.Render(label.Render(labelHeader)), styleDim.Render(valueHeader))
		for _, it := range items {
			fmt.Fprintf(w, "%s %s\n", label.Render(it.label), filledBar(it, lo, hi, barW))
		}
	} else {
		fmt.Fprintf(w, "%s %s\n", styleDim.Render(label.Render(labelHeader)), styleDim.Render(value.Render(valueHeader)))
		for _, it := range items {
			var drawn string
			switch {
			case !it.present:
			case lo < 0:
				drawn = twoSidedBar(it.value, lo, hi, barW, st.fill, st.tips)
			default:
				drawn = styleBar.Render(blocks(it.value, hi, barW, st.fill, st.tips))
			}
			fmt.Fprintf(w, "%s %s %s\n", label.Render(it.label), value.Render(it.text), drawn)
		}
	}
	if omitted > 0 {
		fmt.Fprintln(w, styleDim.Render(fmt.Sprintf("… and %d more row%s", omitted, plural(omitted))))
	}
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
	fmt.Fprintf(w, "%s %s\n", styleDim.Render("Open in Omni:"), url)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
