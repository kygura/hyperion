// Package runtime is the strategy loop: Runner ticks each enabled strategy
// on its cadence (snapshot → decider → decide → governor → venue), the
// Governor is the human-agency gate, and the DecisionStore is the record.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// Sentinel errors the API maps to status codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Event types published on the bus (PROTOCOL.md "WebSocket").
const (
	EventDecision = "strategy.decision"
	EventVerdict  = "strategy.verdict"
	EventConfig   = "strategy.config"
	EventGovernor = "strategy.governor"
	EventVenue    = "strategy.venue"
)

// VerdictEvent is the strategy.verdict payload.
type VerdictEvent struct {
	DecisionID string           `json:"decision_id"`
	Verdict    strategy.Verdict `json:"verdict"`
}

// Config wires a Runner.
type Config struct {
	Strategies map[string]strategy.Strategy
	Venues     map[string]venue.Venue
	Decider    decider.Decider
	Governor   *Governor
	Store      *DecisionStore
	Bus        *bus.Bus         // nil: no events
	Journal    *journal.Journal // nil: no journal lines
	Configs    []strategy.StrategyConfig
	// OnConfig is called after a config change is applied (persistence hook).
	OnConfig func(strategy.StrategyConfig)
	// Now and TickInterval are injectable for tests.
	Now          func() time.Time
	TickInterval time.Duration
}

type entry struct {
	strat   strategy.Strategy
	man     strategy.Manifest
	cfg     strategy.StrategyConfig
	lastRun *time.Time
	lastID  string
	lastAct string
	lastErr string
	nextRun *time.Time
	tickMu  sync.Mutex // serialises ticks of one strategy
}

// Runner owns the strategy entries and the scheduler loop.
type Runner struct {
	venues   map[string]venue.Venue
	decider  decider.Decider
	gov      *Governor
	store    *DecisionStore
	bus      *bus.Bus
	journal  *journal.Journal
	onConfig func(strategy.StrategyConfig)
	now      func() time.Time
	interval time.Duration

	mu      sync.RWMutex
	entries map[string]*entry
}

// New builds a Runner: one entry per strategy, with the matching config from
// cfg.Configs or a disabled default (paper when supported).
func New(cfg Config) (*Runner, error) {
	if cfg.Decider == nil {
		return nil, errors.New("runtime: decider is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("runtime: store is required")
	}
	if cfg.Governor == nil {
		cfg.Governor = NewGovernor(strategy.GovernorSettings{Mode: strategy.ModeManual}, cfg.Store.OpenCount)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Second
	}
	r := &Runner{
		venues:   cfg.Venues,
		decider:  cfg.Decider,
		gov:      cfg.Governor,
		store:    cfg.Store,
		bus:      cfg.Bus,
		journal:  cfg.Journal,
		onConfig: cfg.OnConfig,
		now:      cfg.Now,
		interval: cfg.TickInterval,
		entries:  make(map[string]*entry, len(cfg.Strategies)),
	}
	if r.venues == nil {
		r.venues = map[string]venue.Venue{}
	}
	byID := map[string]strategy.StrategyConfig{}
	for _, c := range cfg.Configs {
		byID[c.ID] = c
	}
	for id, s := range cfg.Strategies {
		man := s.Manifest()
		if man.ID == "" {
			man.ID = id
		}
		e := &entry{strat: s, man: man, cfg: r.defaultConfig(man)}
		if c, ok := byID[id]; ok {
			c.ID = id
			if c.Venue == "" {
				c.Venue = e.cfg.Venue
			}
			if c.Params == nil {
				c.Params = strategy.Params{}
			}
			if err := c.Validate(man, r.venueIDs()); err != nil {
				return nil, fmt.Errorf("runtime: config %s: %w", id, err)
			}
			e.cfg = c
		}
		if e.cfg.Enabled {
			t := r.now()
			e.nextRun = &t
		}
		r.entries[id] = e
	}
	return r, nil
}

func (r *Runner) venueIDs() []string {
	ids := make([]string, 0, len(r.venues))
	for id := range r.venues {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// defaultConfig: disabled, paper when the manifest supports it and it is
// wired, else the first supported wired venue, else the manifest's first.
func (r *Runner) defaultConfig(m strategy.Manifest) strategy.StrategyConfig {
	v := ""
	if _, ok := r.venues["paper"]; ok && m.SupportsVenue("paper") {
		v = "paper"
	} else {
		for _, id := range m.Venues {
			if _, ok := r.venues[id]; ok {
				v = id
				break
			}
		}
	}
	if v == "" && len(m.Venues) > 0 {
		v = m.Venues[0]
	}
	return strategy.StrategyConfig{ID: m.ID, Enabled: false, Venue: v, Params: strategy.Params{}}
}

// Governor returns the governor.
func (r *Runner) Governor() *Governor { return r.gov }

// Store returns the decision store.
func (r *Runner) Store() *DecisionStore { return r.store }

// Manifests lists every strategy's manifest, sorted by id.
func (r *Runner) Manifests() []strategy.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]strategy.Manifest, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.man)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Statuses lists every strategy's status, sorted by id.
func (r *Runner) Statuses() []strategy.StrategyStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]strategy.StrategyStatus, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.status())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

