package operator

import (
	"fmt"
	"strings"

	"github.com/hyperagent/tui/internal/apiclient"
)

// strategiesView is the STRATEGIES table: id, enabled, venue, cadence,
// last run, last intent/action, error.
func (m *Model) strategiesView(w, h int) string {
	cw := w - 4
	// Fixed columns, then LAST INTENT and ERROR share what is left; the
	// intent column grows from its 14-cell minimum ("open_short ETH") up to
	// 26 so the verdict status fits on wide terminals.
	const (
		cID    = 14
		cEN    = 3
		cVenue = 10
		cCad   = 4
		cLast  = 8
		gap    = 2
	)
	fixed := cID + cEN + cVenue + cCad + cLast + 5*gap
	remaining := cw - fixed
	cIntent := clampInt(remaining/2, 14, 26)
	cErr := remaining - cIntent - gap
	showErr := cErr >= 6
	withStatus := cIntent >= 24

	cols := func(id, en, venue, cad, last, intent, errText string) string {
		row := padR(id, cID) + "  " + padR(en, cEN) + "  " + padR(venue, cVenue) + "  " + padR(cad, cCad) + "  " + padR(last, cLast) + "  " + padR(intent, cIntent)
		if showErr {
			row += "  " + padR(errText, cErr)
		}
		return fit(row, cw)
	}
	lines := []string{dimStyle.Render(cols("STRATEGY", "EN", "VENUE", "CAD", "LAST RUN", "LAST INTENT", "ERROR"))}

	rowsH := h - 3 // border + header
	start, end := window(m.stratCursor, len(m.strategies), rowsH)
	for i := start; i < end; i++ {
		s := m.strategies[i]
		intent, intentErr := m.lastIntent(s, withStatus)
		errText := s.LastError
		if errText == "" {
			errText = intentErr
		}
		id := fit(s.Manifest.ID, cID)
		venue := fit(orDash(s.Config.Venue), cVenue)
		cad := fit(orDash(s.Manifest.Cadence), cCad)
		last := fit(ago(s.LastRunAt), cLast)
		in := fit(intent, cIntent)
		e := fit(errText, cErr)
		if i == m.stratCursor {
			lines = append(lines, selStyle.Render(cols(id, enText(s.Config.Enabled), venue, cad, last, in, e)))
			continue
		}
		es := dimStyle
		if errText != "" {
			es = redStyle
		}
		row := brightStyle.Render(id) + "  " + onOff(s.Config.Enabled) + "  " + textStyle.Render(venue) + "  " +
			textStyle.Render(cad) + "  " + dimStyle.Render(last) + "  " + textStyle.Render(in)
		if showErr {
			row += "  " + es.Render(e)
		}
		lines = append(lines, truncTail(row, cw))
	}
	if len(m.strategies) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("no strategies registered on the daemon"))
	}
	right := fmt.Sprintf("%d strategies", len(m.strategies))
	if s, ok := m.selectedStrategy(); ok {
		right = s.Manifest.Name + " v" + s.Manifest.Version + " · " + right
	}
	return box("STRATEGIES", right, lines, w, h)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func enText(b bool) string {
	if b {
		return "ON"
	}
	return "off"
}

// lastIntent summarises the newest decision for a strategy: "open_short
// ETH" (first intent), "hold" when the tick produced none, or the decision
// error. Prefers the status's last_decision_id when it is in memory.
func (m *Model) lastIntent(s apiclient.StrategyStatus, withStatus bool) (intent, errText string) {
	var d *apiclient.DecisionRecord
	for i := range m.decisions {
		if s.LastDecisionID != "" && m.decisions[i].ID == s.LastDecisionID {
			d = &m.decisions[i]
			break
		}
		if d == nil && m.decisions[i].StrategyID == s.Manifest.ID && !m.decisions[i].DryRun {
			d = &m.decisions[i]
			if s.LastDecisionID == "" {
				break
			}
		}
	}
	if d == nil {
		return "—", ""
	}
	if d.Error != "" {
		return "error", d.Error
	}
	if len(d.Intents) == 0 {
		return "hold", ""
	}
	in := d.Intents[0]
	text := in.Action + " " + in.Market
	if len(d.Intents) > 1 {
		text += fmt.Sprintf(" +%d", len(d.Intents)-1)
	}
	if v, ok := d.LatestVerdict(in.ID); ok && withStatus {
		text += " · " + v.Status
	}
	return text, ""
}

