// Package builtin holds the reference strategies (SPEC.md "Reference
// strategies"). They exist to prove the contract end to end — fixture
// states, fake-decider tests, a deterministic answers → intents mapping —
// and are not a claim of edge.
package builtin

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/registry"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

func init() {
	registry.Register("funding_skew", func() strategy.Strategy { return &FundingSkew{} })
	registry.Register("regime_rotation", func() strategy.Strategy { return &RegimeRotation{} })
}

// FundingSkew fades a crowded side: when funding on the most extreme market
// of the universe is extreme and the decider names a crowded side, it opens
// the opposite position sized by params.size_usd.
type FundingSkew struct{}

// FundingSkewManifest is the static manifest (exported for tests/fixtures).
var FundingSkewManifest = strategy.Manifest{
	ID:          "funding_skew",
	Name:        "Funding skew fade",
	Version:     "0.1.0",
	Description: "Fade the crowded side when perp funding is extreme: short when longs pay up, long when shorts pay up.",
	Venues:      []string{"hyperliquid", "paper"},
	Cadence:     "5m",
	Markets:     []string{"BTC", "ETH"},
	Params: []strategy.ParamSpec{
		{Key: "size_usd", Type: strategy.ParamNumber, Label: "Size per intent (USD)", Default: 250.0, Min: strategy.F(10), Max: strategy.F(100000), Step: strategy.F(10), Description: "Notional of each fade."},
		{Key: "min_p", Type: strategy.ParamNumber, Label: "Min P(funding extreme)", Default: 0.8, Min: strategy.F(0), Max: strategy.F(1), Step: strategy.F(0.05), Description: "Act only when the decider's funding_extreme likelihood is at least this."},
		{Key: "min_crowding", Type: strategy.ParamNumber, Label: "Min crowding score", Default: 1.0, Min: strategy.F(0), Max: strategy.F(2), Step: strategy.F(0.25), Description: "Act only when the crowding score (0 not, 1 somewhat, 2 extremely) is at least this."},
		{Key: "funding_ref_hourly", Type: strategy.ParamNumber, Label: "Reference |funding| per hour", Default: 0.0005, Min: strategy.F(0.00001), Max: strategy.F(0.01), Step: strategy.F(0.00005), Description: "Funding magnitude the state labels as 'extreme' (bucketed for the decider)."},
		{Key: "side_bias", Type: strategy.ParamEnum, Label: "Side bias", Options: []string{"both", "long_only", "short_only"}, Default: "both", Description: "Restrict which fades may open."},
	},
	Questions: map[string]decider.Question{
		"funding_extreme": decider.Noul("The focus market's funding rate is extreme relative to normal perp funding and the premium confirms it"),
		"direction": decider.Choice("Which side of the focus market is crowded and should be faded", map[string]string{
			"fade_long":  "longs are crowded: funding strongly positive, premium positive, price up on the day — open a short",
			"fade_short": "shorts are crowded: funding strongly negative, premium negative, price down on the day — open a long",
			"none":       "funding is unremarkable or the signals disagree — do nothing",
		}),
		"crowding": decider.Score("How crowded is the paying side of the focus market", []string{"not crowded", "somewhat crowded", "extremely crowded"}),
	},
}

// Manifest implements strategy.Strategy.
func (FundingSkew) Manifest() strategy.Manifest { return FundingSkewManifest }

// FundingMarketState is one market's compact state.
type FundingMarketState struct {
	Symbol           string  `json:"symbol"`
	FundingHourlyBps float64 `json:"funding_hourly_bps"`
	FundingAnnualPct float64 `json:"funding_annual_pct"`
	FundingLabel     string  `json:"funding_label"` // e.g. "extreme positive"
	PremiumBps       float64 `json:"premium_bps"`
	Ret24hPct        float64 `json:"ret_24h_pct"`
	OIUSDMillions    float64 `json:"oi_usd_m"`
	PositionUSD      float64 `json:"position_usd"`
}

// FundingSkewState is the state sent to the decider.
type FundingSkewState struct {
	TS      string               `json:"ts"`
	Focus   string               `json:"focus"` // market with the largest |funding|
	Markets []FundingMarketState `json:"markets"`
}

// Snapshot implements strategy.Strategy.
func (FundingSkew) Snapshot(ctx context.Context, v venue.Venue, p strategy.Params) (strategy.State, error) {
	markets, err := v.Markets(ctx, p.Markets(FundingSkewManifest))
	if err != nil {
		return nil, err
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("funding_skew: no markets for %v", p.Markets(FundingSkewManifest))
	}
	positions, err := v.Positions(ctx)
	if err != nil {
		return nil, err
	}
	return BuildFundingSkewState(time.Now().UTC(), markets, positions, p), nil
}