// Status returns one strategy's status.
func (r *Runner) Status(id string) (strategy.StrategyStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return strategy.StrategyStatus{}, fmt.Errorf("strategy %q: %w", id, ErrNotFound)
	}
	return e.status(), nil
}

func (e *entry) status() strategy.StrategyStatus {
	cfg := e.cfg
	cfg.Params = cfg.Params.Clone()
	return strategy.StrategyStatus{
		Manifest:       e.man,
		Config:         cfg,
		LastRunAt:      e.lastRun,
		LastDecisionID: e.lastID,
		LastAction:     e.lastAct,
		LastError:      e.lastErr,
		NextRunAt:      e.nextRun,
	}
}

// SetConfig validates and applies an operator config change. Enabling a
// strategy while the governor is killed is a conflict (revive first).
func (r *Runner) SetConfig(cfg strategy.StrategyConfig) (strategy.StrategyStatus, error) {
	r.mu.Lock()
	e, ok := r.entries[cfg.ID]
	if !ok {
		r.mu.Unlock()
		return strategy.StrategyStatus{}, fmt.Errorf("strategy %q: %w", cfg.ID, ErrNotFound)
	}
	if cfg.Venue == "" {
		cfg.Venue = e.cfg.Venue
	}
	if cfg.Params == nil {
		cfg.Params = strategy.Params{}
	}
	if err := cfg.Validate(e.man, r.venueIDs()); err != nil {
		r.mu.Unlock()
		return strategy.StrategyStatus{}, err
	}
	if cfg.Enabled && r.gov.Killed() {
		r.mu.Unlock()
		return strategy.StrategyStatus{}, fmt.Errorf("%w: governor is killed; revive it before enabling strategies", ErrConflict)
	}
	wasEnabled := e.cfg.Enabled
	e.cfg = cfg
	if cfg.Enabled && !wasEnabled {
		t := r.now()
		e.nextRun = &t
	} else if !cfg.Enabled {
		e.nextRun = nil
	}
	st := e.status()
	r.mu.Unlock()
	if r.onConfig != nil {
		r.onConfig(cfg)
	}
	r.publish(EventConfig, st)
	return st, nil
}

// UpdateGovernor applies operator settings. killed=false on a killed
// governor revives it; killed=true kills without touching configs (use
// Kill for the full stop).
func (r *Runner) UpdateGovernor(s strategy.GovernorSettings) (strategy.GovernorSettings, error) {
	wasKilled := r.gov.Killed()
	out, err := r.gov.Update(s)
	if err != nil {
		return out, err
	}
	switch {
	case wasKilled && !s.Killed:
		out = r.gov.Revive()
	case !wasKilled && s.Killed:
		out = r.gov.Kill()
	}
	r.publish(EventGovernor, out)
	return out, nil
}

// Kill is the kill switch: governor killed, every strategy disabled, every
// open proposal/approval rejected.
func (r *Runner) Kill() strategy.GovernorSettings {
	settings := r.gov.Kill()
	r.mu.Lock()
	var changed []strategy.StrategyStatus
	for _, e := range r.entries {
		if e.cfg.Enabled {
			e.cfg.Enabled = false
			e.nextRun = nil
			changed = append(changed, e.status())
		}
	}
	r.mu.Unlock()
	for _, st := range changed {
		if r.onConfig != nil {
			r.onConfig(st.Config)
		}
		r.publish(EventConfig, st)
	}
	for _, o := range r.store.Open() {
		r.appendVerdict(o.DecisionID, strategy.Verdict{
			IntentID: o.Intent.ID, Status: strategy.StatusRejected, By: strategy.ByGovernor, Reason: "killed",
		})
	}
	r.publish(EventGovernor, settings)
	return settings
}

