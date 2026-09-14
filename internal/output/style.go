package output

import "github.com/charmbracelet/lipgloss"

// Brand palette. Orange, not red, for negatives: traffic-light red is website-only.
const (
	omniPink = lipgloss.Color("#FF5FA2")
	deepPink = lipgloss.Color("#4D122C")
	orange   = lipgloss.Color("#FF7B3A")
	darkGray = lipgloss.Color("#818181")
	midGray  = lipgloss.Color("#BABABA")
)

var (
	styleDim     = lipgloss.NewStyle().Foreground(darkGray)
	styleBar     = lipgloss.NewStyle().Foreground(omniPink)
	styleNeg     = lipgloss.NewStyle().Foreground(orange)
	styleFill    = lipgloss.NewStyle().Background(omniPink).Foreground(deepPink)
	styleFillNeg = lipgloss.NewStyle().Background(orange).Foreground(deepPink)
	styleBorder  = lipgloss.NewStyle().Foreground(darkGray)
	styleHeader  = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(omniPink)
	styleCell    = lipgloss.NewStyle().Padding(0, 1)
	styleNum     = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
	styleMuted   = lipgloss.NewStyle().Padding(0, 1).Foreground(midGray)
)

// colorEnabled reports whether the active lipgloss profile emits color at all;
// piped output and TERM=dumb degrade to plain text, styles and all.
func colorEnabled() bool {
	return styleFill.Render(" ") != " "
}
