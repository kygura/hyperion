// Package paper is the in-memory venue simulator: fills at mark plus a
// configurable slippage, tracks positions and unrealised PnL from the marks
// it is fed (SetMarks, or a MarketSource pulled on every Markets call).
// Deterministic: injectable clock, sequential order ids, no randomness.
package paper

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// ID is the venue id.
const ID = "paper"

// MarketSource feeds live marks into the simulator (e.g. the hyperliquid
// adapter's Markets). Called on every Markets call; its result replaces the
// stored marks for the symbols it returns.
type MarketSource func(ctx context.Context, symbols []string) ([]venue.Market, error)

// Venue is the simulator. Safe for concurrent use.
type Venue struct {
	slippageBps float64
	feeBps      float64
	source      MarketSource
	now         func() time.Time

	mu        sync.Mutex
	marks     map[string]venue.Market
	positions map[string]*position
	fills     []venue.Fill
	seq       int
}

// position is tracked in base units so uPnL follows the mark.
type position struct {
	size  float64 // signed base units
	entry float64 // volume-weighted entry
}

// Option configures the simulator.
type Option func(*Venue)

// WithSlippageBps sets the fill slippage in basis points (default 5).
func WithSlippageBps(bps float64) Option { return func(v *Venue) { v.slippageBps = bps } }

// WithFeeBps sets the taker fee in basis points (default 0).
func WithFeeBps(bps float64) Option { return func(v *Venue) { v.feeBps = bps } }

// WithSource sets a live MarketSource.
func WithSource(s MarketSource) Option { return func(v *Venue) { v.source = s } }

// WithClock injects the clock (tests).
func WithClock(now func() time.Time) Option { return func(v *Venue) { v.now = now } }

// New builds a simulator with no marks; feed it with SetMarks or WithSource.
func New(opts ...Option) *Venue {
	v := &Venue{
		slippageBps: 5,
		now:         time.Now,
		marks:       make(map[string]venue.Market),
		positions:   make(map[string]*position),
	}
	for _, o := range opts {
		o(v)
	}
	return v
}

// ID implements venue.Venue.
func (v *Venue) ID() string { return ID }

// Capabilities implements venue.Venue.
func (v *Venue) Capabilities() []string { return []string{"perps", "paper"} }

// SetMarks replaces the stored market context for the given symbols.
func (v *Venue) SetMarks(markets ...venue.Market) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, m := range markets {
		if m.Mid == 0 {
			m.Mid = m.Mark
		}
		v.marks[m.Symbol] = m
	}
}

// Status implements venue.Venue.
func (v *Venue) Status(ctx context.Context) (venue.VenueStatus, error) {
	pos, _ := v.Positions(ctx)
	return venue.VenueStatus{
		ID:           ID,
		Kind:         "paper",
		Chain:        "none",
		Status:       "connected",
		Capabilities: v.Capabilities(),
		Positions:    pos,
	}, nil
}

// Markets implements venue.Venue: pulls the source (when set) then returns
// the stored marks for symbols (all when empty), sorted by symbol.
func (v *Venue) Markets(ctx context.Context, symbols []string) ([]venue.Market, error) {
	if v.source != nil {
		live, err := v.source(ctx, symbols)
		if err != nil {
			// Stale marks are better than none for a simulator; surface the
			// error only when we have nothing at all.
			v.mu.Lock()
			empty := len(v.marks) == 0
			v.mu.Unlock()
			if empty {
				return nil, fmt.Errorf("paper: market source: %w", err)
			}
		} else {
			v.SetMarks(live...)
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []venue.Market
	if len(symbols) == 0 {
		for _, m := range v.marks {
			out = append(out, m)
		}
	} else {
		for _, s := range symbols {
			if m, ok := v.marks[s]; ok {
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

// Positions implements venue.Venue, in USD at the current mark.
func (v *Venue) Positions(context.Context) ([]venue.Position, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]venue.Position, 0, len(v.positions))
	for sym, p := range v.positions {
		if p.size == 0 {
			continue
		}
		mark := p.entry
		if m, ok := v.marks[sym]; ok && m.Mark > 0 {
			mark = m.Mark
		}
		out = append(out, venue.Position{
			Market:  sym,
			SizeUSD: round2(p.size * mark),
			Entry:   p.entry,
			Mark:    mark,
			UPnLUSD: round2(p.size * (mark - p.entry)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out, nil
}

// Place implements venue.Venue: an immediate fill at mark ± slippage.
// ReduceOnly orders are clipped to the open position and never flip it.
func (v *Venue) Place(_ context.Context, o venue.Order) (venue.Fill, error) {
	if o.SizeUSD <= 0 {
		return venue.Fill{}, fmt.Errorf("paper: size_usd must be positive")
	}
	if o.Side != venue.SideBuy && o.Side != venue.SideSell {
		return venue.Fill{}, fmt.Errorf("paper: side must be buy|sell, got %q", o.Side)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	m, ok := v.marks[o.Market]
	if !ok || m.Mark <= 0 {
		return venue.Fill{}, fmt.Errorf("paper: no mark for %s", o.Market)
	}
	dir := 1.0
	if o.Side == venue.SideSell {
		dir = -1
	}
	price := m.Mark * (1 + dir*v.slippageBps/10000)
	if o.PriceLimit != nil {
		if (dir > 0 && price > *o.PriceLimit) || (dir < 0 && price < *o.PriceLimit) {
			return venue.Fill{}, fmt.Errorf("paper: price %.4f outside limit %.4f", price, *o.PriceLimit)
		}
	}
	units := o.SizeUSD / price
	p := v.positions[o.Market]
	if p == nil {
		p = &position{}
		v.positions[o.Market] = p
	}
	if o.ReduceOnly {
		if p.size == 0 || (p.size > 0) == (dir > 0) {
			return venue.Fill{}, fmt.Errorf("paper: reduce-only %s on %s would not reduce", o.Side, o.Market)
		}
		if units > math.Abs(p.size) {
			units = math.Abs(p.size)
		}
	}
	delta := dir * units
	newSize := p.size + delta
	switch {
	case p.size == 0 || (p.size > 0) == (delta > 0):
		// Adding to (or opening) a position: volume-weighted entry.
		p.entry = (math.Abs(p.size)*p.entry + units*price) / (math.Abs(p.size) + units)
	case math.Abs(delta) > math.Abs(p.size):
		// Flip: the remainder opens a fresh position at the fill price.
		p.entry = price
	}
	p.size = newSize
	if math.Abs(p.size) < 1e-12 {
		delete(v.positions, o.Market)
	}
	v.seq++
	id := o.ID
	if id == "" {
		id = fmt.Sprintf("paper-%d", v.seq)
	}
	fill := venue.Fill{
		OrderID: id,
		Market:  o.Market,
		Side:    o.Side,
		SizeUSD: round2(units * price),
		Price:   price,
		FeeUSD:  round2(units * price * v.feeBps / 10000),
		TS:      v.now(),
	}
	v.fills = append(v.fills, fill)
	return fill, nil
}

// Cancel implements venue.Venue: fills are immediate, so nothing rests.
func (v *Venue) Cancel(context.Context, string) error { return nil }

// Fills returns every fill so far (tests, reporting).
func (v *Venue) Fills() []venue.Fill {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]venue.Fill(nil), v.fills...)
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
