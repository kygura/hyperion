package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/builtin"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/fake"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/jev"
	"github.com/hyperagent/hyperagent/internal/strategy/registry"
	"github.com/hyperagent/hyperagent/internal/strategy/runtime"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
	"github.com/hyperagent/hyperagent/internal/strategy/venue/paper"
)

// strategyServer wires a real runner (builtin strategies, paper venue with
// the builtin fixture marks, scripted fake decider) behind the API.
func strategyServer(t *testing.T, gov strategy.GovernorSettings) (*httptest.Server, *runtime.Runner, *fake.Decider, *config.Config) {
	t.Helper()
	pv := paper.New(paper.WithSlippageBps(0))
	pv.SetMarks(builtin.FixtureMarkets()...)
	m := builtin.FundingSkewManifest
	d := fake.New(map[string]decider.Answer{
		"funding_extreme": fake.NoulAnswer(0.93),
		"direction":       fake.ChoiceAnswer(m.Questions["direction"], "fade_long", 0.71),
		"crowding":        fake.ScoreAnswer(m.Questions["crowding"], 1.35, 0.6),
	})
	store, err := runtime.NewDecisionStore("")
	if err != nil {
		t.Fatal(err)
	}
	deps := testDeps(t, nil)
	rt, err := runtime.New(runtime.Config{
		Strategies: registry.All(),
		Venues:     map[string]venue.Venue{"paper": pv},
		Decider:    d,
		Governor:   runtime.NewGovernor(gov, store.OpenCount),
		Store:      store,
		Bus:        deps.Bus,
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.Strategy = rt
	saved := deps.Cfg
	deps.SaveConfig = func(apply func(*config.Config)) error { apply(&saved); return nil }
	s := NewServer(deps)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, rt, d, &saved
}

func do(t *testing.T, srv *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestStrategyRoutesReturn503WhenDisabled(t *testing.T) {
	s := NewServer(testDeps(t, nil))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	for _, p := range []string{"/api/strategy/manifests", "/api/strategy/configs", "/api/strategy/governor", "/api/strategy/venues", "/api/strategy/decisions"} {
		if code, body := do(t, srv, http.MethodGet, p, nil); code != http.StatusServiceUnavailable || !strings.Contains(string(body), "disabled") {
			t.Errorf("%s = %d %s", p, code, body)
		}
	}
}

func TestStrategyManifestsConfigsAndPut(t *testing.T) {
	srv, _, _, saved := strategyServer(t, strategy.GovernorSettings{Mode: strategy.ModeManual, MaxNotionalUSD: 1000, MaxOpenIntents: 5})

	code, body := do(t, srv, http.MethodGet, "/api/strategy/manifests", nil)
	var mans struct {
		Manifests []strategy.Manifest `json:"manifests"`
	}
	if err := json.Unmarshal(body, &mans); err != nil || code != 200 || len(mans.Manifests) != 2 || mans.Manifests[0].ID != "funding_skew" {
		t.Fatalf("manifests = %d %s (%v)", code, body, err)
	}
	if q := mans.Manifests[0].Questions["direction"]; q.Type != "choice" || len(q.ChoiceOptions()) != 3 {
		t.Errorf("question over the wire = %+v", q)
	}

	code, body = do(t, srv, http.MethodGet, "/api/strategy/configs", nil)
	var cfgs struct {
		Strategies []strategy.StrategyStatus `json:"strategies"`
	}
	if err := json.Unmarshal(body, &cfgs); err != nil || code != 200 || len(cfgs.Strategies) != 2 {
		t.Fatalf("configs = %d %s", code, body)
	}
	if st := cfgs.Strategies[0]; st.Config.Venue != "paper" || st.Config.Enabled || st.LastRunAt != nil {
		t.Errorf("default status = %+v", st)
	}
	// Wire shape: last_run_at null, last_action omitted before any run.
	if !strings.Contains(string(body), `"last_run_at":null`) || strings.Contains(string(body), "last_action") {
		t.Errorf("status wire shape: %s", body)
	}

	code, body = do(t, srv, http.MethodGet, "/api/strategy/configs/nope", nil)
	if code != http.StatusNotFound {
		t.Errorf("unknown id = %d %s", code, body)
	}

	// Invalid param → 400 with field.
	code, body = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{
		"venue": "paper", "params": map[string]any{"size_usd": 5},
	})
	var e struct{ Error, Field string }
	_ = json.Unmarshal(body, &e)
	if code != http.StatusBadRequest || e.Field != "params.size_usd" || e.Error == "" {
		t.Errorf("bad param = %d %s", code, body)
	}
	code, body = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{
		"venue": "paper", "params": map[string]any{"side_bias": "sideways"},
	})
	_ = json.Unmarshal(body, &e)
	if code != http.StatusBadRequest || e.Field != "params.side_bias" {
		t.Errorf("bad enum = %d %s", code, body)
	}
	code, body = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{"venue": "binance"})
	_ = json.Unmarshal(body, &e)
	if code != http.StatusBadRequest || e.Field != "venue" {
		t.Errorf("bad venue = %d %s", code, body)
	}
	code, body = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{"id": "other"})
	if code != http.StatusBadRequest {
		t.Errorf("mismatched id = %d %s", code, body)
	}

	// Valid PUT applies, echoes the status and persists via SaveConfig.
	code, body = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{
		"id": "funding_skew", "enabled": true, "venue": "paper",
		"params":   map[string]any{"size_usd": 300, "min_p": 0.7},
		"governor": map[string]any{"min_confidence": 0.65},
	})
	var st strategy.StrategyStatus
	if err := json.Unmarshal(body, &st); err != nil || code != 200 {
		t.Fatalf("put = %d %s", code, body)
	}
	if !st.Config.Enabled || st.Config.Params.Float("size_usd", 0) != 300 || st.Config.Governor.MinConfidence == nil || *st.Config.Governor.MinConfidence != 0.65 || st.NextRunAt == nil {
		t.Errorf("put status = %+v", st)
	}
	if len(saved.Strategy.Configs) != 1 || saved.Strategy.Configs[0].ID != "funding_skew" || !saved.Strategy.Configs[0].Enabled {
		t.Errorf("saved config = %+v", saved.Strategy.Configs)
	}
}

