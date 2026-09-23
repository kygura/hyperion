package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/fake"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
	"github.com/hyperagent/hyperagent/internal/strategy/venue/paper"
)

// testStrategy emits one open_short ETH intent when "go" >= 0.5.
type testStrategy struct {
	snapshotErr error
}

var testManifest = strategy.Manifest{
	ID: "t", Name: "Test", Version: "0", Venues: []string{"paper"}, Cadence: "1m", Markets: []string{"ETH"},
	Params:    []strategy.ParamSpec{{Key: "size_usd", Type: "number", Default: 100.0, Min: strategy.F(1), Max: strategy.F(1000)}},
	Questions: map[string]decider.Question{"go": decider.Noul("go?")},
}

func (s *testStrategy) Manifest() strategy.Manifest { return testManifest }
func (s *testStrategy) Snapshot(ctx context.Context, v venue.Venue, p strategy.Params) (strategy.State, error) {
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	ms, err := v.Markets(ctx, p.Markets(testManifest))
	if err != nil {
		return nil, err
	}
	return map[string]any{"eth_mark": ms[0].Mark}, nil
}
func (s *testStrategy) Decide(_ strategy.State, a strategy.Answers, p strategy.Params) []strategy.Intent {
	if a["go"].NoulValue() < 0.5 {
		return nil
	}
	return []strategy.Intent{{Market: "ETH", Action: strategy.ActionOpenShort, SizeUSD: p.Float("size_usd", 0), Reason: "go", Confidence: a["go"].NoulValue()}}
}

// clock is a mutex-guarded fake time source shared by the runner, the paper
// venue and the test (which advances it while the scheduler goroutine runs).
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type harness struct {
	r     *Runner
	d     *fake.Decider
	pv    *paper.Venue
	b     *bus.Bus
	evs   <-chan bus.StrategyEvent
	clock *clock
}

