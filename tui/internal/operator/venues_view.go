package operator

import (
	"fmt"
	"strings"
)

// venuesView: one line per venue (id, kind, chain, status, capabilities)
// with the selected venue's positions table below.
func (m *Model) venuesView(w, h int) string {
	cw := w - 4
	var lines []string
	lines = append(lines, dimStyle.Render(fit(padR("VENUE", 14)+" "+padR("KIND", 12)+" "+padR("CHAIN", 12)+" "+padR("STATUS", 13)+" CAPABILITIES", cw)))
	for i, v := range m.venues {
		caps := strings.Join(v.Capabilities, ",")
		if caps == "" {
			caps = "—"
		}
		if i == m.venueCursor {
			lines = append(lines, selStyle.Render(fit(padR(v.ID, 14)+" "+padR(orDash(v.Kind), 12)+" "+padR(orDash(v.Chain), 12)+" "+padR(orDash(v.Status), 13)+" "+caps, cw)))
			continue
		}
		lines = append(lines, truncTail(brightStyle.Render(padR(v.ID, 14))+" "+textStyle.Render(padR(orDash(v.Kind), 12))+" "+
			textStyle.Render(padR(orDash(v.Chain), 12))+" "+venueStatusStyle(v.Status).Render(padR(orDash(v.Status), 13))+" "+dimStyle.Render(caps), cw))
	}
	if len(m.venues) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("no venues reported"))
	}
	lines = append(lines, "")

	if m.venueCursor < len(m.venues) {
		v := m.venues[m.venueCursor]
		lines = append(lines, titleStyle.Render("POSITIONS · "+v.ID)+dimStyle.Render(fmt.Sprintf("  %d open", len(v.Positions))))
		lines = append(lines, dimStyle.Render(padR("MARKET", 10)+" "+padR("SIDE", 5)+" "+padL("SIZE USD", 12)+" "+padL("ENTRY", 12)+" "+padL("MARK", 12)+" "+padL("uPNL", 10)))
		total := 0.0
		for _, p := range v.Positions {
			side, ss := "LONG", greenStyle
			if p.SizeUSD < 0 {
				side, ss = "SHORT", redStyle
			}
			abs := p.SizeUSD
			if abs < 0 {
				abs = -abs
			}
			total += p.UPnLUSD
			lines = append(lines, brightStyle.Render(padR(p.Market, 10))+" "+ss.Bold(true).Render(padR(side, 5))+" "+
				textStyle.Render(padL(fnum(abs, 2), 12))+" "+textStyle.Render(padL(fnum(p.Entry, priceDecimals(p.Entry)), 12))+" "+
				textStyle.Render(padL(fnum(p.Mark, priceDecimals(p.Mark)), 12))+" "+signed(fmt.Sprintf("%+.2f", p.UPnLUSD), p.UPnLUSD, 10))
		}
		if len(v.Positions) == 0 {
			lines = append(lines, dimStyle.Italic(true).Render("flat — no open positions"))
		} else {
			lines = append(lines, dimStyle.Render(padR("total uPnL", 10))+" "+strings.Repeat(" ", 5+1+12+1+12+1+12+1)+signed(fmt.Sprintf("%+.2f", total), total, 10))
		}
	}
	return box("VENUES", fmt.Sprintf("%d", len(m.venues)), lines, w, h)
}

func priceDecimals(v float64) int {
	if v < 0 {
		v = -v
	}
	switch {
	case v < 1:
		return 4
	case v < 100:
		return 2
	case v < 10000:
		return 1
	}
	return 0
}