func TestStrategyRunApproveRejectDecisions(t *testing.T) {
	srv, rt, _, _ := strategyServer(t, strategy.GovernorSettings{Mode: strategy.ModeManual, MaxNotionalUSD: 1000, MaxOpenIntents: 5})

	// Dry run: intents, no verdicts.
	code, body := do(t, srv, http.MethodPost, "/api/strategy/configs/funding_skew/run", map[string]any{"dry_run": true})
	var rec strategy.DecisionRecord
	if err := json.Unmarshal(body, &rec); err != nil || code != 200 {
		t.Fatalf("dry run = %d %s", code, body)
	}
	if !rec.DryRun || len(rec.Intents) != 1 || len(rec.Verdicts) != 0 || rec.Intents[0].Action != "open_short" || rec.Intents[0].Market != "ETH" {
		t.Fatalf("dry-run record = %+v", rec)
	}
	if rec.Answers["funding_extreme"].Confidence != nil || rec.Answers["crowding"].Legend["1"].What != "somewhat crowded" {
		t.Errorf("answers over the wire = %+v", rec.Answers)
	}
	if !strings.Contains(string(body), `"target_weight":null`) || !strings.Contains(string(body), `"state_digest":"sha256:`) {
		t.Errorf("record wire shape: %s", body)
	}

	// Live run in manual mode → proposed.
	code, body = do(t, srv, http.MethodPost, "/api/strategy/configs/funding_skew/run", nil)
	if err := json.Unmarshal(body, &rec); err != nil || code != 200 {
		t.Fatalf("run = %d %s", code, body)
	}
	if rec.DryRun || len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != "proposed" {
		t.Fatalf("live record = %+v", rec)
	}
	iid := rec.Intents[0].ID

	// Decisions list newest first, filter, limit validation, get by id.
	code, body = do(t, srv, http.MethodGet, "/api/strategy/decisions?limit=1&strategy=funding_skew", nil)
	var list struct {
		Decisions []strategy.DecisionRecord `json:"decisions"`
	}
	if err := json.Unmarshal(body, &list); err != nil || code != 200 || len(list.Decisions) != 1 || list.Decisions[0].ID != rec.ID {
		t.Errorf("decisions = %d %s", code, body)
	}
	if code, _ = do(t, srv, http.MethodGet, "/api/strategy/decisions?limit=x", nil); code != http.StatusBadRequest {
		t.Errorf("bad limit = %d", code)
	}
	if code, _ = do(t, srv, http.MethodGet, "/api/strategy/decisions?strategy=regime_rotation", nil); code != 200 {
		t.Errorf("filtered = %d", code)
	}
	if code, _ = do(t, srv, http.MethodGet, "/api/strategy/decisions/"+rec.ID, nil); code != 200 {
		t.Errorf("get decision = %d", code)
	}
	if code, _ = do(t, srv, http.MethodGet, "/api/strategy/decisions/missing", nil); code != http.StatusNotFound {
		t.Errorf("missing decision = %d", code)
	}

	// Reject unknown intent → 404; approve → executed on paper.
	if code, _ = do(t, srv, http.MethodPost, "/api/strategy/decisions/"+rec.ID+"/intents/zzz/reject", nil); code != http.StatusNotFound {
		t.Errorf("reject unknown = %d", code)
	}
	code, body = do(t, srv, http.MethodPost, "/api/strategy/decisions/"+rec.ID+"/intents/"+iid+"/approve", nil)
	if err := json.Unmarshal(body, &rec); err != nil || code != 200 {
		t.Fatalf("approve = %d %s", code, body)
	}
	if v, _ := rec.LatestVerdict(iid); v.Status != "executed" || v.By != "venue" {
		t.Errorf("after approve = %+v", rec.Verdicts)
	}
	// Second approve → 409.
	if code, _ = do(t, srv, http.MethodPost, "/api/strategy/decisions/"+rec.ID+"/intents/"+iid+"/approve", nil); code != http.StatusConflict {
		t.Errorf("re-approve = %d", code)
	}
	// Venues show the paper position.
	code, body = do(t, srv, http.MethodGet, "/api/strategy/venues", nil)
	var vs struct {
		Venues []venue.VenueStatus `json:"venues"`
	}
	if err := json.Unmarshal(body, &vs); err != nil || code != 200 || len(vs.Venues) != 1 || vs.Venues[0].ID != "paper" || len(vs.Venues[0].Positions) != 1 || vs.Venues[0].Positions[0].SizeUSD >= 0 {
		t.Errorf("venues = %d %s", code, body)
	}

	// A new proposal, then reject it.
	code, body = do(t, srv, http.MethodPost, "/api/strategy/configs/regime_rotation/run", nil)
	if code != 200 {
		t.Fatalf("regime run = %d %s", code, body)
	}
	_ = json.Unmarshal(body, &rec)
	if len(rec.Intents) == 0 {
		t.Skip("regime fixture produced no intents under uniform answers")
	}
	for _, in := range rec.Intents {
		var got strategy.DecisionRecord
		code, body = do(t, srv, http.MethodPost, "/api/strategy/decisions/"+rec.ID+"/intents/"+in.ID+"/reject", nil)
		_ = json.Unmarshal(body, &got)
		if v, _ := got.LatestVerdict(in.ID); code != 200 || v.Status != "rejected" || v.By != "operator" {
			t.Errorf("reject %s = %d %+v", in.ID, code, got.Verdicts)
		}
	}
	if rt.Store().OpenCount() != 0 {
		t.Errorf("open intents = %d", rt.Store().OpenCount())
	}
}

