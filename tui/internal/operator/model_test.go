package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// fakeAPI is an in-memory daemon: every call is recorded and answered from
// state, so Update tests can assert on the exact request and drive the
// resulting message back through the model.
type fakeAPI struct {
	mu        sync.Mutex
	calls     []string
	statuses  []apiclient.StrategyStatus
	decisions []apiclient.DecisionRecord
	venues    []apiclient.VenueStatus
	governor  apiclient.GovernorSettings
	fail      error // when set, every call returns it
	lastCfg   apiclient.StrategyConfig
	lastGov   apiclient.GovernorSettings
}

func (f *fakeAPI) record(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeAPI) Manifests(context.Context) ([]apiclient.Manifest, error) {
	f.record("manifests")
	if f.fail != nil {
		return nil, f.fail
	}
	var out []apiclient.Manifest
	for _, s := range f.statuses {
		out = append(out, s.Manifest)
	}
	return out, nil
}
func (f *fakeAPI) StrategyConfigs(context.Context) ([]apiclient.StrategyStatus, error) {
	f.record("configs")
	if f.fail != nil {
		return nil, f.fail
	}
	return f.statuses, nil
}
func (f *fakeAPI) PutStrategyConfig(_ context.Context, cfg apiclient.StrategyConfig) (apiclient.StrategyStatus, error) {
	f.record("put " + cfg.ID)
	f.lastCfg = cfg
	if f.fail != nil {
		return apiclient.StrategyStatus{}, f.fail
	}
	for i, s := range f.statuses {
		if s.Manifest.ID == cfg.ID {
			f.statuses[i].Config = cfg
			return f.statuses[i], nil
		}
	}
	return apiclient.StrategyStatus{}, &apiclient.APIError{Status: 404, Message: "unknown strategy"}
}
func (f *fakeAPI) RunStrategy(_ context.Context, id string, dry bool) (apiclient.DecisionRecord, error) {
	f.record(fmt.Sprintf("run %s dry=%v", id, dry))
	if f.fail != nil {
		return apiclient.DecisionRecord{}, f.fail
	}
	return apiclient.DecisionRecord{ID: "run-" + id, StrategyID: id, DryRun: dry, Venue: "paper", TS: apiclient.Time{Time: time.Now()},
		Intents: []apiclient.Intent{{ID: "dry-int", Market: "ETH", Action: "open_short", SizeUSD: 250}}}, nil
}
func (f *fakeAPI) Decisions(context.Context, int, string) ([]apiclient.DecisionRecord, error) {
	f.record("decisions")
	if f.fail != nil {
		return nil, f.fail
	}
	return f.decisions, nil
}
func (f *fakeAPI) ApproveIntent(_ context.Context, d, i string) (apiclient.DecisionRecord, error) {
	f.record("approve " + d + " " + i)
	return f.verdict(d, i, "approved")
}
func (f *fakeAPI) RejectIntent(_ context.Context, d, i string) (apiclient.DecisionRecord, error) {
	f.record("reject " + d + " " + i)
	return f.verdict(d, i, "rejected")
}
func (f *fakeAPI) verdict(d, i, status string) (apiclient.DecisionRecord, error) {
	if f.fail != nil {
		return apiclient.DecisionRecord{}, f.fail
	}
	for k := range f.decisions {
		if f.decisions[k].ID == d {
			f.decisions[k].Verdicts = append(f.decisions[k].Verdicts, apiclient.IntentVerdict{IntentID: i, Status: status, By: "operator"})
			return f.decisions[k], nil
		}
	}
	return apiclient.DecisionRecord{}, &apiclient.APIError{Status: 404, Message: "unknown decision"}
}
func (f *fakeAPI) Venues(context.Context) ([]apiclient.VenueStatus, error) {
	f.record("venues")
	if f.fail != nil {
		return nil, f.fail
	}
	return f.venues, nil
}
func (f *fakeAPI) Governor(context.Context) (apiclient.GovernorSettings, error) {
	f.record("governor")
	if f.fail != nil {
		return apiclient.GovernorSettings{}, f.fail
	}
	return f.governor, nil
}
func (f *fakeAPI) PutGovernor(_ context.Context, g apiclient.GovernorSettings) (apiclient.GovernorSettings, error) {
	f.record("put governor")
	f.lastGov = g
	if f.fail != nil {
		return apiclient.GovernorSettings{}, f.fail
	}
	f.governor = g
	return g, nil
}
func (f *fakeAPI) Kill(context.Context) (apiclient.GovernorSettings, error) {
	f.record("kill")
	if f.fail != nil {
		return apiclient.GovernorSettings{}, f.fail
	}
	f.governor.Killed = true
	for i := range f.statuses {
		f.statuses[i].Config.Enabled = false
	}
	return f.governor, nil
}

