package operator

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// decisionsView: left list of decisions (newest first), right detail of the
// selected one. Below ~100 columns the list narrows to its essentials.
func (m *Model) decisionsView(w, h int) string {
	listW := decisionListWidth(w)
	detailW := w - listW
	return lipgloss.JoinHorizontal(lipgloss.Top, m.decisionListView(listW, h), m.decisionDetailView(detailW, h))
}

// decisionListWidth splits the tab: half the width, clamped so the narrow
// list (36 cols) and the wide one with model/latency (58) both fit.
func decisionListWidth(w int) int {
	return clampInt(w/2, 36, 58)
}

func (m *Model) decisionListView(w, h int) string {
	cw := w - 4
	wide := cw >= 51
	var lines []string
	head := padR("TIME", 8) + " " + padR("STRATEGY", 12) + " " + padR("VENUE", 7) + " " + padR("D", 1) + " " + padL("#", 2)
	if wide {
		head += " " + padR("MODEL", 10) + " " + padL("MS", 5)
	}
	lines = append(lines, dimStyle.Render(fit(head, cw)))

	rowsH := h - 3
	start, end := window(m.decCursor, len(m.decisions), rowsH)
	for i := start; i < end; i++ {
		d := m.decisions[i]
		dry := " "
		if d.DryRun {
			dry = "D"
		}
		row := padR(clock(d.TS), 8) + " " + fit(d.StrategyID, 12) + " " + fit(d.Venue, 7) + " " + dry + " " + padL(strconv.Itoa(len(d.Intents)), 2)
		if wide {
			row += " " + fit(d.Model, 10) + " " + padL(strconv.Itoa(d.LatencyMS), 5)
		}
		row = fit(row, cw)
		switch {
		case i == m.decCursor:
			row = selStyle.Render(row)
		case d.Error != "":
			row = redStyle.Render(row)
		case d.DryRun:
			row = dimStyle.Render(row)
		default:
			row = textStyle.Render(row)
		}
		lines = append(lines, row)
	}
	if len(m.decisions) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("no decisions yet"))
	}
	return box("DECISIONS", fmt.Sprintf("%d", len(m.decisions)), lines, w, h)
}

func (m *Model) decisionDetailView(w, h int) string {
	cw := w - 4
	d, ok := m.selectedDecision()
	if !ok {
		return box("DETAIL", "", []string{dimStyle.Italic(true).Render("select a decision")}, w, h)
	}
	lines := m.decisionLines(d, cw)

	rowsH := h - 2
	if m.detailScroll > len(lines)-rowsH {
		m.detailScroll = len(lines) - rowsH
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
	if m.detailScroll > 0 {
		lines = lines[m.detailScroll:]
	}
	right := shortID(d.ID)
	if d.DryRun {
		right = "DRY-RUN · " + right
	}
	return box("DETAIL", right, lines, w, h)
}

// detailRows is the detail pane's visible row count for the current size.
func (m *Model) detailRows() int {
	chrome := 3 // header, tab bar, footer
	if m.governor.Killed {
		chrome++
	}
	return m.height - chrome - 2
}

// ensureIntentVisible scrolls the detail pane so the selected intent's
// first line is on screen — on a short terminal the questions alone can
// overflow it, and a/x must act on something the operator can see.
func (m *Model) ensureIntentVisible() {
	d, ok := m.selectedDecision()
	if !ok || m.intentCursor >= len(d.Intents) {
		return
	}
	cw := m.width - (m.width * 2 / 5) - 4
	if cw < 20 {
		cw = 20
	}
	lines := m.decisionLines(d, cw)
	target := -1
	marker := keyStyle.Render("▸ ")
	for i, l := range lines {
		if strings.HasPrefix(l, marker) {
			target = i
			break
		}
	}
	if target < 0 {
		return
	}
	rows := m.detailRows()
	if rows <= 0 {
		return
	}
	if target < m.detailScroll {
		m.detailScroll = target
	} else if target >= m.detailScroll+rows {
		m.detailScroll = target - rows + 1
	}
}

// decisionLines renders the full detail body: header, each question with
// its answer visualised, then the intents with their latest verdicts.
func (m *Model) decisionLines(d apiclient.DecisionRecord, cw int) []string {
	var lines []string
	ts := "—"
	if !d.TS.IsZero() {
		ts = d.TS.Local().Format("2006-01-02 15:04:05")
	}
	title := brightStyle.Render(d.StrategyID) + dimStyle.Render(" @ ") + textStyle.Render(d.Venue) + dimStyle.Render(" · "+ts)
	if d.DryRun {
		title += "  " + amberStyle.Bold(true).Render("DRY-RUN")
	}
	lines = append(lines, truncTail(title, cw))
	meta := fmt.Sprintf("model %s · %d ms · %d→%d tok · %s", orDash(d.Model), d.LatencyMS, d.Usage.InputTokens, d.Usage.OutputTokens, orDash(d.StateDigest))
	lines = append(lines, dimStyle.Render(truncTail(meta, cw)))
	if d.Error != "" {
		lines = append(lines, redStyle.Bold(true).Render("error ")+redStyle.Render(truncTail(d.Error, cw-6)))
	}
	lines = append(lines, "")

	keys := sortedKeys(d.Questions)
	for k := range d.Answers {
		if _, has := d.Questions[k]; !has {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, questionLines(k, d.Questions[k], d.Answers[k], cw)...)
		lines = append(lines, "")
	}
	if len(keys) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("no questions asked"), "")
	}

	lines = append(lines, titleStyle.Render("INTENTS")+dimStyle.Render(fmt.Sprintf("  %d", len(d.Intents))))
	if len(d.Intents) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("none — hold"))
	}
	for i, in := range d.Intents {
		lines = append(lines, intentLines(d, in, i == m.intentCursor, cw)...)
	}
	return lines
}