func newHarness(t *testing.T, gov strategy.GovernorSettings, cfgs ...strategy.StrategyConfig) *harness {
	t.Helper()
	clk := &clock{t: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	pv := paper.New(paper.WithSlippageBps(0), paper.WithClock(clk.Now))
	pv.SetMarks(venue.Market{Symbol: "ETH", Mark: 3000})
	d := fake.New(map[string]decider.Answer{"go": fake.NoulAnswer(0.9)})
	store, err := NewDecisionStore("")
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	evs := b.SubscribeStrategy(64)
	r, err := New(Config{
		Strategies: map[string]strategy.Strategy{"t": &testStrategy{}},
		Venues:     map[string]venue.Venue{"paper": pv},
		Decider:    d,
		Governor:   NewGovernor(gov, store.OpenCount),
		Store:      store,
		Bus:        b,
		Configs:    cfgs,
		Now:        clk.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{r: r, d: d, pv: pv, b: b, evs: evs, clock: clk}
}

func (h *harness) drain(t *testing.T) []bus.StrategyEvent {
	t.Helper()
	var out []bus.StrategyEvent
	for {
		select {
		case e := <-h.evs:
			out = append(out, e)
		case <-time.After(20 * time.Millisecond):
			return out
		}
	}
}

func types(evs []bus.StrategyEvent) string {
	var ts []string
	for _, e := range evs {
		ts = append(ts, e.Type)
	}
	return strings.Join(ts, ",")
}

func TestDryRunNeverTouchesGovernorOrVenue(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeAuto})
	rec, err := h.r.RunOnce(context.Background(), "t", true)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.DryRun || len(rec.Intents) != 1 || len(rec.Verdicts) != 0 || rec.Error != "" {
		t.Fatalf("dry-run record = %+v", rec)
	}
	if rec.Model != fake.Model || rec.StateDigest == "" || !strings.HasPrefix(rec.StateDigest, "sha256:") {
		t.Errorf("model/digest = %q %q", rec.Model, rec.StateDigest)
	}
	if rec.Intents[0].ID == "" || rec.Intents[0].StrategyID != "t" || rec.Intents[0].Venue != "paper" {
		t.Errorf("intent not stamped: %+v", rec.Intents[0])
	}
	if fills := h.pv.Fills(); len(fills) != 0 {
		t.Errorf("dry run placed orders: %+v", fills)
	}
	if got, ok := h.r.Store().Get(rec.ID); !ok || got.ID != rec.ID {
		t.Error("dry-run record not stored")
	}
	st, _ := h.r.Status("t")
	if st.LastDecisionID != rec.ID || st.LastAction != "open_short ETH" || st.LastRunAt == nil {
		t.Errorf("status = %+v", st)
	}
	if ts := types(h.drain(t)); ts != "strategy.decision,strategy.config" {
		t.Errorf("events = %s", ts)
	}
	// JSON shape: dry-run record has empty (not null) verdicts.
	b, _ := json.Marshal(rec)
	if !strings.Contains(string(b), `"verdicts":[]`) || !strings.Contains(string(b), `"dry_run":true`) {
		t.Errorf("json = %s", b)
	}
}

func TestManualModeProposesThenApproveExecutes(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeManual, MaxNotionalUSD: 1000, MaxOpenIntents: 5})
	rec, err := h.r.RunOnce(context.Background(), "t", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != strategy.StatusProposed || rec.Verdicts[0].By != strategy.ByGovernor || rec.Verdicts[0].Reason != "mode=manual" {
		t.Fatalf("verdicts = %+v", rec.Verdicts)
	}
	if h.r.Store().OpenCount() != 1 {
		t.Errorf("open intents = %d, want 1", h.r.Store().OpenCount())
	}
	h.drain(t)
	iid := rec.Intents[0].ID
	// Reject of an unknown intent → not found; approve → executed on paper.
	if _, err := h.r.Reject(rec.ID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("reject unknown = %v", err)
	}
	got, err := h.r.Approve(context.Background(), rec.ID, iid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Verdicts) != 3 || got.Verdicts[1].Status != strategy.StatusApproved || got.Verdicts[1].By != strategy.ByOperator || got.Verdicts[2].Status != strategy.StatusExecuted || got.Verdicts[2].By != strategy.ByVenue {
		t.Fatalf("verdicts after approve = %+v", got.Verdicts)
	}
	if fills := h.pv.Fills(); len(fills) != 1 || fills[0].Side != venue.SideSell || fills[0].SizeUSD != 100 {
		t.Errorf("fills = %+v", fills)
	}
	if ts := types(h.drain(t)); ts != "strategy.verdict,strategy.verdict" {
		t.Errorf("events = %s", ts)
	}
	// Approving again is a conflict (no longer proposed).
	if _, err := h.r.Approve(context.Background(), rec.ID, iid); !errors.Is(err, ErrConflict) {
		t.Errorf("second approve = %v, want conflict", err)
	}
	if h.r.Store().OpenCount() != 0 {
		t.Errorf("open intents = %d, want 0", h.r.Store().OpenCount())
	}
}