// fixtures: the PROTOCOL.md shapes, hand-built.

func fixtureManifest() apiclient.Manifest {
	min, max, step := 10.0, 100000.0, 10.0
	return apiclient.Manifest{
		ID: "funding_skew", Name: "Funding skew fade", Version: "0.1.0", Description: "fade extreme funding",
		Venues: []string{"hyperliquid", "paper"}, Cadence: "5m", Markets: []string{"BTC", "ETH"},
		Params: []apiclient.ParamSpec{
			{Key: "size_usd", Type: "number", Label: "Size per intent (USD)", Default: 250.0, Min: &min, Max: &max, Step: &step, Description: "per-intent notional"},
			{Key: "min_p", Type: "number", Default: 0.8},
			{Key: "side_bias", Type: "enum", Options: []string{"both", "long_only", "short_only"}, Default: "both"},
			{Key: "aggressive", Type: "bool", Default: false},
			{Key: "note", Type: "string", Default: ""},
		},
		Questions: map[string]apiclient.Question{
			"funding_extreme": {Type: "noul", Instructions: "Funding is extreme relative to the 30-day range"},
			"direction":       {Type: "choice", Instructions: "Which side to fade", Criteria: apiclient.Criteria{Options: map[string]string{"fade_long": "…", "fade_short": "…", "none": "…"}}},
			"crowding":        {Type: "score", Instructions: "How crowded is the long side", Criteria: apiclient.Criteria{Levels: []string{"not crowded", "somewhat", "extremely"}}},
		},
	}
}

func fixtureStatus() apiclient.StrategyStatus {
	return apiclient.StrategyStatus{
		Manifest: fixtureManifest(),
		Config:   apiclient.StrategyConfig{ID: "funding_skew", Enabled: false, Venue: "paper", Params: map[string]any{"size_usd": 250.0, "min_p": 0.8}},
	}
}

func fixtureDecision() apiclient.DecisionRecord {
	conf71, conf60 := 0.71, 0.6
	return apiclient.DecisionRecord{
		ID: "dec-1", TS: apiclient.Time{Time: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)}, StrategyID: "funding_skew", Venue: "paper",
		StateDigest: "sha256:abc", State: json.RawMessage(`{"funding":0.01}`),
		Questions: fixtureManifest().Questions,
		Answers: map[string]apiclient.Answer{
			"funding_extreme": {Type: "noul", Noul: 0.93},
			"direction":       {Type: "choice", Choice: "fade_short", Probabilities: map[string]float64{"fade_long": 0.1, "fade_short": 0.82, "none": 0.08}, Confidence: &conf71},
			"crowding":        {Type: "score", Score: 1.35, Probabilities: map[string]float64{"0": 0.05, "1": 0.55, "2": 0.4}, Legend: map[string]apiclient.LegendEntry{"0": {What: "not crowded"}, "1": {What: "somewhat"}, "2": {What: "extremely"}}, Confidence: &conf60},
		},
		Intents:   []apiclient.Intent{{ID: "int-1", StrategyID: "funding_skew", Venue: "paper", Market: "ETH", Action: "open_short", SizeUSD: 250, Reason: "funding_extreme=0.93 direction=fade_short crowding=1.35", Confidence: 0.71}},
		Verdicts:  []apiclient.IntentVerdict{{IntentID: "int-1", Status: "proposed", By: "governor", Reason: "mode=manual"}},
		Model:     "jev-1.13.0",
		Usage:     apiclient.Usage{InputTokens: 412, OutputTokens: 30},
		LatencyMS: 180,
	}
}

