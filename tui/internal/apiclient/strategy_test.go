package apiclient

import (
	"encoding/json"
	"testing"
	"time"
)

// The payloads below are the PROTOCOL.md examples verbatim (docs/jev/PROTOCOL.md).

const (
	protoQuestionChoice = `{ "type": "choice", "instructions": "Which regime is this market in", "criteria": { "risk_on": "...", "risk_off": "...", "chop": "..." } }`
	protoQuestionScore  = `{ "type": "score",  "instructions": "How crowded is the long side", "criteria": ["not crowded", "somewhat", "extremely"] }`
	protoQuestionNoul   = `{ "type": "noul",   "instructions": "Funding is extreme relative to the 30-day range" }`

	protoAnswerChoice = `{ "type": "choice", "choice": "risk_off", "probabilities": { "risk_on": 0.1, "risk_off": 0.82, "chop": 0.08 }, "confidence": 0.71 }`
	protoAnswerScore  = `{ "type": "score",  "score": 1.35, "probabilities": { "0": 0.05, "1": 0.55, "2": 0.4 }, "legend": { "0": "not crowded", "1": "somewhat", "2": "extremely" }, "confidence": 0.6 }`
	protoAnswerNoul   = `{ "type": "noul",   "noul": 0.93 }`

	protoParamNumber = `{ "key": "size_usd", "type": "number", "label": "Size per intent (USD)", "default": 250, "min": 10, "max": 100000, "step": 10, "description": "..." }`
	protoParamEnum   = `{ "key": "side_bias", "type": "enum", "options": ["both", "long_only", "short_only"], "default": "both" }`

	protoManifest = `{
  "id": "funding_skew", "name": "Funding skew fade", "version": "0.1.0",
  "description": "...", "venues": ["hyperliquid", "paper"], "cadence": "5m",
  "markets": ["BTC", "ETH"],
  "params": [ ` + protoParamNumber + `, ` + protoParamEnum + ` ],
  "questions": { "funding_extreme": ` + protoQuestionNoul + `, "direction": ` + protoQuestionChoice + `, "crowding": ` + protoQuestionScore + ` }
}`

	protoGovernor = `{ "mode": "manual", "min_confidence": 0.75, "max_notional_usd": 1000, "max_open_intents": 5, "killed": false }`
	protoConfig   = `{ "id": "funding_skew", "enabled": false, "venue": "paper", "params": { "size_usd": 250, "min_p": 0.8 }, "governor": { "min_confidence": 0.75 } }`
	protoStatus   = `{ "manifest": ` + protoManifest + `, "config": ` + protoConfig + `, "last_run_at": "…", "last_decision_id": "…", "last_error": "", "next_run_at": "…" }`

	protoIntent = `{
  "id": "01J…", "strategy_id": "funding_skew", "venue": "paper", "market": "ETH",
  "action": "open_short",
  "size_usd": 250, "target_weight": null, "price_limit": null,
  "reason": "funding_extreme=0.93 direction=fade_short crowding=1.35",
  "confidence": 0.71
}`
	protoVerdict  = `{ "intent_id": "01J…", "status": "proposed", "by": "governor", "reason": "mode=manual", "ts": "…" }`
	protoDecision = `{
  "id": "01J…", "ts": "2026-09-23T10:00:00Z", "strategy_id": "funding_skew", "venue": "paper", "dry_run": false,
  "state_digest": "sha256:…", "state": { "funding": 0.01 },
  "questions": { "crowding": ` + protoQuestionScore + ` }, "answers": { "crowding": ` + protoAnswerScore + ` },
  "intents": [ ` + protoIntent + ` ], "verdicts": [ ` + protoVerdict + ` ],
  "model": "jev-1.13.0", "usage": { "input_tokens": 412, "output_tokens": 30 }, "latency_ms": 180,
  "error": ""
}`
	protoVenue = `{ "id": "paper", "kind": "paper", "chain": "none", "status": "connected", "capabilities": ["perps"], "positions": [ { "market": "ETH", "size_usd": -250, "entry": 3100.5, "mark": 3080.2, "upnl_usd": 1.6 } ] }`
)