func TestStrategyGovernorAndKill(t *testing.T) {
	srv, _, _, saved := strategyServer(t, strategy.GovernorSettings{Mode: strategy.ModeManual, MaxNotionalUSD: 1000, MaxOpenIntents: 5})

	code, body := do(t, srv, http.MethodGet, "/api/strategy/governor", nil)
	var g strategy.GovernorSettings
	if err := json.Unmarshal(body, &g); err != nil || code != 200 || g.Mode != "manual" || g.Killed {
		t.Fatalf("governor = %d %s", code, body)
	}
	if code, body = do(t, srv, http.MethodPut, "/api/strategy/governor", map[string]any{"mode": "yolo"}); code != http.StatusBadRequest {
		t.Errorf("bad mode = %d %s", code, body)
	}
	code, body = do(t, srv, http.MethodPut, "/api/strategy/governor", map[string]any{"mode": "threshold", "min_confidence": 0.5, "max_notional_usd": 800, "max_open_intents": 3})
	_ = json.Unmarshal(body, &g)
	if code != 200 || g.Mode != "threshold" || g.MinConfidence != 0.5 || g.MaxNotionalUSD != 800 || saved.Strategy.Governor.Mode != "threshold" {
		t.Errorf("put governor = %d %s (saved %+v)", code, body, saved.Strategy.Governor)
	}

	// Enable, propose (threshold 0.5 < 0.71 → approved+executed; raise it first).
	do(t, srv, http.MethodPut, "/api/strategy/governor", map[string]any{"mode": "threshold", "min_confidence": 0.9, "max_notional_usd": 800, "max_open_intents": 3})
	do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{"enabled": true, "venue": "paper"})
	code, body = do(t, srv, http.MethodPost, "/api/strategy/configs/funding_skew/run", nil)
	var rec strategy.DecisionRecord
	_ = json.Unmarshal(body, &rec)
	if code != 200 || len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != "proposed" {
		t.Fatalf("run = %d %+v", code, rec.Verdicts)
	}

	// Kill: killed=true, configs disabled, proposal rejected.
	code, body = do(t, srv, http.MethodPost, "/api/strategy/kill", nil)
	_ = json.Unmarshal(body, &g)
	if code != 200 || !g.Killed {
		t.Fatalf("kill = %d %s", code, body)
	}
	code, body = do(t, srv, http.MethodGet, "/api/strategy/configs/funding_skew", nil)
	var st strategy.StrategyStatus
	_ = json.Unmarshal(body, &st)
	if st.Config.Enabled || saved.Strategy.Configs[0].Enabled {
		t.Errorf("still enabled after kill: %+v / saved %+v", st.Config, saved.Strategy.Configs)
	}
	code, body = do(t, srv, http.MethodGet, "/api/strategy/decisions/"+rec.ID, nil)
	_ = json.Unmarshal(body, &rec)
	if v, _ := rec.LatestVerdict(rec.Intents[0].ID); v.Status != "rejected" || v.Reason != "killed" {
		t.Errorf("proposal after kill = %+v", rec.Verdicts)
	}
	// Enabling while killed → 409; un-kill via PUT governor killed=false.
	if code, _ = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{"enabled": true, "venue": "paper"}); code != http.StatusConflict {
		t.Errorf("enable while killed = %d", code)
	}
	code, body = do(t, srv, http.MethodPut, "/api/strategy/governor", map[string]any{"mode": "manual", "killed": false})
	_ = json.Unmarshal(body, &g)
	if code != 200 || g.Killed || g.Mode != "manual" {
		t.Errorf("un-kill = %d %s", code, body)
	}
	if code, _ = do(t, srv, http.MethodPut, "/api/strategy/configs/funding_skew", map[string]any{"enabled": true, "venue": "paper"}); code != 200 {
		t.Errorf("enable after un-kill = %d", code)
	}
}