func fixtureVenue() apiclient.VenueStatus {
	return apiclient.VenueStatus{ID: "paper", Kind: "paper", Chain: "none", Status: "connected", Capabilities: []string{"perps"},
		Positions: []apiclient.VenuePosition{{Market: "ETH", SizeUSD: -250, Entry: 3100.5, Mark: 3080.2, UPnLUSD: 1.6}}}
}

func fixtureGovernor() apiclient.GovernorSettings {
	return apiclient.GovernorSettings{Mode: "manual", MinConfidence: 0.75, MaxNotionalUSD: 1000, MaxOpenIntents: 5}
}

func newFake() *fakeAPI {
	return &fakeAPI{
		statuses:  []apiclient.StrategyStatus{fixtureStatus()},
		decisions: []apiclient.DecisionRecord{fixtureDecision()},
		venues:    []apiclient.VenueStatus{fixtureVenue()},
		governor:  fixtureGovernor(),
	}
}

// loadedModel builds a model, runs Init's snapshot synchronously and sizes it.
func loadedModel(t *testing.T, api *fakeAPI, w, h int) *Model {
	t.Helper()
	m := New(Config{API: api, CoreURL: "http://127.0.0.1:8787"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	cmd := m.fetchSnapshot()
	if cmd == nil {
		t.Fatal("fetchSnapshot returned nil")
	}
	msg := cmd()
	if snap, ok := msg.(snapshotMsg); !ok || snap.Err != nil {
		t.Fatalf("snapshot = %T %+v", msg, msg)
	}
	m.Update(msg)
	if !m.loaded {
		t.Fatal("model not loaded after snapshot")
	}
	return m
}

// press sends one key and runs any returned command synchronously, feeding
// its message back (one level, no batches).
func press(t *testing.T, m *Model, key string) tea.Msg {
	t.Helper()
	var k tea.KeyPressMsg
	switch key {
	case "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		k = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		k = tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		k = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		k = tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgdown":
		k = tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		k = tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "space":
		k = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+s":
		k = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+c":
		k = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(key)
		if len(r) != 1 {
			t.Fatalf("press: unknown key %q", key)
		}
		k = tea.KeyPressMsg{Code: r[0], Text: key}
		if r[0] >= 'A' && r[0] <= 'Z' {
			k.Mod = tea.ModShift
		}
	}
	if got := k.String(); got != key {
		t.Fatalf("press: built %q for %q", got, key)
	}
	_, cmd := m.Update(k)
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg != nil {
		if _, isBatch := msg.(tea.BatchMsg); !isBatch {
			m.Update(msg)
		}
	}
	return msg
}

func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func hasCall(api *fakeAPI, s string) bool {
	api.mu.Lock()
	defer api.mu.Unlock()
	for _, c := range api.calls {
		if c == s {
			return true
		}
	}
	return false
}

// ---- snapshot / offline ----

func TestSnapshotFailureIsOffline(t *testing.T) {
	prev := refreshTick
	refreshTick = func() tea.Cmd { return nil }
	defer func() { refreshTick = prev }()
	api := newFake()
	api.fail = errors.New("connection refused")
	m := New(Config{API: api, CoreURL: "http://127.0.0.1:8787"})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	msg := m.fetchSnapshot()()
	m.Update(msg)
	if m.loaded || m.offlineErr == "" {
		t.Fatalf("loaded=%v offlineErr=%q, want offline", m.loaded, m.offlineErr)
	}
	out := m.View().Content
	for _, want := range []string{"OFFLINE", "connection refused", "127.0.0.1:8787", "retrying"} {
		if !strings.Contains(out, want) {
			t.Errorf("offline view missing %q", want)
		}
	}
	// The retry tick re-fetches while offline; a recovered daemon loads.
	api.fail = nil
	_, cmd := m.Update(refreshTickMsg{})
	if cmd == nil {
		t.Fatal("refresh tick returned no cmd while offline")
	}
	// With the timer stubbed out tea.Batch may collapse to the single
	// snapshot cmd; accept either shape.
	switch msg := cmd().(type) {
	case snapshotMsg:
		m.Update(msg)
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if snap, ok := c().(snapshotMsg); ok {
				m.Update(snap)
			}
		}
	default:
		t.Fatalf("expected snapshot or batch, got %T", msg)
	}
	if !m.loaded || m.offlineErr != "" {
		t.Fatalf("after recovery loaded=%v offlineErr=%q", m.loaded, m.offlineErr)
	}
}

func TestConnRepairsWithSnapshot(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	before := len(api.calls)
	_, cmd := m.Update(connMsg{Connected: true})
	if cmd == nil {
		t.Fatal("connect did not trigger a snapshot")
	}
	m.Update(cmd())
	if len(api.calls) <= before || !m.connected {
		t.Errorf("calls=%v connected=%v", api.calls, m.connected)
	}
	m.Update(connMsg{Connected: false})
	if m.connected {
		t.Error("still connected after loss")
	}
	if out := m.View().Content; !strings.Contains(out, "POLLING") || !strings.Contains(out, "stale") {
		t.Errorf("view after link loss should say POLLING/stale:\n%s", out)
	}
}

func TestMergeStatusesAddsUnconfiguredManifests(t *testing.T) {
	mf2 := fixtureManifest()
	mf2.ID = "regime_rotation"
	out := mergeStatuses([]apiclient.StrategyStatus{fixtureStatus()}, []apiclient.Manifest{fixtureManifest(), mf2})
	if len(out) != 2 || out[0].Manifest.ID != "funding_skew" || out[1].Manifest.ID != "regime_rotation" {
		t.Fatalf("merged = %+v", out)
	}
	if out[1].Config.Venue != "hyperliquid" || out[1].Config.Params["size_usd"] != 250.0 || out[1].Config.Enabled {
		t.Errorf("default config = %+v", out[1].Config)
	}
}

// ---- tabs ----

func TestTabSwitching(t *testing.T) {
	m := loadedModel(t, newFake(), 100, 30)
	for _, c := range []struct {
		key  string
		want tab
	}{{"2", tabDecisions}, {"3", tabVenues}, {"4", tabGovernor}, {"1", tabStrategies}} {
		press(t, m, c.key)
		if m.tab != c.want {
			t.Errorf("after %q tab = %v, want %v", c.key, m.tab, c.want)
		}
	}
	press(t, m, "tab")
	if m.tab != tabDecisions {
		t.Errorf("tab → %v", m.tab)
	}
	press(t, m, "shift+tab")
	press(t, m, "shift+tab")
	if m.tab != tabGovernor {
		t.Errorf("shift+tab wraps → %v, want GOVERNOR", m.tab)
	}
	if msg := press(t, m, "q"); msg == nil {
		t.Error("q should quit")
	}
}

// ---- STRATEGIES ----

func TestStrategiesToggleEnabled(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "e")
	if !hasCall(api, "put funding_skew") || !api.lastCfg.Enabled || api.lastCfg.Venue != "paper" {
		t.Fatalf("toggle: calls=%v cfg=%+v", api.calls, api.lastCfg)
	}
	if !m.strategies[0].Config.Enabled || m.busy != "" {
		t.Errorf("model not updated from PUT result: enabled=%v busy=%q", m.strategies[0].Config.Enabled, m.busy)
	}
	press(t, m, "e")
	if api.lastCfg.Enabled {
		t.Error("second toggle should disable")
	}
	// A killed governor refuses local enables.
	m.governor.Killed = true
	n := len(api.calls)
	press(t, m, "e")
	if len(api.calls) != n || !strings.Contains(m.notice, "killed") {
		t.Errorf("enable under kill: calls=%v notice=%q", api.calls[n:], m.notice)
	}
}

