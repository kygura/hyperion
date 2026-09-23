// Package cockpit is the four-panel operator cockpit: the pitch mock's
// layout (pitch/mock-tui) rendered from real daemon data over the
// apiclient cache and WS bridge. Design:
// docs/superpowers/specs/2026-07-06-cockpit-tui-design.md
package cockpit

import (
	"fmt"

	"github.com/hyperagent/tui/internal/theme"
)

// The palette and layout primitives live in internal/theme so the operator
// console and this cockpit share one theme; the unexported names below are
// the cockpit's historical spellings, kept so its views read unchanged.
var (
	cAccent = theme.Accent
	cText   = theme.Text
	cBright = theme.Bright
	cDim    = theme.Dim
	cBorder = theme.Border
	cGreen  = theme.Green
	cRed    = theme.Red
	cAmber  = theme.Amber
	cPurple = theme.Purple
	cCyan   = theme.Cyan

	logoStyle   = theme.LogoStyle
	textStyle   = theme.TextStyle
	brightStyle = theme.BrightStyle
	dimStyle    = theme.DimStyle
	borderStyle = theme.BorderStyle
	titleStyle  = theme.TitleStyle
	phaseStyle  = theme.PhaseStyle
	greenStyle  = theme.GreenStyle
	redStyle    = theme.RedStyle
	amberStyle  = theme.AmberStyle
	keyStyle    = theme.KeyStyle

	tagStyles = theme.TagStyles
)

// box draws a rounded border with an embedded title, exactly h rows and
// w columns.
func box(title, rightTitle string, lines []string, w, h int) string {
	return theme.Box(title, rightTitle, lines, w, h)
}

// spread left-aligns l and right-aligns r within width w.
func spread(l, r string, w int) string { return theme.Spread(l, r, w) }

func padR(s string, w int) string { return theme.PadR(s, w) }

func padL(s string, w int) string { return theme.PadL(s, w) }

// signed pads a numeric string to w then colors it green/red by sign.
func signed(s string, v float64, w int) string { return theme.Signed(s, v, w) }

// cvdStr abbreviates a cumulative-volume-delta value with a K/M/B suffix
// scaled to its own magnitude, rather than a single fixed divisor — CVD is
// in base-asset units and ranges from single digits (BTC) to millions
// (DOGE) across the visualized watchlist, so no one fixed scale reads well
// for every coin.
func cvdStr(v float64) string {
	av := v
	if av < 0 {
		av = -av
	}
	switch {
	case av >= 1e9:
		return fmt.Sprintf("%+.1fB", v/1e9)
	case av >= 1e6:
		return fmt.Sprintf("%+.1fM", v/1e6)
	case av >= 1e3:
		return fmt.Sprintf("%+.1fK", v/1e3)
	default:
		return fmt.Sprintf("%+.0f", v)
	}
}

// fnum formats with thousands separators.
func fnum(v float64, dec int) string { return theme.Fnum(v, dec) }

func priceDec(v float64) int { return theme.PriceDec(v) }

// bar renders a filled utilization bar of exactly w cells, ratio clamped
// to [0, 1].
func bar(ratio float64, w int) string { return theme.Bar(ratio, w) }

// truncTail truncates s to at most w display cells with a "…" tail.
func truncTail(s string, w int) string { return theme.TruncTail(s, w) }
