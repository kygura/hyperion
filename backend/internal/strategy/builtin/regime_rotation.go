package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// RegimeRotation rebalances BTC/ETH toward per-regime target weights of a
// notional NAV once the decider names a regime with enough conviction.
type RegimeRotation struct{}

// RegimeRotationManifest is the static manifest.
var RegimeRotationManifest = strategy.Manifest{
	ID:          "regime_rotation",
	Name:        "Regime rotation",
	Version:     "0.1.0",
	Description: "Classify the majors' regime (risk_on / risk_off / chop) and rebalance BTC and ETH toward that regime's target weights.",
	// monad: spot-only, so negative (short) target weights are refused by
	// the venue; use long-only weights and params.markets = ["ETH", "MON"].
	Venues:      []string{"hyperliquid", "paper", "monad"},
	Cadence:     "1h",
	Markets:     []string{"BTC", "ETH", "SOL", "HYPE"},
	Params: []strategy.ParamSpec{
		{Key: "nav_usd", Type: strategy.ParamNumber, Label: "Book NAV (USD)", Default: 1000.0, Min: strategy.F(100), Max: strategy.F(10000000), Step: strategy.F(100), Description: "Notional the target weights apply to."},
		{Key: "min_conviction", Type: strategy.ParamNumber, Label: "Min conviction score", Default: 1.0, Min: strategy.F(0), Max: strategy.F(2), Step: strategy.F(0.25), Description: "Rebalance only when conviction (0 low, 1 medium, 2 high) is at least this."},
		{Key: "min_delta_usd", Type: strategy.ParamNumber, Label: "Min rebalance size (USD)", Default: 25.0, Min: strategy.F(1), Max: strategy.F(100000), Step: strategy.F(5), Description: "Skip legs smaller than this."},
		{Key: "risk_on_btc", Type: strategy.ParamNumber, Label: "risk_on BTC weight", Default: 0.4, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
		{Key: "risk_on_eth", Type: strategy.ParamNumber, Label: "risk_on ETH weight", Default: 0.6, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
		{Key: "risk_off_btc", Type: strategy.ParamNumber, Label: "risk_off BTC weight", Default: 0.0, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
		{Key: "risk_off_eth", Type: strategy.ParamNumber, Label: "risk_off ETH weight", Default: -0.3, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
		{Key: "chop_btc", Type: strategy.ParamNumber, Label: "chop BTC weight", Default: 0.2, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
		{Key: "chop_eth", Type: strategy.ParamNumber, Label: "chop ETH weight", Default: 0.0, Min: strategy.F(-1), Max: strategy.F(1), Step: strategy.F(0.05)},
	},
	Questions: map[string]decider.Question{
		"regime": decider.Choice("Which regime are the crypto majors in right now", map[string]string{
			"risk_on":  "broad participation, majors up on the day, positive breadth, funding mildly positive — momentum favours longs",
			"risk_off": "majors down on the day, breadth negative, funding flat or negative, ETH weaker than BTC — de-risk or short beta",
			"chop":     "mixed or small moves, breadth near half, no directional edge — stay light",
		}),
		"conviction": decider.Score("How clearly do the signals agree on that regime", []string{"low: signals mixed", "medium: most signals agree", "high: every signal agrees"}),
	},
}

// Manifest implements strategy.Strategy.
func (RegimeRotation) Manifest() strategy.Manifest { return RegimeRotationManifest }

// RegimeMarketState is one market's compact state.
type RegimeMarketState struct {
	Symbol           string  `json:"symbol"`
	Ret24hPct        float64 `json:"ret_24h_pct"`
	FundingHourlyBps float64 `json:"funding_hourly_bps"`
	PremiumBps       float64 `json:"premium_bps"`
	PositionUSD      float64 `json:"position_usd"`
	Trend            string  `json:"trend"` // up | down | flat (24h)
}

// RegimeState is the state sent to the decider.
type RegimeState struct {
	TS               string              `json:"ts"`
	BreadthPct       float64             `json:"breadth_pct"` // share of universe up on the day
	BreadthLabel     string              `json:"breadth_label"`
	ETHvsBTC24hPct   float64             `json:"eth_vs_btc_24h_pct"`
	AvgAbsRet24hPct  float64             `json:"avg_abs_ret_24h_pct"` // crude realised-vol proxy
	VolLabel         string              `json:"vol_label"`
	Majors           []RegimeMarketState `json:"majors"` // BTC, ETH
	UniverseUpCount  int                 `json:"universe_up"`
	UniverseDownCnt  int                 `json:"universe_down"`
	UniverseTotalCnt int                 `json:"universe_total"`
}

// Snapshot implements strategy.Strategy.
func (RegimeRotation) Snapshot(ctx context.Context, v venue.Venue, p strategy.Params) (strategy.State, error) {
	markets, err := v.Markets(ctx, p.Markets(RegimeRotationManifest))
	if err != nil {
		return nil, err
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("regime_rotation: no markets for %v", p.Markets(RegimeRotationManifest))
	}
	positions, err := v.Positions(ctx)
	if err != nil {
		return nil, err
	}
	return BuildRegimeState(time.Now().UTC(), markets, positions), nil
}

// BuildRegimeState is the pure state builder (fixture-testable).
func BuildRegimeState(now time.Time, markets []venue.Market, positions []venue.Position) RegimeState {
	st := RegimeState{TS: now.Format(time.RFC3339)}
	var btc, eth float64
	var absSum float64
	for _, m := range markets {
		st.UniverseTotalCnt++
		switch {
		case m.DayChange > 0.002:
			st.UniverseUpCount++
		case m.DayChange < -0.002:
			st.UniverseDownCnt++
		}
		absSum += math.Abs(m.DayChange)
		if m.Symbol == "BTC" {
			btc = m.DayChange
		}
		if m.Symbol == "ETH" {
			eth = m.DayChange
		}
		if m.Symbol == "BTC" || m.Symbol == "ETH" {
			st.Majors = append(st.Majors, RegimeMarketState{
				Symbol:           m.Symbol,
				Ret24hPct:        round(m.DayChange*100, 2),
				FundingHourlyBps: round(m.Funding*1e4, 3),
				PremiumBps:       round(m.Premium*1e4, 2),
				PositionUSD:      round(venue.PositionFor(positions, m.Symbol).SizeUSD, 2),
				Trend:            trendLabel(m.DayChange),
			})
		}
	}
	sort.Slice(st.Majors, func(i, j int) bool { return st.Majors[i].Symbol < st.Majors[j].Symbol })
	if st.UniverseTotalCnt > 0 {
		st.BreadthPct = round(float64(st.UniverseUpCount)/float64(st.UniverseTotalCnt)*100, 0)
		st.AvgAbsRet24hPct = round(absSum/float64(st.UniverseTotalCnt)*100, 2)
	}
	st.BreadthLabel = breadthLabel(st.BreadthPct)
	st.ETHvsBTC24hPct = round((eth-btc)*100, 2)
	st.VolLabel = volLabel(st.AvgAbsRet24hPct)
	return st
}

func trendLabel(dc float64) string {
	switch {
	case dc > 0.01:
		return "up"
	case dc < -0.01:
		return "down"
	}
	return "flat"
}

func breadthLabel(pct float64) string {
	switch {
	case pct >= 75:
		return "broad advance"
	case pct >= 50:
		return "mixed positive"
	case pct >= 25:
		return "mixed negative"
	}
	return "broad decline"
}

func volLabel(avgAbsPct float64) string {
	switch {
	case avgAbsPct >= 5:
		return "high"
	case avgAbsPct >= 2:
		return "elevated"
	case avgAbsPct >= 0.75:
		return "normal"
	}
	return "quiet"
}

// Decide implements strategy.Strategy: one rebalance intent per major whose
// target (regime weight × NAV) differs from the current position by at
// least min_delta_usd. Side carries the direction of the delta.
func (RegimeRotation) Decide(state strategy.State, a strategy.Answers, p strategy.Params) []strategy.Intent {
	st, ok := asRegimeState(state)
	if !ok {
		return nil
	}
	regime := a["regime"]
	conv := a["conviction"].ScoreValue()
	switch regime.Choice {
	case "risk_on", "risk_off", "chop":
	default:
		return nil
	}
	if conv < p.Float("min_conviction", 1.0) {
		return nil
	}
	nav := p.Float("nav_usd", 1000)
	minDelta := p.Float("min_delta_usd", 25)
	conf := regime.Conf()
	var out []strategy.Intent
	for _, m := range st.Majors {
		key := regime.Choice + "_" + lower(m.Symbol)
		w := p.Float(key, 0)
		target := w * nav
		delta := target - m.PositionUSD
		if math.Abs(delta) < minDelta {
			continue
		}
		side := venue.SideBuy
		if delta < 0 {
			side = venue.SideSell
		}
		out = append(out, strategy.Intent{
			Market:       m.Symbol,
			Action:       strategy.ActionRebalance,
			SizeUSD:      round(math.Abs(delta), 2),
			TargetWeight: strategy.F(w),
			Side:         side,
			Reason:       fmt.Sprintf("regime=%s conviction=%.2f target_weight=%.2f target_usd=%.0f position_usd=%.0f", regime.Choice, conv, w, target, m.PositionUSD),
			Confidence:   conf,
		})
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func asRegimeState(state strategy.State) (RegimeState, bool) {
	switch s := state.(type) {
	case RegimeState:
		return s, true
	case *RegimeState:
		return *s, true
	}
	var out RegimeState
	if roundTrip(state, &out) == nil && len(out.Majors) > 0 {
		return out, true
	}
	return out, false
}

// roundTrip converts an arbitrary decoded state into the typed struct.
func roundTrip(in any, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
