package operator

import (
	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/theme"
)

// Short aliases onto the shared theme — one palette for both programs.
var (
	logoStyle   = theme.LogoStyle
	textStyle   = theme.TextStyle
	brightStyle = theme.BrightStyle
	dimStyle    = theme.DimStyle
	titleStyle  = theme.TitleStyle
	greenStyle  = theme.GreenStyle
	redStyle    = theme.RedStyle
	amberStyle  = theme.AmberStyle
	purpleStyle = theme.PurpleStyle
	cyanStyle   = theme.CyanStyle
	keyStyle    = theme.KeyStyle
	selStyle    = theme.SelectedStyle

	killedBanner = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A0505")).Background(theme.Red).Bold(true)
	tabActive    = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(theme.Accent).Bold(true)
	tabIdle      = lipgloss.NewStyle().Foreground(theme.Dim)
)

func box(title, right string, lines []string, w, h int) string {
	return theme.Box(title, right, lines, w, h)
}
func spread(l, r string, w int) string         { return theme.Spread(l, r, w) }
func padR(s string, w int) string              { return theme.PadR(s, w) }
func padL(s string, w int) string              { return theme.PadL(s, w) }
func fit(s string, w int) string               { return theme.Fit(s, w) }
func fnum(v float64, dec int) string           { return theme.Fnum(v, dec) }
func truncTail(s string, w int) string         { return theme.TruncTail(s, w) }
func signed(s string, v float64, w int) string { return theme.Signed(s, v, w) }
func barStyled(r float64, w int, st lipgloss.Style) string {
	return theme.BarStyled(r, w, st)
}

// verdictStyle colours an intent verdict status by state.
func verdictStyle(status string) lipgloss.Style {
	switch status {
	case "proposed":
		return amberStyle.Bold(true)
	case "approved":
		return cyanStyle.Bold(true)
	case "executed":
		return greenStyle.Bold(true)
	case "rejected":
		return dimStyle.Bold(true)
	case "gated":
		return purpleStyle.Bold(true)
	case "failed":
		return redStyle.Bold(true)
	}
	return textStyle
}

// venueStatusStyle colours a venue status string.
func venueStatusStyle(status string) lipgloss.Style {
	switch status {
	case "connected", "ok", "ready":
		return greenStyle.Bold(true)
	case "degraded", "connecting", "reconnecting":
		return amberStyle.Bold(true)
	case "", "disconnected", "error", "down":
		return redStyle.Bold(true)
	}
	return textStyle
}

// modeStyle colours the governor mode chip.
func modeStyle(mode string) lipgloss.Style {
	switch mode {
	case "auto":
		return redStyle.Bold(true)
	case "threshold":
		return amberStyle.Bold(true)
	}
	return greenStyle.Bold(true)
}
