package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

const (
	bannerInfoW   = 46 // inner width of the info panel
	bannerLabelW  = 12 // width of the label column in the info panel
	bannerArtMaxW = 53 // inner width of the art panel with the whole sky showing
	bannerArtMinW = 22 // Blobby and a cell of margin

	// A line wider than the terminal wraps and tears the box apart, so the
	// banner is drawn to fit: between these widths the sky is cropped, and a
	// terminal narrower than BannerMinWidth should go without.
	BannerMaxWidth = bannerArtMaxW + bannerInfoW + 3
	BannerMinWidth = bannerArtMinW + bannerInfoW + 3
)

// blobby is Omni's mascot at two pixels per terminal row, drawn with half
// blocks: P body, M shadow, L rim light, o face. It is wider than it is tall
// because most terminals' cells are a little taller than two squares; drawn
// square, Blobby comes out an egg.
var blobby = []string{
	"....PPPPLL....",
	"..PPPPPPPPLL..",
	".MPPPPPPPPPPL.",
	".MPPPPPPPPPPL.",
	"MPPPPPPPPPPPPL",
	"MPPPPPoPPPPoPL",
	"MPPPPPPPooPPPL",
	"MPPPPPPPPPPPPP",
	".MPPPPPPPPPPP.",
	".MMPPPPPPPPPP.",
	"..MMPPPPPPPP..",
	"....MMMPPP....",
}

// A step softer than the brand colours: omniPink with its neon magenta
// shadow glares against a black terminal.
var blobbyPalette = map[byte]lipgloss.Color{
	'P': lipgloss.Color("#FB72AB"),
	'M': lipgloss.Color("#D15289"),
	'L': lipgloss.Color("#FFB0D2"),
	'o': lipgloss.Color("#5A1F3D"),
}

// Blobby's top-left cell in the art panel. The art is one row taller than
// bannerSky, so its last row lands on the divider and Blobby sits on the line.
const blobbyRow, blobbyCol = 3, 6

var bannerSky = []string{
	"",
	"                         ░░░░░░            ░░░░░",
	"                      ░░░░░░░░░░░░      ░░░░░░░░░░",
	"                                      ░░░░░░░░░░░░░░",
	"    *",
	"                                   *",
	"                        *",
	"                                              *",
}

// bannerWeather is thunder salmon: lightning under the clouds and salmon
// coming down with it. Emoji are two cells wide, so they're placed by their
// left cell rather than typed into bannerSky, which is one rune per cell.
var bannerWeather = []struct {
	row, col int
	glyph    string
}{
	{3, 27, "⚡"},
	{4, 44, "⚡"},
	{5, 29, "🐟"},
	{6, 40, "🐟"},
}

var styleBannerTitle = lipgloss.NewStyle().Bold(true).Foreground(omniPink)

// BannerField is one labelled line of the banner's info panel.
type BannerField struct {
	Label, Value string
}

// BannerInfo is what the welcome banner reports beside Blobby. The info panel
// has room for four fields and the hint; fields past the fourth are dropped.
type BannerInfo struct {
	Version string
	Fields  []BannerField
	Hint    string
	Dir     string
}

