package output

import "github.com/charmbracelet/lipgloss"

// Brand palette. Orange, not red, for negatives: traffic-light red is website-only.
const (
	omniPink = lipgloss.Color("#FF5FA2")
	orange   = lipgloss.Color("#FF7B3A")
	darkGray = lipgloss.Color("#818181")
	midGray  = lipgloss.Color("#BABABA")
)

var (
	styleDim    = lipgloss.NewStyle().Foreground(darkGray)
	styleBar    = lipgloss.NewStyle().Foreground(omniPink)
	styleNeg    = lipgloss.NewStyle().Foreground(orange)
	styleBorder = lipgloss.NewStyle().Foreground(darkGray)
	styleHeader = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(omniPink)
	styleCell   = lipgloss.NewStyle().Padding(0, 1)
	styleNum    = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
	styleMuted  = lipgloss.NewStyle().Padding(0, 1).Foreground(midGray)
)
