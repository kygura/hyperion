package operator

import (
	"fmt"
	"strings"

	"github.com/hyperagent/tui/internal/apiclient"
)

// governorView: the mode selector, the three limits and the kill switch.
func (m *Model) governorView(w, h int) string {
	cw := w - 4
	g := m.gov
	labelW := 18
	var lines []string

	if m.governor.Killed {
		lines = append(lines, killedBanner.Render(fit("  ■ KILLED — kill switch engaged. Every strategy is disabled and open proposals were rejected.  ", cw)), "")
	} else {
		lines = append(lines, greenStyle.Render("● ")+textStyle.Render("kill switch armed")+dimStyle.Render(" — press K to engage (typed confirmation)"), "")
	}

	if m.kill.open {
		lines = append(lines, m.kill.view(cw)...)
		return box("GOVERNOR", "kill switch", lines, w, h)
	}

	for row := 0; row < govRowCount; row++ {
		cursor := "  "
		if row == g.cursor {
			cursor = keyStyle.Render("▸ ")
		}
		label := fit(govRowLabels[row], labelW)
		if row == g.cursor {
			label = brightStyle.Render(label)
		} else {
			label = textStyle.Render(label)
		}
		var val string
		switch {
		case row == govRowMode:
			var opts []string
			for _, mode := range apiclient.GovernorModes {
				if mode == g.draft.Mode {
					opts = append(opts, selStyle.Render(" "+mode+" "))
				} else {
					opts = append(opts, dimStyle.Render(" "+mode+" "))
				}
			}
			val = strings.Join(opts, " ")
		case g.editing && row == g.cursor:
			g.input.SetWidth(minInt(cw-labelW-6, 24))
			val = g.input.View()
		default:
			val = brightStyle.Render(g.rowValue(row))
		}
		live := ""
		if g.dirty && g.rowValue(row) != liveValue(g.live, row) {
			live = amberStyle.Render("  (live: " + liveValue(g.live, row) + ")")
		}
		lines = append(lines, truncTail(cursor+label+"  "+val+live, cw))
		if row == govRowMode {
			lines = append(lines, strings.Repeat(" ", labelW+4)+dimStyle.Render(modeHelp[g.draft.Mode]))
		}
	}
	lines = append(lines, "")
	switch {
	case g.err != "":
		lines = append(lines, redStyle.Render("✗ "+g.err))
	case g.dirty:
		lines = append(lines, amberStyle.Render("● unsaved changes")+dimStyle.Render(" — ctrl+s to PUT /api/strategy/governor, esc to discard"))
	default:
		lines = append(lines, dimStyle.Render("in sync with daemon"))
	}
	lines = append(lines, "")
	for _, l := range wrap(fmt.Sprintf("threshold mode auto-executes intents with confidence ≥ %.2f; manual proposes everything; auto executes all (risk-gated).", g.draft.MinConfidence), cw) {
		lines = append(lines, dimStyle.Render(l))
	}

	return box("GOVERNOR", "human-agency layer", lines, w, h)
}

func liveValue(g apiclient.GovernorSettings, row int) string {
	return governorForm{draft: g}.rowValue(row)
}