// Venues reports every wired venue's status, sorted by id.
func (r *Runner) Venues(ctx context.Context) []venue.VenueStatus {
	out := make([]venue.VenueStatus, 0, len(r.venues))
	for _, id := range r.venueIDs() {
		v := r.venues[id]
		st, err := v.Status(ctx)
		if err != nil {
			st = venue.VenueStatus{ID: id, Status: "disconnected", Error: err.Error(), Capabilities: v.Capabilities(), Positions: []venue.Position{}}
		}
		if st.Positions == nil {
			st.Positions = []venue.Position{}
		}
		if st.Capabilities == nil {
			st.Capabilities = []string{}
		}
		out = append(out, st)
	}
	return out
}

// Run is the scheduler loop: every TickInterval, tick each enabled strategy
// whose next run is due. Ticks run in their own goroutine so one slow venue
// never delays another strategy; a strategy never overlaps with itself.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.runDue(ctx)
		}
	}
}

func (r *Runner) runDue(ctx context.Context) {
	now := r.now()
	r.mu.RLock()
	var due []string
	for id, e := range r.entries {
		if e.cfg.Enabled && e.nextRun != nil && !now.Before(*e.nextRun) {
			due = append(due, id)
		}
	}
	r.mu.RUnlock()
	for _, id := range due {
		go func(id string) {
			if _, err := r.RunOnce(ctx, id, false); err != nil {
				log.Printf("strategy %s: %v", id, err)
			}
		}(id)
	}
}

// RunOnce executes one tick now. dryRun skips the governor and the venue
// entirely: the record carries intents with no verdicts and dry_run=true.
// Errors returned (not recorded): unknown strategy, unwired venue, decider
// unavailable. Snapshot/decider failures land in the record's error field.
func (r *Runner) RunOnce(ctx context.Context, id string, dryRun bool) (strategy.DecisionRecord, error) {
	r.mu.RLock()
	e, ok := r.entries[id]
	r.mu.RUnlock()
	if !ok {
		return strategy.DecisionRecord{}, fmt.Errorf("strategy %q: %w", id, ErrNotFound)
	}
	if a, ok := r.decider.(decider.Availability); ok {
		if err := a.Available(); err != nil {
			return strategy.DecisionRecord{}, err
		}
	}
	e.tickMu.Lock()
	defer e.tickMu.Unlock()

	r.mu.RLock()
	cfg := e.cfg
	cfg.Params = cfg.Params.Clone()
	man := e.man
	r.mu.RUnlock()
	v, ok := r.venues[cfg.Venue]
	if !ok {
		return strategy.DecisionRecord{}, fmt.Errorf("venue %q is not wired: %w", cfg.Venue, ErrNotFound)
	}

	rec := r.tick(ctx, e.strat, man, cfg, v, dryRun)

	r.mu.Lock()
	t := rec.TS
	e.lastRun = &t
	e.lastID = rec.ID
	e.lastErr = rec.Error
	if rec.Error == "" {
		e.lastAct = rec.LastAction()
	}
	if e.cfg.Enabled {
		next := t.Add(man.CadenceDuration())
		e.nextRun = &next
	}
	st := e.status()
	r.mu.Unlock()
	r.publish(EventDecision, rec)
	r.publish(EventConfig, st)
	r.journalLine(rec)
	return rec, nil
}

// tick is one snapshot → evaluate → decide → review → execute pass.
func (r *Runner) tick(ctx context.Context, s strategy.Strategy, man strategy.Manifest, cfg strategy.StrategyConfig, v venue.Venue, dryRun bool) strategy.DecisionRecord {
	now := r.now()
	params := strategy.Merge(man, cfg.Params)
	rec := strategy.DecisionRecord{
		ID:         NewID(now),
		TS:         now,
		StrategyID: man.ID,
		Venue:      cfg.Venue,
		DryRun:     dryRun,
		Questions:  man.Questions,
	}
	finish := func(rec strategy.DecisionRecord) strategy.DecisionRecord {
		stored, err := r.store.Append(rec)
		if err != nil {
			log.Printf("strategy %s: decision store: %v", man.ID, err)
		}
		return stored
	}

	state, err := s.Snapshot(ctx, v, params)
	if err != nil {
		rec.Error = "snapshot: " + err.Error()
		return finish(rec)
	}
	rec.State = state
	rec.StateDigest = Digest(state)

	res, err := r.decider.Evaluate(ctx, state, man.Questions)
	if err != nil {
		rec.Error = "decider: " + err.Error()
		return finish(rec)
	}
	rec.Answers = res.Answers
	rec.Model = res.Model
	rec.Usage = res.Usage
	rec.LatencyMs = res.LatencyMs

	intents := s.Decide(state, res.Answers, params)
	for i := range intents {
		if intents[i].ID == "" {
			intents[i].ID = fmt.Sprintf("%s-%d", rec.ID, i+1)
		}
		intents[i].StrategyID = man.ID
		intents[i].Venue = cfg.Venue
	}
	rec.Intents = intents
	if dryRun {
		return finish(rec)
	}

	for _, in := range intents {
		rec.Verdicts = append(rec.Verdicts, r.gov.Review(in, cfg.Governor))
	}
	rec = finish(rec)
	for _, in := range intents {
		if vd, ok := rec.LatestVerdict(in.ID); ok && vd.Status == strategy.StatusApproved {
			r.execute(ctx, rec.ID, in, v)
		}
	}
	if stored, ok := r.store.Get(rec.ID); ok {
		rec = stored
	}
	return rec
}