func TestStrategiesToggleFailureNotice(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	api.fail = &apiclient.APIError{Status: 409, Message: "governor killed"}
	press(t, m, "e")
	if m.strategies[0].Config.Enabled || !strings.Contains(m.notice, "governor killed") || m.busy != "" {
		t.Errorf("failure: enabled=%v notice=%q busy=%q", m.strategies[0].Config.Enabled, m.notice, m.busy)
	}
	if out := m.View().Content; !strings.Contains(out, "governor killed") {
		t.Error("footer should show the failure notice")
	}
}

func TestStrategiesDryRunFocusesDecision(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "r")
	if !hasCall(api, "run funding_skew dry=true") {
		t.Fatalf("calls = %v", api.calls)
	}
	if m.tab != tabDecisions || m.decCursor != 0 || m.decisions[0].ID != "run-funding_skew" || !m.decisions[0].DryRun {
		t.Errorf("tab=%v cursor=%d decisions[0]=%+v", m.tab, m.decCursor, m.decisions[0])
	}
	if out := m.View().Content; !strings.Contains(out, "DRY-RUN") {
		t.Error("detail should carry the DRY-RUN badge")
	}
}

func TestStrategiesCursorAndNoOpWhenEmpty(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "down")
	press(t, m, "j")
	if m.stratCursor != 0 {
		t.Errorf("cursor overran single row: %d", m.stratCursor)
	}
	m.strategies = nil
	for _, k := range []string{"e", "enter", "r"} {
		press(t, m, k)
	}
	if m.form != nil || len(api.calls) != 5 {
		t.Errorf("empty table triggered actions: form=%v calls=%v", m.form != nil, api.calls)
	}
}