// TestStrategyRun503WhenJevKeyMissing: kind=jev with no key is "decider
// unavailable" (503), and no record is written.
func TestStrategyRun503WhenJevKeyMissing(t *testing.T) {
	t.Setenv("HYPERION_TEST_JEV_KEY", "")
	pv := paper.New()
	pv.SetMarks(builtin.FixtureMarkets()...)
	store, _ := runtime.NewDecisionStore("")
	deps := testDeps(t, nil)
	rt, err := runtime.New(runtime.Config{
		Strategies: registry.All(),
		Venues:     map[string]venue.Venue{"paper": pv},
		Decider:    jev.New("http://127.0.0.1:1", "", "HYPERION_TEST_JEV_KEY"),
		Store:      store,
		Bus:        deps.Bus,
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.Strategy = rt
	srv := httptest.NewServer(NewServer(deps).Handler())
	defer srv.Close()
	code, body := do(t, srv, http.MethodPost, "/api/strategy/configs/funding_skew/run", map[string]any{"dry_run": true})
	if code != http.StatusServiceUnavailable || !strings.Contains(string(body), "decider unavailable") {
		t.Errorf("run without key = %d %s", code, body)
	}
	if len(store.List(10, "")) != 0 {
		t.Error("record written despite unavailable decider")
	}
}