func TestThresholdAndAutoAndGates(t *testing.T) {
	// threshold: 0.9 >= 0.75 → approved+executed automatically.
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeThreshold, MinConfidence: 0.75, MaxNotionalUSD: 1000, MaxOpenIntents: 5})
	rec, _ := h.r.RunOnce(context.Background(), "t", false)
	if len(rec.Verdicts) != 2 || rec.Verdicts[0].Status != strategy.StatusApproved || rec.Verdicts[1].Status != strategy.StatusExecuted {
		t.Fatalf("threshold verdicts = %+v", rec.Verdicts)
	}
	if len(h.pv.Fills()) != 1 {
		t.Errorf("fills = %+v", h.pv.Fills())
	}
	// Per-strategy override raises the bar → proposed.
	hi := 0.95
	if _, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Enabled: false, Venue: "paper", Params: strategy.Params{}, Governor: strategy.GovernorOverride{MinConfidence: &hi}}); err != nil {
		t.Fatal(err)
	}
	rec, _ = h.r.RunOnce(context.Background(), "t", false)
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != strategy.StatusProposed {
		t.Fatalf("override verdicts = %+v", rec.Verdicts)
	}
	// Notional gate.
	if _, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Venue: "paper", Params: strategy.Params{"size_usd": 900.0}}); err != nil {
		t.Fatal(err)
	}
	h.r.Governor().Update(strategy.GovernorSettings{Mode: strategy.ModeAuto, MaxNotionalUSD: 500, MaxOpenIntents: 5})
	rec, _ = h.r.RunOnce(context.Background(), "t", false)
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != strategy.StatusGated || rec.Verdicts[0].By != strategy.ByGate {
		t.Fatalf("notional gate verdicts = %+v", rec.Verdicts)
	}
	// Open-intents gate: one proposal already open (from the override run) → max 1 blocks.
	h.r.Governor().Update(strategy.GovernorSettings{Mode: strategy.ModeAuto, MaxNotionalUSD: 5000, MaxOpenIntents: 1})
	rec, _ = h.r.RunOnce(context.Background(), "t", false)
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != strategy.StatusGated || !strings.Contains(rec.Verdicts[0].Reason, "open intents") {
		t.Fatalf("open gate verdicts = %+v", rec.Verdicts)
	}
}

func TestKillDisablesRejectsAndBlocks(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeManual}, strategy.StrategyConfig{ID: "t", Enabled: true, Venue: "paper"})
	rec, _ := h.r.RunOnce(context.Background(), "t", false)
	h.drain(t)
	settings := h.r.Kill()
	if !settings.Killed {
		t.Fatal("killed flag not set")
	}
	st, _ := h.r.Status("t")
	if st.Config.Enabled || st.NextRunAt != nil {
		t.Errorf("strategy still enabled after kill: %+v", st)
	}
	got, _ := h.r.Store().Get(rec.ID)
	if vd, _ := got.LatestVerdict(rec.Intents[0].ID); vd.Status != strategy.StatusRejected || vd.Reason != "killed" {
		t.Errorf("open proposal after kill = %+v", vd)
	}
	if ts := types(h.drain(t)); ts != "strategy.config,strategy.verdict,strategy.governor" {
		t.Errorf("events = %s", ts)
	}
	// Enabling while killed is a conflict; new intents are rejected.
	if _, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Enabled: true, Venue: "paper"}); !errors.Is(err, ErrConflict) {
		t.Errorf("enable while killed = %v", err)
	}
	rec, _ = h.r.RunOnce(context.Background(), "t", false)
	if rec.Verdicts[0].Status != strategy.StatusRejected {
		t.Errorf("verdict while killed = %+v", rec.Verdicts[0])
	}
	// Un-kill via UpdateGovernor(killed=false); strategy stays disabled.
	s := settings
	s.Killed = false
	if out, err := h.r.UpdateGovernor(s); err != nil || out.Killed {
		t.Errorf("revive = %+v, %v", out, err)
	}
	if st, _ := h.r.Status("t"); st.Config.Enabled {
		t.Error("strategy should stay disabled after revive")
	}
}

func TestErrorsLandInRecord(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeAuto})
	h.d.Err = errors.New("boom")
	rec, err := h.r.RunOnce(context.Background(), "t", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.Error, "decider: boom") || len(rec.Intents) != 0 || len(rec.Verdicts) != 0 {
		t.Errorf("record = %+v", rec)
	}
	st, _ := h.r.Status("t")
	if st.LastError != rec.Error {
		t.Errorf("last_error = %q", st.LastError)
	}
	if _, err := h.r.RunOnce(context.Background(), "missing", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown strategy = %v", err)
	}
}

type unavailable struct{}

func (unavailable) Evaluate(context.Context, any, map[string]decider.Question) (decider.Result, error) {
	return decider.Result{}, decider.ErrUnavailable
}
func (unavailable) Available() error { return decider.ErrUnavailable }

