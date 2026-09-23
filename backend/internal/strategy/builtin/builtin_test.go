package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/fake"
	"github.com/hyperagent/hyperagent/internal/strategy/registry"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
	"github.com/hyperagent/hyperagent/internal/strategy/venue/paper"
)

var fixtureTime = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func fixtureVenue(t *testing.T) *paper.Venue {
	t.Helper()
	v := paper.New(paper.WithClock(func() time.Time { return fixtureTime }))
	v.SetMarks(FixtureMarkets()...)
	return v
}

func TestRegistered(t *testing.T) {
	for _, id := range []string{"funding_skew", "regime_rotation"} {
		s, err := registry.Get(id)
		if err != nil {
			t.Fatalf("registry.Get(%s): %v", id, err)
		}
		m := s.Manifest()
		if m.ID != id || len(m.Questions) == 0 || len(m.Params) == 0 {
			t.Errorf("manifest %s = %+v", id, m)
		}
		for k, q := range m.Questions {
			if err := q.Validate(); err != nil {
				t.Errorf("%s question %s: %v", id, k, err)
			}
		}
		if err := strategy.Defaults(m).Validate(m); err != nil {
			t.Errorf("%s defaults invalid: %v", id, err)
		}
		if _, err := json.Marshal(m); err != nil {
			t.Errorf("%s manifest json: %v", id, err)
		}
	}
}

func TestFundingSkewStateAndDecide(t *testing.T) {
	var s FundingSkew
	m := s.Manifest()
	params := strategy.Merge(m, nil)
	state := BuildFundingSkewState(fixtureTime, FixtureMarkets()[:2], nil, params)
	if state.Focus != "ETH" || len(state.Markets) != 2 {
		t.Fatalf("state = %+v", state)
	}
	eth := state.Markets[1]
	if eth.FundingLabel != "extreme positive" || eth.FundingHourlyBps != 8 || eth.Ret24hPct != 5.2 {
		t.Errorf("eth state = %+v", eth)
	}
	if state.Markets[0].FundingLabel != "flat" {
		t.Errorf("btc label = %q", state.Markets[0].FundingLabel)
	}
	// State must stay compact and JSON-serializable.
	b, err := json.Marshal(state)
	if err != nil || len(b) > 2000 {
		t.Errorf("state json len %d err %v", len(b), err)
	}

	// Confident crowded-long answers → open_short ETH sized by size_usd.
	answers := strategy.Answers{
		"funding_extreme": fake.NoulAnswer(0.93),
		"direction":       fake.ChoiceAnswer(m.Questions["direction"], "fade_long", 0.71),
		"crowding":        fake.ScoreAnswer(m.Questions["crowding"], 1.35, 0.6),
	}
	intents := s.Decide(state, answers, params)
	if len(intents) != 1 {
		t.Fatalf("intents = %+v, want 1", intents)
	}
	in := intents[0]
	if in.Market != "ETH" || in.Action != strategy.ActionOpenShort || in.SizeUSD != 250 || in.Confidence != 0.71 {
		t.Errorf("intent = %+v", in)
	}
	if in.Reason != "funding_extreme=0.93 direction=fade_long crowding=1.35 funding=extreme positive" {
		t.Errorf("reason = %q", in.Reason)
	}

	// Below min_p → nothing.
	low := strategy.Answers{"funding_extreme": fake.NoulAnswer(0.5), "direction": answers["direction"], "crowding": answers["crowding"]}
	if got := s.Decide(state, low, params); len(got) != 0 {
		t.Errorf("low min_p intents = %+v", got)
	}
	// Threshold is a param: lowering min_p re-enables it.
	if got := s.Decide(state, low, strategy.Merge(m, strategy.Params{"min_p": 0.4})); len(got) != 1 {
		t.Errorf("min_p=0.4 intents = %+v", got)
	}
	// Not crowded → nothing.
	calm := strategy.Answers{"funding_extreme": answers["funding_extreme"], "direction": answers["direction"], "crowding": fake.ScoreAnswer(m.Questions["crowding"], 0.4, 0.8)}
	if got := s.Decide(state, calm, params); len(got) != 0 {
		t.Errorf("uncrowded intents = %+v", got)
	}
	// direction=none → nothing.
	none := strategy.Answers{"funding_extreme": answers["funding_extreme"], "direction": fake.ChoiceAnswer(m.Questions["direction"], "none", 0.9), "crowding": answers["crowding"]}
	if got := s.Decide(state, none, params); len(got) != 0 {
		t.Errorf("none intents = %+v", got)
	}
	// fade_short → open_long, but side_bias=short_only blocks it.
	short := strategy.Answers{"funding_extreme": answers["funding_extreme"], "direction": fake.ChoiceAnswer(m.Questions["direction"], "fade_short", 0.8), "crowding": answers["crowding"]}
	if got := s.Decide(state, short, params); len(got) != 1 || got[0].Action != strategy.ActionOpenLong {
		t.Errorf("fade_short intents = %+v", got)
	}
	if got := s.Decide(state, short, strategy.Merge(m, strategy.Params{"side_bias": "short_only"})); len(got) != 0 {
		t.Errorf("short_only intents = %+v", got)
	}
	// Already short the focus market → no second fade.
	positioned := BuildFundingSkewState(fixtureTime, FixtureMarkets()[:2], []venue.Position{{Market: "ETH", SizeUSD: -250}}, params)
	if got := s.Decide(positioned, answers, params); len(got) != 0 {
		t.Errorf("already-short intents = %+v", got)
	}
	// A JSON-decoded state (stdio / replay) decides identically.
	var generic any
	_ = json.Unmarshal(b, &generic)
	if got := s.Decide(generic, answers, params); len(got) != 1 || got[0].Action != strategy.ActionOpenShort {
		t.Errorf("generic-state intents = %+v", got)
	}
}