func mustDecode[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %T: %v\n%s", out, err, raw)
	}
	return out
}

func TestDecodeQuestions(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		typ     string
		options int
		levels  int
	}{
		{"choice", protoQuestionChoice, "choice", 3, 0},
		{"score", protoQuestionScore, "score", 0, 3},
		{"noul", protoQuestionNoul, "noul", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := mustDecode[Question](t, c.raw)
			if q.Type != c.typ {
				t.Errorf("type = %q, want %q", q.Type, c.typ)
			}
			if len(q.Criteria.Options) != c.options || len(q.Criteria.Levels) != c.levels {
				t.Errorf("criteria = %+v, want %d options / %d levels", q.Criteria, c.options, c.levels)
			}
			if q.Instructions == "" {
				t.Error("instructions empty")
			}
		})
	}
	q := mustDecode[Question](t, protoQuestionScore)
	if q.Criteria.Levels[2] != "extremely" {
		t.Errorf("levels = %v", q.Criteria.Levels)
	}
	// Round trip preserves the shape.
	for _, raw := range []string{protoQuestionChoice, protoQuestionScore} {
		q := mustDecode[Question](t, raw)
		buf, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		q2 := mustDecode[Question](t, string(buf))
		if len(q2.Criteria.Options) != len(q.Criteria.Options) || len(q2.Criteria.Levels) != len(q.Criteria.Levels) {
			t.Errorf("round trip lost criteria: %s", buf)
		}
	}
}