// formView renders the open param editor over the STRATEGIES body.
func (m *Model) formView(w, h int) string {
	f := m.form
	cw := w - 4
	labelW := 22
	if cw < 60 {
		labelW = 16
	}
	valW := cw - labelW - 2
	if valW < 12 {
		valW = 12
	}

	var lines []string
	if s, ok := m.strategyByID(f.strategyID); ok && s.Manifest.Description != "" {
		lines = append(lines, dimStyle.Render(truncTail(s.Manifest.Description, cw)), "")
	}
	for i, ff := range f.fields {
		cursor := "  "
		if i == f.cursor {
			cursor = keyStyle.Render("▸ ")
		}
		label := fit(ff.label(), labelW)
		if i == f.cursor {
			label = brightStyle.Render(label)
		} else {
			label = textStyle.Render(label)
		}
		var val string
		switch {
		case ff.spec.Type == apiclient.ParamBool:
			if ff.on {
				val = greenStyle.Bold(true).Render("[x] true")
			} else {
				val = dimStyle.Render("[ ] false")
			}
		case ff.isEnum():
			opts := make([]string, 0, len(ff.spec.Options))
			for j, o := range ff.spec.Options {
				if j == ff.idx {
					opts = append(opts, selStyle.Render(" "+o+" "))
				} else {
					opts = append(opts, dimStyle.Render(" "+o+" "))
				}
			}
			val = truncTail(strings.Join(opts, " "), valW)
			if len(ff.spec.Options) == 0 {
				val = dimStyle.Render("(no options)")
			}
		default:
			ff.input.SetWidth(minInt(valW, 40))
			val = ff.input.View()
		}
		line := cursor + label + "  " + val
		lines = append(lines, line)

		hint := ff.bounds()
		if ff.spec.Description != "" {
			if hint != "" {
				hint += " · "
			}
			hint += ff.spec.Description
		}
		hintW := cw - labelW - 4
		if ff.err != "" {
			lines = append(lines, strings.Repeat(" ", labelW+4)+redStyle.Render("✗ "+truncTail(ff.err, hintW-2)))
		} else if hint != "" {
			lines = append(lines, strings.Repeat(" ", labelW+4)+dimStyle.Render(truncTail(hint, hintW)))
		}
	}
	if f.err != "" {
		lines = append(lines, "", redStyle.Render("✗ "+truncTail(f.err, cw-2)))
	}
	lines = append(lines, "", dimStyle.Render("ctrl+s saves (PUT /api/strategy/configs/"+f.strategyID+") · esc cancels"))

	// Keep the cursor row visible in tall forms.
	rowsH := h - 2
	if len(lines) > rowsH {
		cursorLine := 0
		for i := range f.fields {
			if i == f.cursor {
				break
			}
			cursorLine += 1
			if f.fields[i].bounds() != "" || f.fields[i].spec.Description != "" || f.fields[i].err != "" {
				cursorLine++
			}
		}
		start, end := window(cursorLine, len(lines), rowsH)
		lines = lines[start:end]
	}
	return box("PARAMS · "+f.strategyID, "esc cancel · ctrl+s save", lines, w, h)
}

func (m *Model) strategyByID(id string) (apiclient.StrategyStatus, bool) {
	for _, s := range m.strategies {
		if s.Manifest.ID == id {
			return s, true
		}
	}
	return apiclient.StrategyStatus{}, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
