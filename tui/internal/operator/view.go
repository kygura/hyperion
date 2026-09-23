package operator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// View implements tea.Model. Layout: header (1 row, +1 killed banner), tab
// bar (1), body, footer (1). Everything is a plain string assembled to
// exactly height rows; the body box takes what is left.
func (m *Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}
	if m.width < minW || m.height < minH {
		msg := dimStyle.Render(fmt.Sprintf("operator console needs at least %d×%d — current %d×%d", minW, minH, m.width, m.height))
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg))
		v.AltScreen = true
		return v
	}

	rows := []string{m.headerView()}
	if m.governor.Killed {
		rows = append(rows, m.killedBannerView())
	}
	rows = append(rows, m.tabBarView())
	bodyH := m.height - len(rows) - 1
	rows = append(rows, m.bodyView(m.width, bodyH))
	rows = append(rows, m.footerView())

	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	return v
}

func (m *Model) headerView() string {
	left := logoStyle.Render(" HYPERION ") + dimStyle.Render("  operator console")

	var right []string
	if m.busy != "" {
		right = append(right, amberStyle.Render("⋯ "+m.busy))
	}
	right = append(right, dimStyle.Render("governor ")+modeStyle(m.governor.Mode).Render(strings.ToUpper(orDash(m.governor.Mode))))
	switch {
	case !m.loaded && m.offlineErr != "":
		right = append(right, redStyle.Bold(true).Render("● OFFLINE"))
	case m.connected:
		right = append(right, greenStyle.Bold(true).Render("● CONNECTED"))
	case m.loaded:
		right = append(right, amberStyle.Bold(true).Render("● POLLING"))
	default:
		right = append(right, amberStyle.Bold(true).Render("● CONNECTING"))
	}
	return spread(left, strings.Join(right, "  ")+" ", m.width)
}

func (m *Model) killedBannerView() string {
	text := " ■ KILLED — governor kill switch engaged: every strategy disabled, open proposals rejected "
	return killedBanner.Render(padR(truncTail(text, m.width), m.width))
}

func (m *Model) tabBarView() string {
	var parts []string
	for i := tab(0); i < tabCount; i++ {
		label := fmt.Sprintf(" %d %s ", i+1, tabNames[i])
		if i == tabDecisions {
			if n := m.pendingProposals(); n > 0 {
				label = fmt.Sprintf(" %d %s (%d proposed) ", i+1, tabNames[i], n)
			}
		}
		if i == m.tab {
			parts = append(parts, tabActive.Render(label))
		} else {
			parts = append(parts, tabIdle.Render(label))
		}
	}
	return truncTail(strings.Join(parts, " "), m.width)
}

func (m *Model) footerView() string {
	var keys string
	switch {
	case m.form != nil:
		keys = hints("↑↓/tab", "field", "←→/space", "cycle", "ctrl+s", "save", "esc", "cancel")
	case m.kill.open:
		keys = hints("enter", "confirm", "esc", "cancel")
	default:
		switch m.tab {
		case tabStrategies:
			keys = hints("e", "toggle", "enter", "params", "r", "dry-run", "j/k", "move", "1-4", "tab", "q", "quit")
		case tabDecisions:
			keys = hints("a", "approve", "x", "reject", "j/k", "decision", "h/l", "intent", "pgup/dn", "scroll", "q", "quit")
		case tabVenues:
			keys = hints("j/k", "venue", "R", "refresh", "1-4", "tab", "q", "quit")
		case tabGovernor:
			if m.gov.editing {
				keys = hints("enter", "commit", "esc", "cancel")
			} else {
				keys = hints("j/k", "field", "←→", "mode", "enter", "edit", "ctrl+s", "save", "K", "kill", "q", "quit")
			}
		}
	}
	right := ""
	if m.notice != "" && timeNow().Sub(m.noticeAt) < noticeTTL {
		if m.noticeOK {
			right = greenStyle.Render(m.notice)
		} else {
			right = amberStyle.Render(m.notice)
		}
	} else if m.loaded && !m.connected {
		right = amberStyle.Render("push link down · data may be stale")
	}
	if right != "" {
		right = truncTail(right, m.width/2) + " "
	}
	return spread(" "+keys, right, m.width)
}

// hints renders alternating key/description pairs.
func hints(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(dimStyle.Render("  "))
		}
		b.WriteString(keyStyle.Render(pairs[i]) + dimStyle.Render(" "+pairs[i+1]))
	}
	return b.String()
}

func (m *Model) bodyView(w, h int) string {
	if !m.loaded {
		return m.offlineView(w, h)
	}
	switch m.tab {
	case tabStrategies:
		if m.form != nil {
			return m.formView(w, h)
		}
		return m.strategiesView(w, h)
	case tabDecisions:
		return m.decisionsView(w, h)
	case tabVenues:
		return m.venuesView(w, h)
	case tabGovernor:
		return m.governorView(w, h)
	}
	return ""
}

// offlineView is the body before any snapshot has loaded: either still
// connecting or the daemon is unreachable. No spinner — the retry cadence
// is stated instead.
func (m *Model) offlineView(w, h int) string {
	cw := w - 4
	var lines []string
	if m.offlineErr == "" {
		lines = append(lines, textStyle.Render("connecting to "+orDash(m.coreURL)+" …"))
	} else {
		lines = append(lines,
			redStyle.Bold(true).Render("OFFLINE")+dimStyle.Render(" — daemon unreachable at ")+brightStyle.Render(orDash(m.coreURL)),
			"",
			amberStyle.Render(truncTail(m.offlineErr, cw)),
			"",
			dimStyle.Render(fmt.Sprintf("retrying every %s · press R to retry now · q to quit", refreshEvery)),
		)
	}
	return box("DAEMON", "", lines, w, h)
}

// ---- small shared helpers ----

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func onOff(b bool) string {
	if b {
		return greenStyle.Bold(true).Render("ON ")
	}
	return dimStyle.Render("off")
}

// ago renders a timestamp relative to now ("3m ago"), "—" for zero.
func ago(t apiclient.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := timeNow().Sub(t.Time)
	switch {
	case d < 0:
		return "in " + shortDur(-d)
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	default:
		return shortDur(d) + " ago"
	}
}

func shortDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func clock(t apiclient.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.Local().Format("15:04:05")
}

func pct(p float64) string { return fmt.Sprintf("%3.0f%%", p*100) }

// sortedKeys returns map keys in a stable order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortDecisions orders newest first (ts desc, id desc as tiebreak).
func sortDecisions(ds []apiclient.DecisionRecord) {
	sort.SliceStable(ds, func(i, j int) bool {
		if !ds[i].TS.Equal(ds[j].TS.Time) {
			return ds[i].TS.After(ds[j].TS.Time)
		}
		return ds[i].ID > ds[j].ID
	})
}

// wrap word-wraps s into lines of at most w display cells.
func wrap(s string, w int) []string {
	if w < 1 {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	for i := range out {
		out[i] = truncTail(out[i], w)
	}
	return out
}

// window returns the [start, end) slice bounds that keep cursor visible in
// a viewport of h rows over n items.
func window(cursor, n, h int) (int, int) {
	if h <= 0 || n == 0 {
		return 0, 0
	}
	start := 0
	if cursor >= h {
		start = cursor - h + 1
	}
	end := start + h
	if end > n {
		end = n
		start = end - h
		if start < 0 {
			start = 0
		}
	}
	return start, end
}