// ---- param form ----

func TestParamFormEditValidateSave(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 120, 40)
	press(t, m, "enter")
	if m.form == nil {
		t.Fatal("enter did not open the form")
	}
	out := m.View().Content
	for _, want := range []string{"PARAMS · funding_skew", "Venue", "Size per intent (USD)", "min 10 · max 100000 · step 10", "side_bias", "aggressive", "note", "ctrl+s"} {
		if !strings.Contains(out, want) {
			t.Errorf("form view missing %q", want)
		}
	}
	// Row 0 is venue: cycle to hyperliquid.
	press(t, m, "right")
	// Row 1 size_usd: type an out-of-step value, save must be refused.
	press(t, m, "down")
	m.form.fields[1].input.SetValue("")
	typeText(t, m, "255")
	press(t, m, "ctrl+s")
	if m.form == nil || m.form.fields[1].err == "" || hasCall(api, "put funding_skew") {
		t.Fatalf("invalid step accepted: form=%v err=%q calls=%v", m.form != nil, m.form.fields[1].err, api.calls)
	}
	if !strings.Contains(m.View().Content, "step 10") {
		t.Error("row error not rendered")
	}
	// Below min.
	m.form.fields[1].input.SetValue("5")
	if _, ok := m.form.validate(); ok || !strings.Contains(m.form.fields[1].err, "min") {
		t.Errorf("below-min accepted: %q", m.form.fields[1].err)
	}
	m.form.fields[1].input.SetValue("abc")
	if _, ok := m.form.validate(); ok || m.form.fields[1].err != "not a number" {
		t.Errorf("non-number accepted: %q", m.form.fields[1].err)
	}
	// Fix it, flip the enum and bool, type a string, save.
	m.form.fields[1].input.SetValue("500")
	press(t, m, "down") // min_p
	press(t, m, "down") // side_bias
	press(t, m, "space")
	press(t, m, "down") // aggressive
	press(t, m, "enter")
	press(t, m, "down") // note
	typeText(t, m, "hi")
	press(t, m, "ctrl+s")
	if m.form != nil {
		t.Fatalf("form still open after save; notice=%q", m.notice)
	}
	cfg := api.lastCfg
	if cfg.Venue != "hyperliquid" || cfg.Params["size_usd"] != 500.0 || cfg.Params["min_p"] != 0.8 ||
		cfg.Params["side_bias"] != "long_only" || cfg.Params["aggressive"] != true || cfg.Params["note"] != "hi" || cfg.Enabled {
		t.Errorf("saved config = %+v", cfg)
	}
	if m.strategies[0].Config.Venue != "hyperliquid" {
		t.Error("table not updated from PUT result")
	}
}

func TestParamFormEscCancelsAndServerFieldError(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 120, 40)
	press(t, m, "enter")
	press(t, m, "esc")
	if m.form != nil || hasCall(api, "put funding_skew") {
		t.Fatal("esc did not cancel cleanly")
	}
	press(t, m, "enter")
	api.fail = &apiclient.APIError{Status: 400, Message: "size_usd above venue cap", Field: "params.size_usd"}
	press(t, m, "ctrl+s")
	if m.form == nil {
		t.Fatal("form closed on server error")
	}
	if m.form.cursor != 1 || m.form.fields[1].err != "size_usd above venue cap" {
		t.Errorf("server field error not routed: cursor=%d err=%q", m.form.cursor, m.form.fields[1].err)
	}
	if !strings.Contains(m.View().Content, "above venue cap") {
		t.Error("server error not rendered")
	}
}