// questionLines renders one question and its answer: per-option probability
// bars for choice/score (score also gets its rubric position), a single
// bar for noul, then confidence when present.
func questionLines(key string, q apiclient.Question, a apiclient.Answer, cw int) []string {
	typ := q.Type
	if typ == "" {
		typ = a.Type
	}
	head := purpleStyle.Bold(true).Render(key) + dimStyle.Render(" ["+orDash(typ)+"] ") + textStyle.Render(q.Instructions)
	lines := []string{truncTail(head, cw)}

	labelW := 14
	barW := cw - labelW - 9 // "  " + label + " " + bar + " " + pct(4)
	if barW < 6 {
		barW = 6
	}

	switch typ {
	case apiclient.QuestionChoice:
		opts := sortedKeys(q.Criteria.Options)
		if len(opts) == 0 {
			opts = sortedKeys(a.Probabilities)
		}
		for _, o := range opts {
			p := a.Probabilities[o]
			mark, st := "  ", dimStyle
			if o == a.Choice {
				mark, st = "● ", greenStyle
			}
			lines = append(lines, mark+st.Render(fit(o, labelW))+" "+barStyled(p, barW, st)+" "+st.Render(pct(p)))
		}
	case apiclient.QuestionScore:
		levels := q.Criteria.Levels
		n := len(levels)
		if n == 0 {
			n = len(a.Probabilities)
		}
		if n == 0 && len(a.Legend) > 0 {
			n = len(a.Legend)
		}
		for i := 0; i < n; i++ {
			k := strconv.Itoa(i)
			label := ""
			if i < len(levels) {
				label = levels[i]
			}
			if l := a.LegendText(k); l != "" {
				label = l
			}
			p := a.Probabilities[k]
			st := dimStyle
			if float64(i) == roundHalf(a.Score) {
				st = greenStyle
			}
			lines = append(lines, "  "+st.Render(fit(k+" "+label, labelW))+" "+barStyled(p, barW, st)+" "+st.Render(pct(p)))
		}
		lines = append(lines, "  "+dimStyle.Render(fit("score", labelW))+" "+rubric(a.Score, n, barW)+" "+brightStyle.Render(fmt.Sprintf("%.2f", a.Score)))
	case apiclient.QuestionNoul:
		lines = append(lines, "  "+dimStyle.Render(fit("noul", labelW))+" "+barStyled(a.Noul, barW, cyanStyle)+" "+brightStyle.Render(fmt.Sprintf("%.2f", a.Noul)))
	default:
		lines = append(lines, "  "+dimStyle.Render("(no answer)"))
	}
	if c, ok := a.ConfidenceValue(); ok {
		lines = append(lines, "  "+dimStyle.Render(fit("confidence", labelW))+" "+barStyled(c, barW, amberStyle)+" "+amberStyle.Render(pct(c)))
	}
	return lines
}

func roundHalf(v float64) float64 {
	if v < 0 {
		return -float64(int(-v + 0.5))
	}
	return float64(int(v + 0.5))
}

// rubric draws the score's position on its level scale: "├──┼──●──┤"
// with a tick per level and ● at the (possibly between-level) score.
func rubric(score float64, levels, w int) string {
	if w < 3 {
		return ""
	}
	cells := []rune(strings.Repeat("─", w))
	cells[0], cells[w-1] = '├', '┤'
	if levels > 1 {
		for i := 1; i < levels-1; i++ {
			pos := int(float64(i)/float64(levels-1)*float64(w-1) + 0.5)
			cells[pos] = '┼'
		}
	}
	ratio := 0.0
	if levels > 1 {
		ratio = score / float64(levels-1)
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	pos := int(ratio*float64(w-1) + 0.5)
	left := dimStyle.Render(string(cells[:pos]))
	right := dimStyle.Render(string(cells[pos+1:]))
	return left + greenStyle.Bold(true).Render("●") + right
}

// intentLines renders one intent with its latest verdict coloured by state.
func intentLines(d apiclient.DecisionRecord, in apiclient.Intent, selected bool, cw int) []string {
	cursor := "  "
	if selected {
		cursor = keyStyle.Render("▸ ")
	}
	size := ""
	if in.SizeUSD != 0 {
		size = " $" + fnum(in.SizeUSD, 0)
	}
	if in.TargetWeight != nil {
		size += fmt.Sprintf(" w=%.2f", *in.TargetWeight)
	}
	if in.PriceLimit != nil {
		size += fmt.Sprintf(" @%s", fnum(*in.PriceLimit, 2))
	}
	head := brightStyle.Render(in.Action) + " " + textStyle.Render(in.Market) + textStyle.Render(size)
	conf := dimStyle.Render(fmt.Sprintf("  conf %.0f%%", in.Confidence*100))
	status := "no verdict"
	st := dimStyle
	detail := ""
	if v, ok := d.LatestVerdict(in.ID); ok {
		status = strings.ToUpper(v.Status)
		st = verdictStyle(v.Status)
		detail = v.By
		if v.Reason != "" {
			detail += ": " + v.Reason
		}
	}
	line1 := cursor + head + "  " + st.Render(status) + conf
	if selected {
		line1 = cursor + selStyle.Render(in.Action+" "+in.Market+size+" ") + " " + st.Render(status) + conf
	}
	lines := []string{truncTail(line1, cw)}
	if detail != "" {
		lines = append(lines, "    "+dimStyle.Render(truncTail(detail, cw-4)))
	}
	if in.Reason != "" {
		lines = append(lines, "    "+textStyle.Render(truncTail(in.Reason, cw-4)))
	}
	return lines
}
