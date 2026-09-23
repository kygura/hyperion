package operator

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

var smokeSizes = []struct{ w, h int }{{120, 40}, {80, 24}}

func rows(s string) int { return strings.Count(s, "\n") + 1 }

func TestViewSmokeStrategies(t *testing.T) {
	for _, sz := range smokeSizes {
		m := loadedModel(t, newFake(), sz.w, sz.h)
		out := m.View().Content
		for _, want := range []string{"HYPERION", "STRATEGIES", "DECISIONS", "VENUES", "GOVERNOR", "funding_skew", "paper", "5m", "MANUAL", "toggle", "dry-run", "open_short ETH"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d strategies view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
		press(t, m, "enter")
		out = m.View().Content
		for _, want := range []string{"PARAMS", "Venue", "Size per intent", "side_bias"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d form view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d form rows = %d", sz.w, sz.h, got)
		}
	}
}

func TestViewSmokeDecisions(t *testing.T) {
	for _, sz := range smokeSizes {
		m := loadedModel(t, newFake(), sz.w, sz.h)
		press(t, m, "2")
		out := m.View().Content
		for _, want := range []string{"DETAIL", "funding_skew", "jev-1.13.0", "crowding", "direction", "funding_extreme", "fade_short", "82%", "score", "1.35", "confidence", "approve", "reject"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d decisions view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
		// Selecting an intent scrolls the detail so it is on screen even
		// when the questions overflow a short terminal.
		press(t, m, "l")
		out = m.View().Content
		for _, want := range []string{"INTENTS", "open_short", "PROPOSED"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d decisions view after intent select missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
		// A decision with an error and no intents still renders.
		m.Update(decisionMsg(apiclient.DecisionRecord{ID: "err-1", StrategyID: "funding_skew", Venue: "paper", Error: "decider unavailable"}))
		press(t, m, "k")
		out = m.View().Content
		if !strings.Contains(out, "decider unavailable") || !strings.Contains(out, "hold") {
			t.Errorf("%dx%d error decision not rendered", sz.w, sz.h)
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
	}
}

func TestViewSmokeVenues(t *testing.T) {
	for _, sz := range smokeSizes {
		m := loadedModel(t, newFake(), sz.w, sz.h)
		press(t, m, "3")
		out := m.View().Content
		for _, want := range []string{"VENUES", "paper", "connected", "perps", "POSITIONS", "ETH", "SHORT", "3,100.5", "+1.60"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d venues view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
	}
}

// TestViewSmokeMonadVenue: an evm venue (spot, meta, error) renders in the
// VENUES table with its detail line, at both sizes, without row overflow.
func TestViewSmokeMonadVenue(t *testing.T) {
	for _, sz := range smokeSizes {
		f := newFake()
		f.venues = []apiclient.VenueStatus{{
			ID: "monad", Kind: "evm", Chain: "monad", Status: "degraded", Capabilities: []string{"spot", "execute"},
			Positions: []apiclient.VenuePosition{{Market: "MON", SizeUSD: 42.1, Mark: 0.0263}},
			Error:     "native balance: timeout",
			Meta:      map[string]any{"network": "mainnet", "chain_id": 143.0, "head_block": 41234567.0, "native_balance": 3.2, "native_symbol": "MON", "address": "0x1234567890abcdef1234567890abcdef12345678", "protocol": "uniswap_v3"},
		}}
		m := loadedModel(t, f, sz.w, sz.h)
		press(t, m, "3")
		out := m.View().Content
		for _, want := range []string{"monad", "evm", "degraded", "spot,execute", "chain 143", "block 41,234,567", "MON", "LONG", "native balance: timeout"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d monad venue view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
	}
}

// TestStrategiesLastActionFallback: with no decision in memory the
// STRATEGIES table shows the status's last_action instead of a dash.
func TestStrategiesLastActionFallback(t *testing.T) {
	f := newFake()
	f.decisions = nil
	f.statuses[0].LastAction = "rebalance ETH +1"
	m := loadedModel(t, f, 120, 40)
	if out := m.View().Content; !strings.Contains(out, "rebalance ETH +1") {
		t.Errorf("strategies view missing last_action fallback:\n%s", out)
	}
}

func TestViewSmokeGovernor(t *testing.T) {
	for _, sz := range smokeSizes {
		m := loadedModel(t, newFake(), sz.w, sz.h)
		press(t, m, "4")
		out := m.View().Content
		for _, want := range []string{"GOVERNOR", "MODE", "manual", "threshold", "auto", "MIN CONFIDENCE", "0.75", "MAX NOTIONAL", "1000", "MAX OPEN INTENTS", "kill switch armed", "in sync"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d governor view missing %q", sz.w, sz.h, want)
			}
		}
		if got := rows(out); got != sz.h {
			t.Errorf("%dx%d rows = %d", sz.w, sz.h, got)
		}
		press(t, m, "K")
		out = m.View().Content
		if !strings.Contains(out, "KILL SWITCH") || rows(out) != sz.h {
			t.Errorf("%dx%d kill prompt: rows=%d", sz.w, sz.h, rows(out))
		}
		press(t, m, "esc")
		m.Update(governorMsg(apiclient.GovernorSettings{Mode: "manual", Killed: true}))
		out = m.View().Content
		if !strings.Contains(out, "KILLED") || rows(out) != sz.h {
			t.Errorf("%dx%d killed: rows=%d", sz.w, sz.h, rows(out))
		}
	}
}

func TestViewSmokeOfflineAndTiny(t *testing.T) {
	m := New(Config{CoreURL: "http://127.0.0.1:8787"})
	if m.View().Content != "" {
		t.Error("unsized view should be empty")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := m.View().Content
	if !strings.Contains(out, "connecting") || rows(out) != 24 {
		t.Errorf("connecting view:\n%s", out)
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(m.View().Content, "needs at least") {
		t.Error("tiny view should explain the minimum size")
	}
}

func TestQuestionRenderingShapes(t *testing.T) {
	q := apiclient.Question{Type: "score", Instructions: "x", Criteria: apiclient.Criteria{Levels: []string{"a", "b", "c"}}}
	a := apiclient.Answer{Type: "score", Score: 2, Probabilities: map[string]float64{"0": 0, "1": 0.2, "2": 0.8}}
	lines := questionLines("q", q, a, 60)
	if len(lines) != 5 { // head + 3 levels + rubric (no confidence)
		t.Errorf("score lines = %d: %q", len(lines), lines)
	}
	r := rubric(0.5, 3, 21)
	if !strings.Contains(r, "●") || !strings.Contains(r, "┼") {
		t.Errorf("rubric = %q", r)
	}
	if rubric(0, 3, 2) != "" {
		t.Error("rubric below width should be empty")
	}
	// Legend-only score (no question criteria) still enumerates levels.
	a.Legend = map[string]apiclient.LegendEntry{"0": {What: "lo"}, "1": {What: "mid"}, "2": {What: "hi"}}
	lines = questionLines("q", apiclient.Question{Type: "score"}, a, 60)
	if len(lines) != 5 || !strings.Contains(lines[3], "hi") {
		t.Errorf("legend score lines = %q", lines)
	}
	// Choice with no criteria falls back to the probability keys.
	c := 0.5
	lines = questionLines("q", apiclient.Question{Type: "choice"}, apiclient.Answer{Type: "choice", Choice: "b", Probabilities: map[string]float64{"a": 0.4, "b": 0.6}, Confidence: &c}, 60)
	if len(lines) != 4 || !strings.Contains(lines[2], "●") {
		t.Errorf("choice lines = %q", lines)
	}
	// Noul.
	lines = questionLines("q", apiclient.Question{Type: "noul"}, apiclient.Answer{Type: "noul", Noul: 0.93}, 60)
	if len(lines) != 2 || !strings.Contains(lines[1], "0.93") {
		t.Errorf("noul lines = %q", lines)
	}
	// Narrow width never panics.
	for w := 1; w < 30; w++ {
		questionLines("q", q, a, w)
		rubric(1.5, 3, w)
	}
}