func TestFundingSkewSnapshotAndFakeDecider(t *testing.T) {
	var s FundingSkew
	m := s.Manifest()
	params := strategy.Merge(m, nil)
	state, err := s.Snapshot(context.Background(), fixtureVenue(t), params)
	if err != nil {
		t.Fatal(err)
	}
	// Uniform fake: direction picks "fade_long" (first sorted option) with
	// p=1/3, noul 0.5 → below min_p, no intent.
	d := fake.New(nil)
	res, err := d.Evaluate(context.Background(), state, m.Questions)
	if err != nil {
		t.Fatal(err)
	}
	if res.Answers["funding_extreme"].Confidence != nil {
		t.Error("noul answer must carry no confidence")
	}
	if got := s.Decide(state, res.Answers, params); len(got) != 0 {
		t.Errorf("uniform intents = %+v", got)
	}
	// Scripted fake drives the intent.
	d.Set("funding_extreme", fake.NoulAnswer(0.95))
	d.Set("direction", fake.ChoiceAnswer(m.Questions["direction"], "fade_long", 0.8))
	d.Set("crowding", fake.ScoreAnswer(m.Questions["crowding"], 1.6, 0.7))
	res, _ = d.Evaluate(context.Background(), state, m.Questions)
	if got := s.Decide(state, res.Answers, params); len(got) != 1 || got[0].Market != "ETH" {
		t.Errorf("scripted intents = %+v", got)
	}
}

func TestRegimeRotationStateAndDecide(t *testing.T) {
	var s RegimeRotation
	m := s.Manifest()
	params := strategy.Merge(m, nil)
	state := BuildRegimeState(fixtureTime, FixtureMarkets(), []venue.Position{{Market: "BTC", SizeUSD: 200}})
	if len(state.Majors) != 2 || state.Majors[0].Symbol != "BTC" || state.Majors[1].Symbol != "ETH" {
		t.Fatalf("state = %+v", state)
	}
	if state.BreadthPct != 50 || state.BreadthLabel != "mixed positive" || state.UniverseUpCount != 2 || state.UniverseDownCnt != 2 {
		t.Errorf("breadth = %+v", state)
	}
	if state.ETHvsBTC24hPct != 4.8 || state.Majors[1].Trend != "up" || state.Majors[0].Trend != "flat" {
		t.Errorf("majors = %+v", state)
	}
	if state.Majors[0].PositionUSD != 200 {
		t.Errorf("btc position = %v", state.Majors[0].PositionUSD)
	}

	// risk_on with conviction: BTC target 400 (have 200 → buy 200), ETH target 600 (buy 600).
	answers := strategy.Answers{
		"regime":     fake.ChoiceAnswer(m.Questions["regime"], "risk_on", 0.82),
		"conviction": fake.ScoreAnswer(m.Questions["conviction"], 1.5, 0.7),
	}
	intents := s.Decide(state, answers, params)
	if len(intents) != 2 {
		t.Fatalf("intents = %+v", intents)
	}
	btc, eth := intents[0], intents[1]
	if btc.Market != "BTC" || btc.Action != strategy.ActionRebalance || btc.SizeUSD != 200 || btc.Side != venue.SideBuy || *btc.TargetWeight != 0.4 {
		t.Errorf("btc intent = %+v", btc)
	}
	if eth.Market != "ETH" || eth.SizeUSD != 600 || eth.Side != venue.SideBuy || eth.Confidence != 0.82 {
		t.Errorf("eth intent = %+v", eth)
	}
	// risk_off: BTC target 0 (sell 200), ETH target -300 (sell 300).
	off := strategy.Answers{"regime": fake.ChoiceAnswer(m.Questions["regime"], "risk_off", 0.9), "conviction": answers["conviction"]}
	got := s.Decide(state, off, params)
	if len(got) != 2 || got[0].Side != venue.SideSell || got[0].SizeUSD != 200 || got[1].Side != venue.SideSell || got[1].SizeUSD != 300 {
		t.Errorf("risk_off intents = %+v", got)
	}
	// Low conviction → nothing; lowering min_conviction re-enables.
	weak := strategy.Answers{"regime": answers["regime"], "conviction": fake.ScoreAnswer(m.Questions["conviction"], 0.3, 0.5)}
	if got := s.Decide(state, weak, params); len(got) != 0 {
		t.Errorf("weak intents = %+v", got)
	}
	if got := s.Decide(state, weak, strategy.Merge(m, strategy.Params{"min_conviction": 0})); len(got) != 2 {
		t.Errorf("min_conviction=0 intents = %+v", got)
	}
	// chop with BTC weight 0.2 → target 200 == position → no BTC leg, ETH target 0 → none.
	chop := strategy.Answers{"regime": fake.ChoiceAnswer(m.Questions["regime"], "chop", 0.6), "conviction": answers["conviction"]}
	if got := s.Decide(state, chop, params); len(got) != 0 {
		t.Errorf("chop intents = %+v", got)
	}
	// Snapshot through a venue works and stays compact.
	st, err := s.Snapshot(context.Background(), fixtureVenue(t), params)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(st); len(b) > 2000 {
		t.Errorf("state too large: %d bytes", len(b))
	}
	if _, ok := st.(RegimeState); !ok {
		t.Errorf("snapshot type %T", st)
	}
	_ = decider.TypeChoice
}