// execute places an approved intent and records executed/failed.
func (r *Runner) execute(ctx context.Context, decisionID string, in strategy.Intent, v venue.Venue) {
	if in.Action == strategy.ActionHold {
		r.appendVerdict(decisionID, strategy.Verdict{IntentID: in.ID, Status: strategy.StatusExecuted, By: strategy.ByGovernor, Reason: "hold: no order"})
		return
	}
	positions, err := v.Positions(ctx)
	if err != nil {
		r.appendVerdict(decisionID, strategy.Verdict{IntentID: in.ID, Status: strategy.StatusFailed, By: strategy.ByVenue, Reason: "positions: " + err.Error()})
		return
	}
	order, err := IntentToOrder(in, positions)
	if err != nil {
		r.appendVerdict(decisionID, strategy.Verdict{IntentID: in.ID, Status: strategy.StatusFailed, By: strategy.ByVenue, Reason: err.Error()})
		return
	}
	fill, err := v.Place(ctx, order)
	if err != nil {
		r.appendVerdict(decisionID, strategy.Verdict{IntentID: in.ID, Status: strategy.StatusFailed, By: strategy.ByVenue, Reason: err.Error()})
		return
	}
	reason := fmt.Sprintf("%s %s $%.2f @ %.4f", fill.Side, fill.Market, fill.SizeUSD, fill.Price)
	if fill.TxHash != "" {
		reason += " tx=" + fill.TxHash
	}
	r.appendVerdict(decisionID, strategy.Verdict{
		IntentID: in.ID, Status: strategy.StatusExecuted, By: strategy.ByVenue, Reason: reason,
	})
}

// IntentToOrder maps an intent onto a venue order given the current
// positions. close and scale derive their side from the open position;
// rebalance needs the strategy's Side hint.
func IntentToOrder(in strategy.Intent, positions []venue.Position) (venue.Order, error) {
	o := venue.Order{
		ID: in.ID, Market: in.Market, SizeUSD: in.SizeUSD,
		PriceLimit: in.PriceLimit, Confidence: in.Confidence, Reason: in.Reason,
	}
	pos := venue.PositionFor(positions, in.Market)
	switch in.Action {
	case strategy.ActionOpenLong:
		o.Side = venue.SideBuy
	case strategy.ActionOpenShort:
		o.Side = venue.SideSell
	case strategy.ActionClose:
		if pos.SizeUSD == 0 {
			return o, fmt.Errorf("close %s: no open position", in.Market)
		}
		o.ReduceOnly = true
		o.Side = venue.SideSell
		if pos.SizeUSD < 0 {
			o.Side = venue.SideBuy
		}
		if o.SizeUSD <= 0 {
			o.SizeUSD = math.Abs(pos.SizeUSD)
		}
	case strategy.ActionScale:
		switch {
		case in.Side != "":
			o.Side = in.Side
		case pos.SizeUSD > 0:
			o.Side = venue.SideBuy
		case pos.SizeUSD < 0:
			o.Side = venue.SideSell
		default:
			return o, fmt.Errorf("scale %s: no open position and no side", in.Market)
		}
	case strategy.ActionRebalance:
		if in.Side != venue.SideBuy && in.Side != venue.SideSell {
			return o, fmt.Errorf("rebalance %s: intent carries no side", in.Market)
		}
		o.Side = in.Side
		// Trimming toward zero is reduce-only; crossing through zero would
		// need two legs, which this scaffold does not split.
		if (pos.SizeUSD > 0 && in.Side == venue.SideSell) || (pos.SizeUSD < 0 && in.Side == venue.SideBuy) {
			if in.SizeUSD <= math.Abs(pos.SizeUSD)+1e-9 {
				o.ReduceOnly = true
			}
		}
	default:
		return o, fmt.Errorf("action %q cannot be placed", in.Action)
	}
	if o.SizeUSD <= 0 {
		return o, fmt.Errorf("%s %s: size_usd must be positive", in.Action, in.Market)
	}
	return o, nil
}

