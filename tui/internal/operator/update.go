package operator

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case connMsg:
		was := m.connected
		m.connected = msg.Connected
		if msg.Connected && !was {
			// Frames missed while disconnected are state, not a stream: a
			// fresh snapshot repairs every tab.
			return m, m.fetchSnapshot()
		}
		return m, nil

	case refreshTickMsg:
		// Offline retry, and a stale-data poll while the push link is down.
		if !m.loaded || m.offlineErr != "" || !m.connected {
			return m, tea.Batch(m.fetchSnapshot(), refreshTick())
		}
		return m, refreshTick()

	case snapshotMsg:
		m.fetching = false
		if msg.Err != nil {
			m.offlineErr = msg.Err.Error()
			return m, nil
		}
		m.offlineErr = ""
		m.loaded = true
		m.strategies = msg.Strategies
		m.venues = msg.Venues
		m.gov.setLive(msg.Governor)
		m.governor = msg.Governor
		m.mergeDecisions(msg.Decisions)
		m.clampCursors()
		if len(msg.Partial) > 0 {
			m.setNotice("partial snapshot: "+strings.Join(msg.Partial, "; "), false)
		}
		return m, nil

	case decisionMsg:
		m.upsertDecision(apiclient.DecisionRecord(msg))
		m.clampCursors()
		return m, nil

	case verdictMsg:
		for i := range m.decisions {
			if m.decisions[i].ID == msg.DecisionID {
				m.decisions[i].Verdicts = append(m.decisions[i].Verdicts, msg.Verdict)
				break
			}
		}
		return m, nil

	case configMsg:
		m.upsertStatus(apiclient.StrategyStatus(msg))
		m.clampCursors()
		return m, nil

	case governorMsg:
		m.governor = apiclient.GovernorSettings(msg)
		m.gov.setLive(m.governor)
		return m, nil

	case venueMsg:
		m.upsertVenue(apiclient.VenueStatus(msg))
		m.clampCursors()
		return m, nil

	case opResultMsg:
		return m.handleOpResult(msg)
	}
	return m, nil
}

// mergeDecisions applies a REST page (newest first) over the live list:
// records already seen keep their live verdicts if longer.
func (m *Model) mergeDecisions(page []apiclient.DecisionRecord) {
	if len(m.decisions) == 0 {
		m.decisions = page
		return
	}
	seen := make(map[string]int, len(m.decisions))
	for i, d := range m.decisions {
		seen[d.ID] = i
	}
	for _, d := range page {
		if i, ok := seen[d.ID]; ok {
			if len(d.Verdicts) >= len(m.decisions[i].Verdicts) {
				m.decisions[i] = d
			}
			continue
		}
		m.decisions = append(m.decisions, d)
	}
	sortDecisions(m.decisions)
	if len(m.decisions) > decisionsKeep {
		m.decisions = m.decisions[:decisionsKeep]
	}
}

func (m *Model) clampCursors() {
	m.stratCursor = clamp(m.stratCursor, len(m.strategies))
	m.decCursor = clamp(m.decCursor, len(m.decisions))
	m.venueCursor = clamp(m.venueCursor, len(m.venues))
	if d, ok := m.selectedDecision(); ok {
		m.intentCursor = clamp(m.intentCursor, len(d.Intents))
	} else {
		m.intentCursor = 0
	}
}

func clamp(i, n int) int {
	if n == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func (m *Model) handleOpResult(msg opResultMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.Err != nil {
		if m.form != nil && strings.HasPrefix(msg.Op, "save ") {
			m.form.setServerError(msg.Err)
		}
		m.setNotice(msg.Op+" failed: "+msg.Err.Error(), false)
		return m, nil
	}
	switch {
	case msg.Status != nil:
		m.upsertStatus(*msg.Status)
		if m.form != nil && m.form.strategyID == msg.Status.Manifest.ID && strings.HasPrefix(msg.Op, "save ") {
			m.form = nil
		}
	case msg.Decision != nil:
		i := m.upsertDecision(*msg.Decision)
		if msg.FocusDecision {
			m.focusDecision(i)
		}
	case msg.Governor != nil:
		m.governor = *msg.Governor
		m.gov.live = *msg.Governor
		m.gov.reset()
	}
	m.clampCursors()
	m.setNotice(msg.Op+" ok", true)
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Modal inputs first: they own every key until closed.
	if m.form != nil {
		return m.handleFormKey(msg)
	}
	if m.kill.open {
		cmd, confirmed, cancelled := m.kill.handleKey(msg)
		if confirmed {
			return m, m.doKill()
		}
		if cancelled {
			m.setNotice("kill cancelled", false)
		}
		return m, cmd
	}
	if m.tab == tabGovernor && m.gov.editing {
		cmd, _, _ := m.gov.handleKey(msg)
		return m, cmd
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "1", "2", "3", "4":
		m.tab = tab(key[0] - '1')
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
		return m, nil
	case "R":
		return m, m.fetchSnapshot()
	}

	switch m.tab {
	case tabStrategies:
		return m.handleStrategiesKey(key)
	case tabDecisions:
		return m.handleDecisionsKey(key)
	case tabVenues:
		return m.handleVenuesKey(key)
	case tabGovernor:
		return m.handleGovernorKey(msg)
	}
	return m, nil
}

func (m *Model) handleFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	cmd, done, save, cfg := m.form.handleKey(msg)
	if done {
		m.form = nil
		return m, nil
	}
	if save {
		return m, m.doSaveConfig(cfg)
	}
	if msg.String() == "ctrl+s" {
		m.setNotice("fix the highlighted fields before saving", false)
	}
	return m, cmd
}