func TestParamFormPreservesUnknownParams(t *testing.T) {
	s := fixtureStatus()
	s.Config.Params["legacy_flag"] = 7.0
	f := newParamForm(s)
	cfg, ok := f.validate()
	if !ok || cfg.Params["legacy_flag"] != 7.0 || cfg.Params["side_bias"] != "both" || cfg.Params["aggressive"] != false {
		t.Errorf("validate = %+v ok=%v", cfg, ok)
	}
}

// ---- DECISIONS ----

func TestDecisionsApproveReject(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 120, 40)
	press(t, m, "2")
	press(t, m, "a")
	if !hasCall(api, "approve dec-1 int-1") {
		t.Fatalf("calls = %v", api.calls)
	}
	if v, _ := m.decisions[0].LatestVerdict("int-1"); v.Status != "approved" {
		t.Errorf("verdict after approve = %+v", v)
	}
	// Already approved → reject refused locally.
	n := len(api.calls)
	press(t, m, "x")
	if len(api.calls) != n || !strings.Contains(m.notice, "not proposed") {
		t.Errorf("reject on approved: calls=%v notice=%q", api.calls[n:], m.notice)
	}
	// Fresh proposed decision → reject.
	d := fixtureDecision()
	d.ID = "dec-2"
	d.TS = apiclient.Time{Time: d.TS.Add(time.Minute)}
	api.decisions = append(api.decisions, d)
	m.Update(decisionMsg(d))
	if m.decisions[0].ID != "dec-2" || m.decCursor != 1 {
		t.Fatalf("live prepend: ids=%v cursor=%d", []string{m.decisions[0].ID, m.decisions[1].ID}, m.decCursor)
	}
	press(t, m, "up")
	press(t, m, "x")
	if !hasCall(api, "reject dec-2 int-1") {
		t.Errorf("calls = %v", api.calls)
	}
	if out := m.View().Content; !strings.Contains(out, "REJECTED") {
		t.Error("detail should show REJECTED")
	}
}

func TestDecisionsDryRunCannotBeApproved(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 120, 40)
	press(t, m, "r")
	n := len(api.calls)
	press(t, m, "a")
	if len(api.calls) != n || !strings.Contains(m.notice, "dry-run") {
		t.Errorf("approve on dry-run: calls=%v notice=%q", api.calls[n:], m.notice)
	}
}

func TestDecisionsVerdictAndIntentCursor(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 120, 40)
	press(t, m, "2")
	m.Update(verdictMsg{DecisionID: "dec-1", Verdict: apiclient.IntentVerdict{IntentID: "int-1", Status: "executed", By: "venue"}})
	if v, _ := m.decisions[0].LatestVerdict("int-1"); v.Status != "executed" {
		t.Errorf("verdict not appended: %+v", m.decisions[0].Verdicts)
	}
	if !strings.Contains(m.View().Content, "EXECUTED") {
		t.Error("detail should show EXECUTED")
	}
	m.decisions[0].Intents = append(m.decisions[0].Intents, apiclient.Intent{ID: "int-2", Action: "open_long", Market: "BTC", SizeUSD: 100})
	press(t, m, "l")
	if m.intentCursor != 1 {
		t.Errorf("intent cursor = %d", m.intentCursor)
	}
	press(t, m, "]")
	if m.intentCursor != 1 {
		t.Errorf("intent cursor overran: %d", m.intentCursor)
	}
	press(t, m, "h")
	if m.intentCursor != 0 {
		t.Errorf("intent cursor = %d after h", m.intentCursor)
	}
	press(t, m, "pgdown")
	if m.detailScroll == 0 {
		t.Error("pgdown did not scroll")
	}
	press(t, m, "pgup")
	if m.detailScroll != 0 {
		t.Errorf("pgup scroll = %d", m.detailScroll)
	}
	if m.pendingProposals() != 0 {
		t.Errorf("pending = %d, want 0 (executed + no verdict)", m.pendingProposals())
	}
}

