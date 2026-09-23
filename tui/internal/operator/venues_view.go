package operator

import (
	"fmt"
	"strings"

	"github.com/hyperagent/tui/internal/apiclient"
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
		if d := venueDetail(v); d != "" {
			lines = append(lines, dimStyle.Render(truncTail(d, cw)))
		}
		if v.Error != "" {
			lines = append(lines, redStyle.Render(truncTail("✗ "+v.Error, cw)))
		}
		if d := venueDetail(v); d != "" || v.Error != "" {
			lines = append(lines, "")
		}
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
			entry := "—" // spot venues cannot know an entry for a balance
			if p.Entry != 0 {
				entry = fnum(p.Entry, priceDecimals(p.Entry))
			}
			lines = append(lines, brightStyle.Render(padR(p.Market, 10))+" "+ss.Bold(true).Render(padR(side, 5))+" "+
				textStyle.Render(padL(fnum(abs, 2), 12))+" "+textStyle.Render(padL(entry, 12))+" "+
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

// venueDetail renders the optional meta of the selected venue on one line:
// "mainnet · chain 143 · block 41,234,567 · 3.2000 MON · 0xabc… · uniswap_v3".
// Empty when the venue reports no meta (paper, hyperliquid).
func venueDetail(v apiclient.VenueStatus) string {
	if len(v.Meta) == 0 {
		return ""
	}
	var parts []string
	str := func(k string) string {
		if s, ok := v.Meta[k].(string); ok {
			return s
		}
		return ""
	}
	num := func(k string) (float64, bool) {
		f, ok := v.Meta[k].(float64)
		return f, ok
	}
	if s := str("network"); s != "" {
		parts = append(parts, s)
	}
	if f, ok := num("chain_id"); ok {
		parts = append(parts, fmt.Sprintf("chain %.0f", f))
	}
	if f, ok := num("head_block"); ok {
		parts = append(parts, "block "+fnum(f, 0))
	}
	if f, ok := num("native_balance"); ok {
		sym := str("native_symbol")
		if sym == "" {
			sym = "native"
		}
		parts = append(parts, fnum(f, 4)+" "+sym)
	}
	if s := str("address"); s != "" {
		if len(s) > 12 {
			s = s[:6] + "…" + s[len(s)-4:]
		}
		parts = append(parts, s)
	}
	if s := str("protocol"); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " · ")
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