// BuildFundingSkewState is the pure state builder (fixture-testable).
func BuildFundingSkewState(now time.Time, markets []venue.Market, positions []venue.Position, p strategy.Params) FundingSkewState {
	ref := p.Float("funding_ref_hourly", 0.0005)
	st := FundingSkewState{TS: now.Format(time.RFC3339)}
	best := 0.0
	for _, m := range markets {
		ms := FundingMarketState{
			Symbol:           m.Symbol,
			FundingHourlyBps: round(m.Funding*1e4, 3),
			FundingAnnualPct: round(m.Funding*24*365*100, 1),
			FundingLabel:     fundingLabel(m.Funding, ref),
			PremiumBps:       round(m.Premium*1e4, 2),
			Ret24hPct:        round(m.DayChange*100, 2),
			OIUSDMillions:    round(m.OpenInterestUSD/1e6, 1),
			PositionUSD:      round(venue.PositionFor(positions, m.Symbol).SizeUSD, 2),
		}
		st.Markets = append(st.Markets, ms)
		if a := math.Abs(m.Funding); a > best || st.Focus == "" {
			best = a
			st.Focus = m.Symbol
		}
	}
	sort.Slice(st.Markets, func(i, j int) bool { return st.Markets[i].Symbol < st.Markets[j].Symbol })
	return st
}

// fundingLabel buckets funding for a text-trained decider (SPEC: compute the
// numbers in Go, send each with a label).
func fundingLabel(f, ref float64) string {
	sign := "positive"
	if f < 0 {
		sign = "negative"
	}
	a := math.Abs(f)
	switch {
	case a >= ref:
		return "extreme " + sign
	case a >= ref/2:
		return "elevated " + sign
	case a >= ref/5:
		return "normal " + sign
	default:
		return "flat"
	}
}

// Decide implements strategy.Strategy: open opposite to the crowded side on
// the focus market when funding_extreme >= min_p, crowding >= min_crowding
// and direction != none. Already positioned in the fade direction → nothing.
func (FundingSkew) Decide(state strategy.State, a strategy.Answers, p strategy.Params) []strategy.Intent {
	st, ok := asFundingState(state)
	if !ok || st.Focus == "" {
		return nil
	}
	extreme := a["funding_extreme"].NoulValue()
	dir := a["direction"]
	crowd := a["crowding"].ScoreValue()
	if extreme < p.Float("min_p", 0.8) || crowd < p.Float("min_crowding", 1.0) {
		return nil
	}
	var action string
	switch dir.Choice {
	case "fade_long":
		action = strategy.ActionOpenShort
	case "fade_short":
		action = strategy.ActionOpenLong
	default:
		return nil
	}
	switch p.String("side_bias", "both") {
	case "long_only":
		if action != strategy.ActionOpenLong {
			return nil
		}
	case "short_only":
		if action != strategy.ActionOpenShort {
			return nil
		}
	}
	var focus FundingMarketState
	for _, m := range st.Markets {
		if m.Symbol == st.Focus {
			focus = m
		}
	}
	if (action == strategy.ActionOpenShort && focus.PositionUSD < 0) || (action == strategy.ActionOpenLong && focus.PositionUSD > 0) {
		return nil // already faded
	}
	conf := dir.Conf()
	if c, ok := dir.Probabilities[dir.Choice]; ok && conf == 0 {
		conf = c
	}
	return []strategy.Intent{{
		Market:     st.Focus,
		Action:     action,
		SizeUSD:    p.Float("size_usd", 250),
		Reason:     fmt.Sprintf("funding_extreme=%.2f direction=%s crowding=%.2f funding=%s", extreme, dir.Choice, crowd, focus.FundingLabel),
		Confidence: conf,
	}}
}

// asFundingState accepts the typed state or its JSON-decoded map form (the
// stdio path and tests hand decoded states back in).
func asFundingState(state strategy.State) (FundingSkewState, bool) {
	switch s := state.(type) {
	case FundingSkewState:
		return s, true
	case *FundingSkewState:
		return *s, true
	}
	var out FundingSkewState
	if roundTrip(state, &out) == nil && out.Focus != "" {
		return out, true
	}
	return out, false
}

func round(f float64, places int) float64 {
	pow := math.Pow(10, float64(places))
	return math.Round(f*pow) / pow
}