func (m *Model) handleStrategiesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.stratCursor = clamp(m.stratCursor-1, len(m.strategies))
	case "down", "j":
		m.stratCursor = clamp(m.stratCursor+1, len(m.strategies))
	case "e":
		s, ok := m.selectedStrategy()
		if !ok {
			return m, nil
		}
		cfg := cloneConfig(s.Config)
		cfg.Enabled = !cfg.Enabled
		if m.governor.Killed && cfg.Enabled {
			m.setNotice("governor is killed — enable refused locally", false)
			return m, nil
		}
		return m, m.doToggle(cfg)
	case "enter":
		s, ok := m.selectedStrategy()
		if !ok {
			return m, nil
		}
		m.form = newParamForm(s)
		return m, m.form.focus()
	case "r":
		s, ok := m.selectedStrategy()
		if !ok {
			return m, nil
		}
		return m, m.doDryRun(s.Manifest.ID)
	}
	return m, nil
}

func (m *Model) handleDecisionsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.decCursor = clamp(m.decCursor-1, len(m.decisions))
		m.intentCursor, m.detailScroll = 0, 0
	case "down", "j":
		m.decCursor = clamp(m.decCursor+1, len(m.decisions))
		m.intentCursor, m.detailScroll = 0, 0
	case "left", "h", "[":
		if d, ok := m.selectedDecision(); ok {
			m.intentCursor = clamp(m.intentCursor-1, len(d.Intents))
			m.ensureIntentVisible()
		}
	case "right", "l", "]":
		if d, ok := m.selectedDecision(); ok {
			m.intentCursor = clamp(m.intentCursor+1, len(d.Intents))
			m.ensureIntentVisible()
		}
	case "pgdown", "ctrl+d":
		m.detailScroll += 5
	case "pgup", "ctrl+u":
		m.detailScroll -= 5
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case "a", "x":
		d, in, ok := m.selectedIntent()
		if !ok {
			m.setNotice("no intent selected", false)
			return m, nil
		}
		if d.DryRun {
			m.setNotice("dry-run decision: nothing to approve or reject", false)
			return m, nil
		}
		v, has := d.LatestVerdict(in.ID)
		if !has || v.Status != apiclient.VerdictProposed {
			m.setNotice(fmt.Sprintf("intent %s is %s, not proposed", shortID(in.ID), v.Status), false)
			return m, nil
		}
		if key == "a" {
			return m, m.doIntent("approve", d.ID, in.ID)
		}
		return m, m.doIntent("reject", d.ID, in.ID)
	}
	return m, nil
}

func (m *Model) handleVenuesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.venueCursor = clamp(m.venueCursor-1, len(m.venues))
	case "down", "j":
		m.venueCursor = clamp(m.venueCursor+1, len(m.venues))
	}
	return m, nil
}

func (m *Model) handleGovernorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "K" {
		return m, m.kill.show()
	}
	cmd, save, _ := m.gov.handleKey(msg)
	if save {
		return m, m.doPutGovernor(m.gov.draft)
	}
	return m, cmd
}

// ---- commands: every daemon write, off the render loop ----

func (m *Model) startOp(label string) bool {
	if m.api == nil {
		m.setNotice("no daemon connection", false)
		return false
	}
	if m.busy != "" {
		m.setNotice("busy: "+m.busy, false)
		return false
	}
	m.busy = label
	return true
}

func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestTimeout)
}

func (m *Model) doToggle(cfg apiclient.StrategyConfig) tea.Cmd {
	verb := "disable"
	if cfg.Enabled {
		verb = "enable"
	}
	op := verb + " " + cfg.ID
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		s, err := api.PutStrategyConfig(ctx, cfg)
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		return opResultMsg{Op: op, Status: &s}
	}
}

func (m *Model) doSaveConfig(cfg apiclient.StrategyConfig) tea.Cmd {
	op := "save " + cfg.ID
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		s, err := api.PutStrategyConfig(ctx, cfg)
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		return opResultMsg{Op: op, Status: &s}
	}
}

func (m *Model) doDryRun(id string) tea.Cmd {
	op := "dry-run " + id
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		d, err := api.RunStrategy(ctx, id, true)
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		d.DryRun = true // the daemon sets it; belt and braces for the badge
		return opResultMsg{Op: op, Decision: &d, FocusDecision: true}
	}
}

func (m *Model) doIntent(verb, decisionID, intentID string) tea.Cmd {
	op := verb + " " + shortID(intentID)
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		var (
			d   apiclient.DecisionRecord
			err error
		)
		if verb == "approve" {
			d, err = api.ApproveIntent(ctx, decisionID, intentID)
		} else {
			d, err = api.RejectIntent(ctx, decisionID, intentID)
		}
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		return opResultMsg{Op: op, Decision: &d}
	}
}

func (m *Model) doPutGovernor(g apiclient.GovernorSettings) tea.Cmd {
	op := "governor save"
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		out, err := api.PutGovernor(ctx, g)
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		return opResultMsg{Op: op, Governor: &out}
	}
}

func (m *Model) doKill() tea.Cmd {
	op := "KILL"
	if !m.startOp(op) {
		return nil
	}
	api := m.api
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		out, err := api.Kill(ctx)
		if err != nil {
			return opResultMsg{Op: op, Err: err}
		}
		return opResultMsg{Op: op, Governor: &out}
	}
}

// shortID trims an opaque id for footer/notice use.
func shortID(id string) string {
	if len(id) > 10 {
		return id[:10] + "…"
	}
	return id
}