func TestDeciderUnavailableIsReturnedNotRecorded(t *testing.T) {
	store, _ := NewDecisionStore("")
	r, err := New(Config{Strategies: map[string]strategy.Strategy{"t": &testStrategy{}}, Venues: map[string]venue.Venue{"paper": paper.New()}, Decider: unavailable{}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunOnce(context.Background(), "t", true); !errors.Is(err, decider.ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	if len(store.List(10, "")) != 0 {
		t.Error("unavailable decider must not produce a record")
	}
}

func TestSetConfigValidation(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeManual})
	_, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Venue: "paper", Params: strategy.Params{"size_usd": 5000.0}})
	var ve *strategy.ValidationError
	if !errors.As(err, &ve) || ve.Field != "params.size_usd" {
		t.Errorf("err = %v", err)
	}
	if _, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Venue: "mars"}); !errors.As(err, &ve) || ve.Field != "venue" {
		t.Errorf("venue err = %v", err)
	}
	if _, err := h.r.SetConfig(strategy.StrategyConfig{ID: "zzz"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id = %v", err)
	}
	st, err := h.r.SetConfig(strategy.StrategyConfig{ID: "t", Enabled: true, Venue: "paper", Params: strategy.Params{"size_usd": 50.0, "markets": []any{"ETH"}}})
	if err != nil || !st.Config.Enabled || st.NextRunAt == nil {
		t.Errorf("status = %+v, %v", st, err)
	}
}

func TestSchedulerTicksEnabledStrategy(t *testing.T) {
	h := newHarness(t, strategy.GovernorSettings{Mode: strategy.ModeManual}, strategy.StrategyConfig{ID: "t", Enabled: true, Venue: "paper"})
	h.r.interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go h.r.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && h.d.Calls() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	first := h.d.Calls()
	if first < 1 {
		cancel()
		t.Fatal("scheduler never ticked")
	}
	// Cadence 1m on a frozen clock: no second tick until the clock moves.
	time.Sleep(30 * time.Millisecond)
	if h.d.Calls() != first {
		t.Errorf("ticked again before cadence elapsed: %d → %d", first, h.d.Calls())
	}
	h.clock.Advance(2 * time.Minute)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && h.d.Calls() == first {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if h.d.Calls() == first {
		t.Error("no tick after cadence elapsed")
	}
}

func TestIntentToOrder(t *testing.T) {
	pos := []venue.Position{{Market: "ETH", SizeUSD: -300}, {Market: "BTC", SizeUSD: 500}}
	cases := []struct {
		in       strategy.Intent
		side     string
		reduce   bool
		size     float64
		wantErr  bool
		describe string
	}{
		{strategy.Intent{Market: "SOL", Action: strategy.ActionOpenLong, SizeUSD: 10}, venue.SideBuy, false, 10, false, "open_long"},
		{strategy.Intent{Market: "ETH", Action: strategy.ActionClose}, venue.SideBuy, true, 300, false, "close short → buy full size"},
		{strategy.Intent{Market: "BTC", Action: strategy.ActionClose, SizeUSD: 100}, venue.SideSell, true, 100, false, "close long partial"},
		{strategy.Intent{Market: "SOL", Action: strategy.ActionClose}, "", false, 0, true, "close flat"},
		{strategy.Intent{Market: "BTC", Action: strategy.ActionScale, SizeUSD: 50}, venue.SideBuy, false, 50, false, "scale long"},
		{strategy.Intent{Market: "BTC", Action: strategy.ActionRebalance, SizeUSD: 200, Side: venue.SideSell}, venue.SideSell, true, 200, false, "rebalance trim"},
		{strategy.Intent{Market: "BTC", Action: strategy.ActionRebalance, SizeUSD: 200}, "", false, 0, true, "rebalance without side"},
		{strategy.Intent{Market: "BTC", Action: strategy.ActionHold}, "", false, 0, true, "hold"},
	}
	for _, c := range cases {
		o, err := IntentToOrder(c.in, pos)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v", c.describe, err)
			continue
		}
		if err == nil && (o.Side != c.side || o.ReduceOnly != c.reduce || o.SizeUSD != c.size) {
			t.Errorf("%s: order = %+v", c.describe, o)
		}
	}
}