func TestDecisionsNoIntentNotice(t *testing.T) {
	api := newFake()
	api.decisions = nil
	m := loadedModel(t, api, 100, 30)
	press(t, m, "2")
	press(t, m, "a")
	if !strings.Contains(m.notice, "no intent") {
		t.Errorf("notice = %q", m.notice)
	}
}

// ---- VENUES ----

func TestVenuesCursorAndLiveUpdate(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "3")
	m.Update(venueMsg(apiclient.VenueStatus{ID: "hyperliquid", Kind: "perp-dex", Chain: "hyperliquid", Status: "disconnected"}))
	if len(m.venues) != 2 || m.venues[0].ID != "hyperliquid" {
		t.Fatalf("venues = %+v", m.venues)
	}
	press(t, m, "j")
	if m.venueCursor != 1 {
		t.Errorf("cursor = %d", m.venueCursor)
	}
	out := m.View().Content
	for _, want := range []string{"hyperliquid", "disconnected", "paper", "POSITIONS · paper", "SHORT", "250.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("venues view missing %q", want)
		}
	}
	press(t, m, "k")
	if m.venueCursor != 0 {
		t.Errorf("cursor = %d after k", m.venueCursor)
	}
}

// ---- GOVERNOR ----

func TestGovernorEditAndSave(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "4")
	press(t, m, "right") // manual → threshold
	if m.gov.draft.Mode != "threshold" || !m.gov.dirty {
		t.Fatalf("mode cycle: %+v", m.gov.draft)
	}
	press(t, m, "j") // min confidence
	press(t, m, "enter")
	if !m.gov.editing {
		t.Fatal("enter did not open the editor")
	}
	m.gov.input.SetValue("")
	typeText(t, m, "1.5")
	press(t, m, "enter")
	if !m.gov.editing || m.gov.err == "" {
		t.Fatalf("out-of-range confidence accepted: editing=%v err=%q", m.gov.editing, m.gov.err)
	}
	m.gov.input.SetValue("")
	typeText(t, m, "0.9")
	press(t, m, "enter")
	if m.gov.editing || m.gov.draft.MinConfidence != 0.9 {
		t.Fatalf("commit: editing=%v draft=%+v", m.gov.editing, m.gov.draft)
	}
	press(t, m, "j") // max notional
	press(t, m, "enter")
	m.gov.input.SetValue("")
	typeText(t, m, "2500")
	press(t, m, "enter")
	press(t, m, "j") // max open
	press(t, m, "enter")
	m.gov.input.SetValue("")
	typeText(t, m, "3")
	press(t, m, "enter")
	if out := m.View().Content; !strings.Contains(out, "unsaved") || !strings.Contains(out, "live: manual") {
		t.Error("dirty draft not shown against live")
	}
	press(t, m, "ctrl+s")
	if !hasCall(api, "put governor") {
		t.Fatalf("calls = %v", api.calls)
	}
	want := apiclient.GovernorSettings{Mode: "threshold", MinConfidence: 0.9, MaxNotionalUSD: 2500, MaxOpenIntents: 3}
	if api.lastGov != want {
		t.Errorf("PUT governor = %+v, want %+v", api.lastGov, want)
	}
	if m.gov.dirty || m.governor != want {
		t.Errorf("after save dirty=%v governor=%+v", m.gov.dirty, m.governor)
	}
	if out := m.View().Content; !strings.Contains(out, "THRESHOLD") {
		t.Error("header should show the new mode")
	}
	// A live governor event while clean updates the draft; while dirty it does not clobber it.
	m.Update(governorMsg(apiclient.GovernorSettings{Mode: "auto", MinConfidence: 0.5, MaxNotionalUSD: 1, MaxOpenIntents: 1}))
	if m.gov.draft.Mode != "auto" {
		t.Error("clean draft should follow live governor")
	}
	press(t, m, "up")
	press(t, m, "up")
	press(t, m, "up")
	press(t, m, "left")
	m.Update(governorMsg(apiclient.GovernorSettings{Mode: "manual"}))
	if m.gov.draft.Mode != "threshold" {
		t.Errorf("dirty draft clobbered: %+v", m.gov.draft)
	}
	press(t, m, "esc")
	if m.gov.dirty || m.gov.draft.Mode != "manual" {
		t.Errorf("esc should discard the draft: %+v", m.gov.draft)
	}
}

