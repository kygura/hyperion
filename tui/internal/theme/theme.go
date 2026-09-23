// Package theme is the single Lipgloss palette shared by every TUI program in
// this module (the legacy cockpit and the operator console). The colours are
// the pitch mock's fixed dark palette (pitch/mock-tui/view.go); the helpers
// are the dense-monospace primitives both programs draw with. Nothing here
// knows about Bubble Tea models — it is styles and string layout only.
package theme

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	Accent = lipgloss.Color("#2DE0A7")
	Text   = lipgloss.Color("#C9D4DE")
	Bright = lipgloss.Color("#EDF3F9")
	Dim    = lipgloss.Color("#5C6B7A")
	Border = lipgloss.Color("#28323D")
	Green  = lipgloss.Color("#4ADE80")
	Red    = lipgloss.Color("#FF6B6B")
	Amber  = lipgloss.Color("#F0B35B")
	Purple = lipgloss.Color("#B48EF7")
	Cyan   = lipgloss.Color("#4FC1E9")

	LogoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(Accent).Bold(true)
	TextStyle   = lipgloss.NewStyle().Foreground(Text)
	BrightStyle = lipgloss.NewStyle().Foreground(Bright).Bold(true)
	DimStyle    = lipgloss.NewStyle().Foreground(Dim)
	BorderStyle = lipgloss.NewStyle().Foreground(Border)
	TitleStyle  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	PhaseStyle  = lipgloss.NewStyle().Foreground(Accent)
	GreenStyle  = lipgloss.NewStyle().Foreground(Green)
	RedStyle    = lipgloss.NewStyle().Foreground(Red)
	AmberStyle  = lipgloss.NewStyle().Foreground(Amber)
	PurpleStyle = lipgloss.NewStyle().Foreground(Purple)
	CyanStyle   = lipgloss.NewStyle().Foreground(Cyan)
	KeyStyle    = lipgloss.NewStyle().Foreground(Accent).Bold(true)

	// SelectedStyle is the cursor row in every table/list: inverted accent.
	SelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(Accent).Bold(true)

	TagStyles = map[string]lipgloss.Style{
		"INGEST":   lipgloss.NewStyle().Foreground(Cyan).Bold(true),
		"REASON":   lipgloss.NewStyle().Foreground(Purple).Bold(true),
		"EXECUTE":  lipgloss.NewStyle().Foreground(Accent).Bold(true),
		"FILL":     lipgloss.NewStyle().Foreground(Green).Bold(true),
		"RISK":     lipgloss.NewStyle().Foreground(Amber).Bold(true),
		"ERROR":    lipgloss.NewStyle().Foreground(Red).Bold(true),
		"OPERATOR": lipgloss.NewStyle().Foreground(Red).Bold(true),
	}
)

// Box draws a rounded border with an embedded title, exactly h rows and
// w columns. Lines beyond h-2 are dropped; lines wider than w-4 overflow
// (callers truncate).
func Box(title, rightTitle string, lines []string, w, h int) string {
	iw := w - 2 // width between the corner glyphs
	cw := iw - 2
	ch := h - 2

	t := " " + title + " "
	r := ""
	if rightTitle != "" {
		r = " " + rightTitle + " "
	}
	fill := iw - 1 - lipgloss.Width(t) - lipgloss.Width(r) - 1
	if fill < 0 {
		fill = 0
	}
	var b strings.Builder
	b.WriteString(BorderStyle.Render("╭─") + TitleStyle.Render(t) +
		BorderStyle.Render(strings.Repeat("─", fill)) + DimStyle.Render(r) + BorderStyle.Render("─╮"))

	for i := 0; i < ch; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		pad := cw - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("\n" + BorderStyle.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + BorderStyle.Render("│"))
	}

	if iw < 0 {
		iw = 0
	}
	b.WriteString("\n" + BorderStyle.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

// Spread left-aligns l and right-aligns r within width w.
func Spread(l, r string, w int) string {
	gap := w - lipgloss.Width(l) - lipgloss.Width(r)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + r
}

// PadR right-pads s with spaces to w display cells.
func PadR(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// PadL left-pads s with spaces to w display cells.
func PadL(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// Fit truncates-or-pads s to exactly w display cells.
func Fit(s string, w int) string {
	return PadR(TruncTail(s, w), w)
}

// Signed pads a numeric string to w then colours it green/red by sign.
func Signed(s string, v float64, w int) string {
	if w > 0 {
		s = PadL(s, w)
	}
	if v >= 0 {
		return GreenStyle.Render(s)
	}
	return RedStyle.Render(s)
}

// Fnum formats with thousands separators.
func Fnum(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	ip, fp := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		ip, fp = s[:i], s[i:]
	}
	neg := strings.HasPrefix(ip, "-")
	if neg {
		ip = ip[1:]
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for j, c := range ip {
		if j > 0 && (len(ip)-j)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + fp
}

// PriceDec picks a display precision for a price by magnitude.
func PriceDec(v float64) int {
	switch {
	case v < 1:
		return 4
	case v < 100:
		return 2
	case v < 10000:
		return 1
	default:
		return 0
	}
}

// Bar renders a filled utilization bar of exactly w cells, ratio clamped
// to [0, 1], in the accent colour.
func Bar(ratio float64, w int) string {
	return BarStyled(ratio, w, PhaseStyle)
}

// BarStyled is Bar with a caller-chosen fill style.
func BarStyled(ratio float64, w int, fill lipgloss.Style) string {
	if w < 1 {
		return ""
	}
	if ratio < 0 || ratio != ratio { // NaN guard
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	n := int(ratio*float64(w) + 0.5)
	if n > w {
		n = w
	}
	return fill.Render(strings.Repeat("█", n)) + DimStyle.Render(strings.Repeat("─", w-n))
}

// TruncTail truncates s to at most w display cells with a "…" tail.
func TruncTail(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