func TestGovernorReview(t *testing.T) {
	open := 0
	g := NewGovernor(strategy.GovernorSettings{Mode: strategy.ModeThreshold, MinConfidence: 0.7, MaxNotionalUSD: 100, MaxOpenIntents: 2}, func() int { return open })
	in := strategy.Intent{ID: "i", Action: strategy.ActionOpenLong, SizeUSD: 50, Confidence: 0.8}
	if v := g.Review(in, strategy.GovernorOverride{}); v.Status != strategy.StatusApproved {
		t.Errorf("threshold approve = %+v", v)
	}
	in.Confidence = 0.6
	if v := g.Review(in, strategy.GovernorOverride{}); v.Status != strategy.StatusProposed {
		t.Errorf("threshold propose = %+v", v)
	}
	auto := strategy.ModeAuto
	if v := g.Review(in, strategy.GovernorOverride{Mode: &auto}); v.Status != strategy.StatusApproved {
		t.Errorf("override auto = %+v", v)
	}
	in.SizeUSD = 150
	if v := g.Review(in, strategy.GovernorOverride{}); v.Status != strategy.StatusGated || v.By != strategy.ByGate {
		t.Errorf("notional = %+v", v)
	}
	in.SizeUSD = 50
	open = 2
	if v := g.Review(in, strategy.GovernorOverride{}); v.Status != strategy.StatusGated {
		t.Errorf("open intents = %+v", v)
	}
	open = 0
	g.Kill()
	if v := g.Review(in, strategy.GovernorOverride{}); v.Status != strategy.StatusRejected || v.Reason != "killed" {
		t.Errorf("killed = %+v", v)
	}
	if _, err := g.Update(strategy.GovernorSettings{Mode: "yolo"}); err == nil {
		t.Error("bad mode accepted")
	}
	if s, _ := g.Update(strategy.GovernorSettings{Mode: strategy.ModeManual}); !s.Killed {
		t.Error("Update must not clear killed")
	}
}

func TestStorePersistsAndReloads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "decisions")
	s, err := NewDecisionStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	rec, _ := s.Append(strategy.DecisionRecord{StrategyID: "a", Intents: []strategy.Intent{{ID: "i1", Action: "open_long"}}})
	if rec.ID == "" || rec.TS != now {
		t.Fatalf("append = %+v", rec)
	}
	s.Append(strategy.DecisionRecord{StrategyID: "b"})
	if _, err := s.AppendVerdict(rec.ID, strategy.Verdict{IntentID: "i1", Status: strategy.StatusProposed, By: "governor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendVerdict(rec.ID, strategy.Verdict{IntentID: "nope"}); err == nil {
		t.Error("verdict for unknown intent accepted")
	}
	if l := s.List(10, ""); len(l) != 2 || l[0].StrategyID != "b" {
		t.Errorf("list = %+v", l)
	}
	if l := s.List(10, "a"); len(l) != 1 || len(l[0].Verdicts) != 1 {
		t.Errorf("filtered list = %+v", l)
	}
	if s.OpenCount() != 1 {
		t.Errorf("open = %d", s.OpenCount())
	}
	// File exists, one line per write (2 appends + 1 verdict re-encode).
	raw, err := os.ReadFile(filepath.Join(dir, "2026-09-23.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "\n"); n != 3 {
		t.Errorf("ndjson lines = %d, want 3", n)
	}
	// Reload: latest copy per id, verdict retained.
	s2 := &DecisionStore{dir: dir, now: s.now, index: map[string]int{}}
	if err := s2.reload(); err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(rec.ID)
	if !ok || len(got.Verdicts) != 1 || len(s2.List(10, "")) != 2 {
		t.Errorf("reloaded = %+v (ok %v)", got, ok)
	}
	// IDs sort by time.
	later := NewID(now.Add(time.Second))
	if !(later > rec.ID) {
		t.Errorf("ids not time-sortable: %s !> %s", later, rec.ID)
	}
}