func TestGovernorKillRequiresTypedConfirmation(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	press(t, m, "4")
	press(t, m, "K")
	if !m.kill.open {
		t.Fatal("K did not open the kill prompt")
	}
	if out := m.View().Content; !strings.Contains(out, "KILL SWITCH") || !strings.Contains(out, "Type KILL") {
		t.Errorf("kill prompt not rendered:\n%s", out)
	}
	typeText(t, m, "kill")
	press(t, m, "enter")
	if hasCall(api, "kill") || m.kill.open {
		t.Fatalf("lowercase confirmation accepted: calls=%v", api.calls)
	}
	press(t, m, "K")
	typeText(t, m, "KIL")
	press(t, m, "esc")
	if hasCall(api, "kill") || m.kill.open {
		t.Fatal("esc did not cancel")
	}
	press(t, m, "K")
	typeText(t, m, "KILL")
	press(t, m, "enter")
	if !hasCall(api, "kill") || !m.governor.Killed {
		t.Fatalf("kill not sent: calls=%v governor=%+v", api.calls, m.governor)
	}
	out := m.View().Content
	if strings.Count(out, "KILLED") < 2 {
		t.Errorf("killed state should be in the banner and the governor panel:\n%s", out)
	}
	press(t, m, "1")
	if !strings.Contains(m.View().Content, "KILLED") {
		t.Error("killed banner should persist across tabs")
	}
}

func TestBusyGuardSerialisesOps(t *testing.T) {
	api := newFake()
	m := loadedModel(t, api, 100, 30)
	m.busy = "dry-run funding_skew"
	n := len(api.calls)
	press(t, m, "e")
	if len(api.calls) != n || !strings.Contains(m.notice, "busy") {
		t.Errorf("busy guard: calls=%v notice=%q", api.calls[n:], m.notice)
	}
}

func TestNoAPINotice(t *testing.T) {
	m := New(Config{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.loaded = true
	m.strategies = []apiclient.StrategyStatus{fixtureStatus()}
	press(t, m, "e")
	if !strings.Contains(m.notice, "no daemon") {
		t.Errorf("notice = %q", m.notice)
	}
}

// ---- bridge ----

func TestFrameToMsg(t *testing.T) {
	cases := []struct {
		frame string
		want  string
	}{
		{`{"type":"strategy.decision","data":{"id":"d1","strategy_id":"s"}}`, "operator.decisionMsg"},
		{`{"type":"strategy.verdict","data":{"decision_id":"d1","verdict":{"intent_id":"i","status":"approved"}}}`, "operator.verdictMsg"},
		{`{"topic":"strategy.config","data":{"manifest":{"id":"s"},"config":{"id":"s"}}}`, "operator.configMsg"},
		{`{"type":"strategy.governor","data":{"mode":"auto"}}`, "operator.governorMsg"},
		{`{"type":"strategy.venue","data":{"id":"paper"}}`, "operator.venueMsg"},
		{`{"topic":"bar","data":{}}`, "<nil>"},
		{`{"topic":"status","data":{"Connected":true}}`, "<nil>"},
		{`garbage`, "<nil>"},
		{`{"type":"strategy.decision","data":"bad"}`, "<nil>"},
	}
	for _, c := range cases {
		got := fmt.Sprintf("%T", frameToMsg([]byte(c.frame)))
		if got != c.want {
			t.Errorf("frameToMsg(%s) = %s, want %s", c.frame, got, c.want)
		}
	}
	if got := wsURLFrom("https://core.example:8787"); got != "wss://core.example:8787/api/ws" {
		t.Errorf("wsURLFrom = %q", got)
	}
	if nextBackoff(time.Second, 0, 30*time.Second) != 2*time.Second || nextBackoff(16*time.Second, 5*time.Second, 30*time.Second) != time.Second {
		t.Error("nextBackoff")
	}
}