// Approve is the operator's confirm: the intent must be proposed. It is
// marked approved then executed through the venue.
func (r *Runner) Approve(ctx context.Context, decisionID, intentID string) (strategy.DecisionRecord, error) {
	rec, in, err := r.pending(decisionID, intentID)
	if err != nil {
		return rec, err
	}
	if r.gov.Killed() {
		return rec, fmt.Errorf("%w: governor is killed", ErrConflict)
	}
	v, ok := r.venues[in.Venue]
	if !ok {
		return rec, fmt.Errorf("venue %q is not wired: %w", in.Venue, ErrNotFound)
	}
	r.appendVerdict(decisionID, strategy.Verdict{IntentID: intentID, Status: strategy.StatusApproved, By: strategy.ByOperator, Reason: "approved by operator"})
	r.execute(ctx, decisionID, in, v)
	rec, _ = r.store.Get(decisionID)
	return rec, nil
}

// Reject is the operator's veto: the intent must be proposed.
func (r *Runner) Reject(decisionID, intentID string) (strategy.DecisionRecord, error) {
	rec, _, err := r.pending(decisionID, intentID)
	if err != nil {
		return rec, err
	}
	return r.appendVerdict(decisionID, strategy.Verdict{IntentID: intentID, Status: strategy.StatusRejected, By: strategy.ByOperator, Reason: "rejected by operator"}), nil
}

// pending resolves a (decision, intent) whose latest verdict is proposed.
func (r *Runner) pending(decisionID, intentID string) (strategy.DecisionRecord, strategy.Intent, error) {
	rec, ok := r.store.Get(decisionID)
	if !ok {
		return rec, strategy.Intent{}, fmt.Errorf("decision %q: %w", decisionID, ErrNotFound)
	}
	in, ok := rec.Intent(intentID)
	if !ok {
		return rec, in, fmt.Errorf("intent %q: %w", intentID, ErrNotFound)
	}
	vd, ok := rec.LatestVerdict(intentID)
	if !ok || vd.Status != strategy.StatusProposed {
		status := "none"
		if ok {
			status = vd.Status
		}
		return rec, in, fmt.Errorf("%w: intent %s is %s, not proposed", ErrConflict, intentID, status)
	}
	return rec, in, nil
}

// appendVerdict stores and publishes one verdict.
func (r *Runner) appendVerdict(decisionID string, v strategy.Verdict) strategy.DecisionRecord {
	v.TS = r.now()
	rec, err := r.store.AppendVerdict(decisionID, v)
	if err != nil {
		log.Printf("strategy: verdict %s/%s: %v", decisionID, v.IntentID, err)
		return rec
	}
	r.publish(EventVerdict, VerdictEvent{DecisionID: decisionID, Verdict: v})
	return rec
}

func (r *Runner) publish(typ string, data any) {
	if r.bus == nil {
		return
	}
	r.bus.PublishStrategy(bus.StrategyEvent{Type: typ, Data: data})
}

// journalLine writes one summary per decision to the legacy journal.
func (r *Runner) journalLine(rec strategy.DecisionRecord) {
	if r.journal == nil {
		return
	}
	coin := ""
	if len(rec.Intents) > 0 {
		coin = rec.Intents[0].Market
	}
	kind := "strategy"
	if rec.Error != "" {
		kind = "error"
	}
	_ = r.journal.Record(journal.Entry{Coin: coin, Kind: kind, Summary: Summary(rec)})
}

// Summary renders a one-line human summary of a decision record.
func Summary(rec strategy.DecisionRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "strategy %s@%s", rec.StrategyID, rec.Venue)
	if rec.DryRun {
		b.WriteString(" (dry run)")
	}
	if rec.Error != "" {
		fmt.Fprintf(&b, ": %s", rec.Error)
		return b.String()
	}
	if len(rec.Intents) == 0 {
		b.WriteString(": no intents")
	}
	for _, in := range rec.Intents {
		fmt.Fprintf(&b, ": %s %s $%.0f", in.Action, in.Market, in.SizeUSD)
		if vd, ok := rec.LatestVerdict(in.ID); ok {
			fmt.Fprintf(&b, " [%s]", vd.Status)
		}
	}
	fmt.Fprintf(&b, " model=%s %dms", rec.Model, rec.LatencyMs)
	return b.String()
}

// Digest is "sha256:<hex>" of the state's canonical JSON.
func Digest(state any) string {
	b, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
