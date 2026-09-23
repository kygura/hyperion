package runtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
)

// Governor is the human-agency layer: every intent passes Review before it
// can reach a venue. Mode decides whether an intent executes or becomes a
// proposal; the hard limits (max_notional_usd, max_open_intents, killed)
// gate regardless of mode. Per-strategy overrides (min_confidence, and
// optionally mode/limits) apply on top of the global settings.
//
// The daily-loss kill is not re-implemented here: the hyperliquid venue
// routes Place through the legacy executor, whose riskCheck owns that gate.
type Governor struct {
	mu       sync.RWMutex
	settings strategy.GovernorSettings
	now      func() time.Time
	// openIntents reports proposals/approvals not yet terminal; injected by
	// the store so the governor holds no record state itself.
	openIntents func() int
}

// NewGovernor builds a governor. openIntents may be nil (limit not enforced).
func NewGovernor(s strategy.GovernorSettings, openIntents func() int) *Governor {
	if s.Mode == "" {
		s.Mode = strategy.ModeManual
	}
	return &Governor{settings: s, now: time.Now, openIntents: openIntents}
}

// Settings returns the current global settings.
func (g *Governor) Settings() strategy.GovernorSettings {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.settings
}

// Update replaces the editable settings (mode, min_confidence, limits). The
// killed flag is not editable through Update: Kill sets it, Revive clears it.
func (g *Governor) Update(s strategy.GovernorSettings) (strategy.GovernorSettings, error) {
	if err := s.Validate(); err != nil {
		return g.Settings(), err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s.Killed = g.settings.Killed
	g.settings = s
	return g.settings, nil
}

// Kill sets the killed flag: every subsequent Review rejects. The runner
// pairs it with disabling every strategy and rejecting open proposals.
func (g *Governor) Kill() strategy.GovernorSettings {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settings.Killed = true
	return g.settings
}

// Revive clears the killed flag (operator re-arm after a kill).
func (g *Governor) Revive() strategy.GovernorSettings {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settings.Killed = false
	return g.settings
}

// Killed reports the kill flag.
func (g *Governor) Killed() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.settings.Killed
}

// Effective returns the settings a strategy sees after its override.
func (g *Governor) Effective(o strategy.GovernorOverride) strategy.GovernorSettings {
	return o.Apply(g.Settings())
}

// Review decides an intent's first verdict under the effective settings:
//
//   - killed                      → rejected  (by governor)
//   - size_usd > max_notional_usd → gated     (by gate)
//   - open intents >= max_open    → gated     (by gate)
//   - hold                        → approved  (no-op; the runner never places it)
//   - mode manual                 → proposed
//   - mode threshold              → approved when confidence >= min_confidence, else proposed
//   - mode auto                   → approved
func (g *Governor) Review(in strategy.Intent, o strategy.GovernorOverride) strategy.Verdict {
	s := g.Effective(o)
	v := strategy.Verdict{IntentID: in.ID, By: strategy.ByGovernor, TS: g.now()}
	if s.Killed {
		v.Status, v.Reason = strategy.StatusRejected, "killed"
		return v
	}
	if in.Action == strategy.ActionHold {
		v.Status, v.Reason = strategy.StatusApproved, "hold: no order"
		return v
	}
	if s.MaxNotionalUSD > 0 && in.SizeUSD > s.MaxNotionalUSD {
		v.Status, v.By = strategy.StatusGated, strategy.ByGate
		v.Reason = fmt.Sprintf("size_usd %.0f exceeds max_notional_usd %.0f", in.SizeUSD, s.MaxNotionalUSD)
		return v
	}
	if s.MaxOpenIntents > 0 && g.openIntents != nil {
		if n := g.openIntents(); n >= s.MaxOpenIntents {
			v.Status, v.By = strategy.StatusGated, strategy.ByGate
			v.Reason = fmt.Sprintf("open intents %d at max_open_intents %d", n, s.MaxOpenIntents)
			return v
		}
	}
	switch s.Mode {
	case strategy.ModeAuto:
		v.Status, v.Reason = strategy.StatusApproved, "mode=auto"
	case strategy.ModeThreshold:
		if in.Confidence >= s.MinConfidence {
			v.Status = strategy.StatusApproved
			v.Reason = fmt.Sprintf("mode=threshold confidence %.2f >= %.2f", in.Confidence, s.MinConfidence)
		} else {
			v.Status = strategy.StatusProposed
			v.Reason = fmt.Sprintf("mode=threshold confidence %.2f < %.2f", in.Confidence, s.MinConfidence)
		}
	default:
		v.Status, v.Reason = strategy.StatusProposed, "mode=manual"
	}
	return v
}
