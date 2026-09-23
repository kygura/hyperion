package operator

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// governorForm is the GOVERNOR tab's editable draft of GovernorSettings.
// Rows: mode (cycled), min_confidence, max_notional_usd, max_open_intents
// (typed). The draft tracks the live value until the operator changes a
// row (dirty), then holds until ctrl+s or esc.
type governorForm struct {
	live   apiclient.GovernorSettings
	draft  apiclient.GovernorSettings
	cursor int
	dirty  bool

	editing bool // a numeric row is open in input
	input   textinput.Model
	err     string
}

const (
	govRowMode = iota
	govRowMinConf
	govRowMaxNotional
	govRowMaxOpen
	govRowCount
)

var govRowLabels = [govRowCount]string{"MODE", "MIN CONFIDENCE", "MAX NOTIONAL USD", "MAX OPEN INTENTS"}

// modeHelp explains each mode inline (SPEC.md "Governor").
var modeHelp = map[string]string{
	apiclient.GovernorManual:    "every intent becomes a proposal",
	apiclient.GovernorThreshold: "confidence ≥ min auto-executes, rest proposed",
	apiclient.GovernorAuto:      "all execute, still risk-gated",
}

func newGovernorForm(g apiclient.GovernorSettings) governorForm {
	return governorForm{live: g, draft: g, input: newInput("", 24)}
}

// setLive applies a live governor update; the draft follows unless dirty.
func (g *governorForm) setLive(s apiclient.GovernorSettings) {
	g.live = s
	if !g.dirty {
		g.draft = s
	} else {
		g.draft.Killed = s.Killed
	}
}

// reset drops the draft back to live.
func (g *governorForm) reset() {
	g.draft = g.live
	g.dirty = false
	g.editing = false
	g.err = ""
}

func (g *governorForm) cycleMode(delta int) {
	modes := apiclient.GovernorModes
	i := indexOf(modes, g.draft.Mode)
	n := len(modes)
	g.draft.Mode = modes[((i+delta)%n+n)%n]
	g.dirty = true
	g.err = ""
}

// rowValue is the draft's display value for a row.
func (g governorForm) rowValue(row int) string {
	switch row {
	case govRowMode:
		return g.draft.Mode
	case govRowMinConf:
		return strconv.FormatFloat(g.draft.MinConfidence, 'f', -1, 64)
	case govRowMaxNotional:
		return strconv.FormatFloat(g.draft.MaxNotionalUSD, 'f', -1, 64)
	case govRowMaxOpen:
		return strconv.Itoa(g.draft.MaxOpenIntents)
	}
	return ""
}

// beginEdit opens the input on the cursor row (numeric rows only).
func (g *governorForm) beginEdit() tea.Cmd {
	if g.cursor == govRowMode {
		g.cycleMode(1)
		return nil
	}
	g.editing = true
	g.err = ""
	g.input.SetValue(g.rowValue(g.cursor))
	g.input.CursorEnd()
	return g.input.Focus()
}

// commitEdit parses the input into the draft row.
func (g *governorForm) commitEdit() {
	raw := strings.TrimSpace(g.input.Value())
	switch g.cursor {
	case govRowMinConf:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 1 {
			g.err = "min confidence must be 0…1"
			return
		}
		g.draft.MinConfidence = v
	case govRowMaxNotional:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			g.err = "max notional must be ≥ 0"
			return
		}
		g.draft.MaxNotionalUSD = v
	case govRowMaxOpen:
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			g.err = "max open intents must be an integer ≥ 0"
			return
		}
		g.draft.MaxOpenIntents = v
	}
	g.dirty = true
	g.editing = false
	g.input.Blur()
	g.err = ""
}

func (g *governorForm) cancelEdit() {
	g.editing = false
	g.input.Blur()
	g.err = ""
}

// handleKey routes a key on the GOVERNOR tab while not in the kill prompt.
// save is true when ctrl+s should PUT the draft.
func (g *governorForm) handleKey(msg tea.KeyPressMsg) (cmd tea.Cmd, save bool, handled bool) {
	if g.editing {
		switch msg.String() {
		case "enter":
			g.commitEdit()
		case "esc":
			g.cancelEdit()
		default:
			g.input, cmd = g.input.Update(msg)
		}
		return cmd, false, true
	}
	switch msg.String() {
	case "up", "k":
		g.cursor = ((g.cursor-1)%govRowCount + govRowCount) % govRowCount
	case "down", "j":
		g.cursor = (g.cursor + 1) % govRowCount
	case "left", "h":
		if g.cursor == govRowMode {
			g.cycleMode(-1)
		}
	case "right", "l", "space":
		if g.cursor == govRowMode {
			g.cycleMode(1)
		}
	case "enter":
		return g.beginEdit(), false, true
	case "esc":
		if g.dirty {
			g.reset()
		} else {
			return nil, false, false
		}
	case "ctrl+s":
		return nil, g.dirty, true
	default:
		return nil, false, false
	}
	return nil, false, true
}

// killPrompt is the typed confirmation for POST /api/strategy/kill: the
// operator must type KILL exactly and press enter.
type killPrompt struct {
	open  bool
	input textinput.Model
}

const killWord = "KILL"

func newKillPrompt() killPrompt {
	ti := newInput("> ", 8)
	ti.Placeholder = "type " + killWord + " and press enter"
	return killPrompt{input: ti}
}

func (k *killPrompt) show() tea.Cmd {
	k.open = true
	k.input.SetValue("")
	return k.input.Focus()
}

func (k *killPrompt) hide() {
	k.open = false
	k.input.Blur()
	k.input.SetValue("")
}

// handleKey returns confirmed=true only for enter with the exact word.
func (k *killPrompt) handleKey(msg tea.KeyPressMsg) (cmd tea.Cmd, confirmed bool, cancelled bool) {
	switch msg.String() {
	case "esc":
		k.hide()
		return nil, false, true
	case "enter":
		ok := k.input.Value() == killWord
		k.hide()
		return nil, ok, !ok
	}
	k.input, cmd = k.input.Update(msg)
	return cmd, false, false
}

func (k killPrompt) view(w int) []string {
	return []string{
		redStyle.Bold(true).Render("KILL SWITCH — disables every strategy and rejects open proposals."),
		dimStyle.Render(fmt.Sprintf("Type %s and press enter to confirm, esc to cancel.", killWord)),
		"",
		truncTail(k.input.View(), w),
	}
}