// Banner writes the welcome banner for a terminal width cells wide: Blobby
// under a stormy sky on the left, the caller's fields on the right, and the
// working directory along the bottom. Every line comes out the same width,
// at most BannerMaxWidth and never less than BannerMinWidth; text that
// doesn't fit its slot is truncated.
func Banner(w io.Writer, info BannerInfo, width int) {
	artW := min(max(width, BannerMinWidth), BannerMaxWidth) - bannerInfoW - 3
	innerW := artW + 1 + bannerInfoW
	// Blobby's pixels are colour: without it the half blocks need different
	// glyphs to keep the silhouette.
	color := lipgloss.ColorProfile() != termenv.Ascii
	border := styleBorder.Render
	pad := func(s string, cells int) string {
		return s + strings.Repeat(" ", max(cells-lipgloss.Width(s), 0))
	}

	// "╭─ Omni CLI v1.2.3 ───┬": two cells of border lead in, then the title.
	title := styleBannerTitle.Render("Omni CLI") + " "
	if v := ansi.Truncate(singleLine(info.Version), artW-2-lipgloss.Width(title)-1, "…"); v != "" {
		title += styleDim.Render(v) + " "
	}
	fmt.Fprintln(w, border("╭─ ")+title+
		border(strings.Repeat("─", artW-2-lipgloss.Width(title))+"┬"+strings.Repeat("─", bannerInfoW)+"╮"))

	rows := []string{""}
	for _, f := range info.Fields[:min(len(info.Fields), 4)] {
		value := ansi.Truncate(singleLine(f.Value), bannerInfoW-2-bannerLabelW-1, "…")
		rows = append(rows, "  "+pad(singleLine(f.Label)+":", bannerLabelW)+styleDim.Render(value))
	}
	rows = append(rows, "", "  "+styleDim.Render(ansi.Truncate(singleLine(info.Hint), bannerInfoW-4, "…")))

	// Terminals disagree about how wide an emoji or a CJK path is. Borders
	// that follow such text are placed by column (CHA) rather than trusted to
	// land after it, so a disagreement costs a blank cell, not a bent box.
	for row := range bannerSky {
		side := ""
		if row < len(rows) {
			side = rows[row]
		}
		fmt.Fprintln(w, border("│")+bannerArtRow(row, artW, color)+ansi.CHA(artW+2)+border("│")+
			pad(side, bannerInfoW)+ansi.CHA(innerW+2)+border("│"))
	}
	fmt.Fprintln(w, border("├")+bannerArtRow(len(bannerSky), artW, color)+border("┴"+strings.Repeat("─", bannerInfoW)+"┤"))

	dir := truncateLeft(singleLine(info.Dir), innerW-4)
	left := (innerW - lipgloss.Width(dir)) / 2
	fmt.Fprintln(w, border("│")+strings.Repeat(" ", left)+pad(styleDim.Render(dir), innerW-left)+ansi.CHA(innerW+2)+border("│"))
	fmt.Fprintln(w, border("╰"+strings.Repeat("─", innerW)+"╯"))
}

// bannerArtRow renders one row of the art panel, artW cells wide. The row
// after the last sky row is the divider under the panel.
func bannerArtRow(row, artW int, color bool) string {
	var sky []rune
	if row < len(bannerSky) {
		sky = []rune(bannerSky[row])
	}
	var b strings.Builder
	for col := 0; col < artW; col++ {
		if cell, ok := blobbyCell(row, col, color); ok {
			b.WriteString(cell)
			continue
		}
		if glyph, ok := weatherAt(row, col); ok {
			// Cropped by the panel's edge, it leaves a blank cell.
			if col+1 < artW {
				b.WriteString(glyph)
				col++
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		switch {
		case row == len(bannerSky):
			// A one-cell gap in the line either side of Blobby.
			if col == blobbyCol-1 || col == blobbyCol+len(blobby[0]) {
				b.WriteByte(' ')
			} else {
				b.WriteString(styleBorder.Render("─"))
			}
		case col < len(sky) && sky[col] != ' ':
			b.WriteString(styleDim.Render(string(sky[col])))
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func weatherAt(row, col int) (string, bool) {
	for _, s := range bannerWeather {
		if s.row == row && s.col == col {
			return s.glyph, true
		}
	}
	return "", false
}

// blobbyCell returns the art-panel cell at (row, col) and whether it falls
// inside Blobby's bounding box. Each cell carries two pixels: the upper as the
// foreground of a half block, the lower as its background.
func blobbyCell(row, col int, color bool) (string, bool) {
	x, y := col-blobbyCol, (row-blobbyRow)*2
	if x < 0 || x >= len(blobby[0]) || y < 0 || y >= len(blobby) {
		return "", false
	}
	top, bottom := blobby[y][x], blobby[y+1][x]
	if !color {
		// The face becomes holes in the silhouette.
		upper, lower := top != '.' && top != 'o', bottom != '.' && bottom != 'o'
		switch {
		case upper && lower:
			return "█", true
		case upper:
			return "▀", true
		case lower:
			return "▄", true
		}
		return " ", true
	}
	switch {
	case top != '.' && bottom != '.':
		return lipgloss.NewStyle().Foreground(blobbyPalette[top]).Background(blobbyPalette[bottom]).Render("▀"), true
	case top != '.':
		return lipgloss.NewStyle().Foreground(blobbyPalette[top]).Render("▀"), true
	case bottom != '.':
		return lipgloss.NewStyle().Foreground(blobbyPalette[bottom]).Render("▄"), true
	}
	return " ", true
}

// truncateLeft keeps the end of s, which is the part of a path that says
// where you are.
func truncateLeft(s string, cells int) string {
	if lipgloss.Width(s) <= cells {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > cells-1 {
		r = r[1:]
	}
	return "…" + string(r)
}