func TestDecodeAnswers(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		check      func(t *testing.T, a Answer)
		confidence *float64
	}{
		{"choice", protoAnswerChoice, func(t *testing.T, a Answer) {
			if a.Choice != "risk_off" || a.Probabilities["risk_off"] != 0.82 {
				t.Errorf("choice answer = %+v", a)
			}
		}, ptr(0.71)},
		{"score", protoAnswerScore, func(t *testing.T, a Answer) {
			if a.Score != 1.35 || a.Probabilities["1"] != 0.55 || a.LegendText("2") != "extremely" {
				t.Errorf("score answer = %+v", a)
			}
		}, ptr(0.6)},
		{"noul", protoAnswerNoul, func(t *testing.T, a Answer) {
			if a.Noul != 0.93 {
				t.Errorf("noul answer = %+v", a)
			}
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := mustDecode[Answer](t, c.raw)
			c.check(t, a)
			got, ok := a.ConfidenceValue()
			if (c.confidence == nil) == ok {
				t.Fatalf("confidence present = %v, want %v", ok, c.confidence != nil)
			}
			if c.confidence != nil && got != *c.confidence {
				t.Errorf("confidence = %v, want %v", got, *c.confidence)
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }

// TestLegendEntryStringOrObject covers "legend values may be strings or
// objects; clients render what when it is an object".
func TestLegendEntryStringOrObject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"string", `"not crowded"`, "not crowded"},
		{"object what", `{"what": "somewhat", "score": 1}`, "somewhat"},
		{"object label", `{"label": "extremely"}`, "extremely"},
		{"object other", `{"x": 1}`, `{"x": 1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mustDecode[LegendEntry](t, c.raw)
			if e.String() != c.want {
				t.Errorf("String() = %q, want %q", e.String(), c.want)
			}
			buf, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			_ = json.Unmarshal([]byte(c.raw), &a)
			_ = json.Unmarshal(buf, &b)
			if ra, _ := json.Marshal(a); string(ra) != mustJSON(b) {
				t.Errorf("round trip = %s, want %s", buf, c.raw)
			}
		})
	}
	a := mustDecode[Answer](t, `{"type":"score","score":0.5,"legend":{"0":{"what":"low"},"1":"high"}}`)
	if a.LegendText("0") != "low" || a.LegendText("1") != "high" || a.LegendText("9") != "" {
		t.Errorf("legend = %+v", a.Legend)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestDecodeParamSpecs(t *testing.T) {
	n := mustDecode[ParamSpec](t, protoParamNumber)
	if n.Key != "size_usd" || n.Type != "number" || n.Label != "Size per intent (USD)" {
		t.Errorf("number spec = %+v", n)
	}
	if n.Default != float64(250) || n.Min == nil || *n.Min != 10 || n.Max == nil || *n.Max != 100000 || n.Step == nil || *n.Step != 10 {
		t.Errorf("number bounds = default %v min %v max %v step %v", n.Default, n.Min, n.Max, n.Step)
	}
	e := mustDecode[ParamSpec](t, protoParamEnum)
	if e.Type != "enum" || len(e.Options) != 3 || e.Options[1] != "long_only" || e.Default != "both" {
		t.Errorf("enum spec = %+v", e)
	}
	if e.Min != nil || e.Max != nil || e.Step != nil {
		t.Errorf("enum spec has bounds: %+v", e)
	}
}

func TestDecodeManifest(t *testing.T) {
	m := mustDecode[Manifest](t, protoManifest)
	if m.ID != "funding_skew" || m.Name != "Funding skew fade" || m.Version != "0.1.0" || m.Cadence != "5m" {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.Venues) != 2 || len(m.Markets) != 2 || len(m.Params) != 2 || len(m.Questions) != 3 {
		t.Errorf("manifest counts = venues %d markets %d params %d questions %d", len(m.Venues), len(m.Markets), len(m.Params), len(m.Questions))
	}
	if m.Questions["direction"].Type != "choice" || m.Questions["crowding"].Criteria.Levels[0] != "not crowded" {
		t.Errorf("questions = %+v", m.Questions)
	}
}

func TestDecodeGovernorAndConfig(t *testing.T) {
	g := mustDecode[GovernorSettings](t, protoGovernor)
	if g.Mode != "manual" || g.MinConfidence != 0.75 || g.MaxNotionalUSD != 1000 || g.MaxOpenIntents != 5 || g.Killed {
		t.Errorf("governor = %+v", g)
	}
	c := mustDecode[StrategyConfig](t, protoConfig)
	if c.ID != "funding_skew" || c.Enabled || c.Venue != "paper" || c.Params["size_usd"] != float64(250) || c.Params["min_p"] != 0.8 {
		t.Errorf("config = %+v", c)
	}
	if c.Governor == nil || c.Governor.MinConfidence == nil || *c.Governor.MinConfidence != 0.75 || c.Governor.Mode != nil {
		t.Errorf("config governor override = %+v", c.Governor)
	}
	// An override with no fields marshals as {} and a nil one is omitted.
	buf, _ := json.Marshal(StrategyConfig{ID: "x", Params: map[string]any{}})
	if string(buf) != `{"id":"x","enabled":false,"venue":"","params":{}}` {
		t.Errorf("config marshal = %s", buf)
	}
}

func TestDecodeStrategyStatusLenientTimestamps(t *testing.T) {
	s := mustDecode[StrategyStatus](t, protoStatus)
	if s.Manifest.ID != "funding_skew" || s.Config.Venue != "paper" || s.LastError != "" || s.LastDecisionID != "…" {
		t.Errorf("status = %+v", s)
	}
	if !s.LastRunAt.IsZero() || !s.NextRunAt.IsZero() {
		t.Errorf("placeholder timestamps should decode to zero: %v %v", s.LastRunAt, s.NextRunAt)
	}
	cases := []struct {
		raw  string
		zero bool
	}{
		{`""`, true}, {`null`, true}, {`"…"`, true}, {`"2026-09-23T10:00:00Z"`, false}, {`"2026-09-23T10:00:00.123456Z"`, false},
	}
	for _, c := range cases {
		var ts Time
		if err := json.Unmarshal([]byte(c.raw), &ts); err != nil {
			t.Errorf("Time(%s): %v", c.raw, err)
		}
		if ts.IsZero() != c.zero {
			t.Errorf("Time(%s).IsZero() = %v, want %v", c.raw, ts.IsZero(), c.zero)
		}
	}
	var ts Time
	_ = json.Unmarshal([]byte(`"2026-09-23T10:00:00Z"`), &ts)
	if ts.Time != time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC) {
		t.Errorf("parsed = %v", ts)
	}
	if buf, _ := json.Marshal(ts); string(buf) != `"2026-09-23T10:00:00Z"` {
		t.Errorf("marshal = %s", buf)
	}
}

func TestDecodeIntentVerdictDecision(t *testing.T) {
	in := mustDecode[Intent](t, protoIntent)
	if in.Action != "open_short" || in.Market != "ETH" || in.SizeUSD != 250 || in.TargetWeight != nil || in.PriceLimit != nil || in.Confidence != 0.71 {
		t.Errorf("intent = %+v", in)
	}
	v := mustDecode[IntentVerdict](t, protoVerdict)
	if v.IntentID != "01J…" || v.Status != "proposed" || v.By != "governor" || v.Reason != "mode=manual" || !v.TS.IsZero() {
		t.Errorf("verdict = %+v", v)
	}
	d := mustDecode[DecisionRecord](t, protoDecision)
	if d.ID != "01J…" || d.StrategyID != "funding_skew" || d.DryRun || d.Model != "jev-1.13.0" || d.LatencyMS != 180 || d.Usage.InputTokens != 412 || d.Usage.OutputTokens != 30 {
		t.Errorf("decision = %+v", d)
	}
	if d.TS.IsZero() || len(d.State) == 0 || d.StateDigest != "sha256:…" {
		t.Errorf("decision ts/state = %v %s %s", d.TS, d.State, d.StateDigest)
	}
	if len(d.Intents) != 1 || len(d.Verdicts) != 1 || d.Answers["crowding"].Score != 1.35 || d.Questions["crowding"].Type != "score" {
		t.Errorf("decision contents = %+v", d)
	}
	lv, ok := d.LatestVerdict("01J…")
	if !ok || lv.Status != "proposed" {
		t.Errorf("LatestVerdict = %+v %v", lv, ok)
	}
	if _, ok := d.LatestVerdict("nope"); ok {
		t.Error("LatestVerdict found a verdict for an unknown intent")
	}
	d.Verdicts = append(d.Verdicts, IntentVerdict{IntentID: "01J…", Status: "approved", By: "operator"})
	if lv, _ := d.LatestVerdict("01J…"); lv.Status != "approved" {
		t.Errorf("LatestVerdict after append = %+v", lv)
	}
}

func TestDecodeVenueStatus(t *testing.T) {
	v := mustDecode[VenueStatus](t, protoVenue)
	if v.ID != "paper" || v.Kind != "paper" || v.Chain != "none" || v.Status != "connected" || len(v.Capabilities) != 1 || v.Capabilities[0] != "perps" {
		t.Errorf("venue = %+v", v)
	}
	if len(v.Positions) != 1 || v.Positions[0].Market != "ETH" || v.Positions[0].SizeUSD != -250 || v.Positions[0].Entry != 3100.5 || v.Positions[0].Mark != 3080.2 || v.Positions[0].UPnLUSD != 1.6 {
		t.Errorf("positions = %+v", v.Positions)
	}
}

// TestDecodeMonadVenue: PROTOCOL.md's evm VenueStatus (spot capabilities,
// error, meta) decodes; meta numbers arrive as float64.
func TestDecodeMonadVenue(t *testing.T) {
	raw := `{ "id": "monad", "kind": "evm", "chain": "monad", "status": "degraded", "capabilities": ["spot", "execute"],
	  "positions": [ { "market": "MON", "size_usd": 42.1, "entry": 0, "mark": 0.0263, "upnl_usd": 0 } ],
	  "error": "native balance: timeout",
	  "meta": { "network": "mainnet", "chain_id": 143, "head_block": 41234567, "native_balance": 3.2, "native_symbol": "MON", "address": "0xabc", "protocol": "uniswap_v3" } }`
	v := mustDecode[VenueStatus](t, raw)
	if v.ID != "monad" || v.Kind != "evm" || v.Chain != "monad" || v.Status != "degraded" || len(v.Capabilities) != 2 || v.Error == "" {
		t.Errorf("venue = %+v", v)
	}
	if v.Meta["chain_id"] != float64(143) || v.Meta["head_block"] != float64(41234567) || v.Meta["protocol"] != "uniswap_v3" {
		t.Errorf("meta = %+v", v.Meta)
	}
	if len(v.Positions) != 1 || v.Positions[0].Market != "MON" || v.Positions[0].Mark != 0.0263 {
		t.Errorf("positions = %+v", v.Positions)
	}
}

func TestDecodeStrategyEvents(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		check func(t *testing.T, p any)
	}{
		{"decision", `{ "type": "strategy.decision", "data": ` + protoDecision + ` }`, func(t *testing.T, p any) {
			d, ok := p.(DecisionRecord)
			if !ok || d.StrategyID != "funding_skew" {
				t.Errorf("payload = %T %+v", p, p)
			}
		}},
		{"verdict", `{ "type": "strategy.verdict",  "data": { "decision_id": "01J…", "verdict": ` + protoVerdict + ` } }`, func(t *testing.T, p any) {
			v, ok := p.(StrategyVerdictEvent)
			if !ok || v.DecisionID != "01J…" || v.Verdict.Status != "proposed" {
				t.Errorf("payload = %T %+v", p, p)
			}
		}},
		{"config", `{ "type": "strategy.config",   "data": ` + protoStatus + ` }`, func(t *testing.T, p any) {
			s, ok := p.(StrategyStatus)
			if !ok || s.Manifest.ID != "funding_skew" {
				t.Errorf("payload = %T %+v", p, p)
			}
		}},
		{"governor", `{ "type": "strategy.governor", "data": ` + protoGovernor + ` }`, func(t *testing.T, p any) {
			g, ok := p.(GovernorSettings)
			if !ok || g.Mode != "manual" {
				t.Errorf("payload = %T %+v", p, p)
			}
		}},
		{"venue", `{ "type": "strategy.venue",    "data": ` + protoVenue + ` }`, func(t *testing.T, p any) {
			v, ok := p.(VenueStatus)
			if !ok || v.ID != "paper" {
				t.Errorf("payload = %T %+v", p, p)
			}
		}},
		// The legacy daemon spells the key "topic"; both must decode.
		{"topic spelling", `{ "topic": "strategy.governor", "data": ` + protoGovernor + ` }`, func(t *testing.T, p any) {
			if _, ok := p.(GovernorSettings); !ok {
				t.Errorf("payload = %T", p)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok, err := DecodeStrategyFrame([]byte(c.frame))
			if err != nil || !ok {
				t.Fatalf("decode: ok=%v err=%v", ok, err)
			}
			c.check(t, p)
		})
	}

	// Legacy topics are not strategy events and are left to the old switch.
	for _, kind := range []string{"bar", "mids", "verdict", "journal", "thesis", "status"} {
		p, ok, err := DecodeStrategyFrame([]byte(`{"topic":"` + kind + `","data":{}}`))
		if ok || err != nil || p != nil {
			t.Errorf("%s: ok=%v err=%v payload=%v, want ignored", kind, ok, err, p)
		}
		if IsStrategyEvent(kind) {
			t.Errorf("IsStrategyEvent(%q) = true", kind)
		}
	}
	if _, _, err := DecodeStrategyFrame([]byte(`not json`)); err == nil {
		t.Error("garbage frame did not error")
	}
	if _, ok, err := DecodeStrategyFrame([]byte(`{"type":"strategy.decision","data":"nope"}`)); !ok || err == nil {
		t.Errorf("bad data: ok=%v err=%v, want ok with error", ok, err)
	}
}
