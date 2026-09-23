// Package hyperliquid adapts the existing hlclient (info REST) and executor
// (risk gates + EIP-712 signing + exchange POST) to the venue.Venue contract.
// Markets/Positions are read-only info calls; Place routes through
// executor.Execute so every legacy risk gate (position caps, exposure,
// daily-loss kill, price sanity) runs unchanged. Without an executor, Place
// returns venue.ErrNotImplemented — there is no unsigned path to the wire.
package hyperliquid

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// ID is the venue id.
const ID = "hyperliquid"

// Executor is the slice of *executor.Executor the adapter needs; an
// interface so tests can stub the signing path.
type Executor interface {
	Execute(ctx context.Context, v metrics.Verdict) error
}

// Venue is the adapter. Safe for concurrent use (the underlying clients are).
type Venue struct {
	client  *hlclient.Client
	address string // master address for positions; "" → no account visibility
	exec    Executor
	now     func() time.Time
}

// Option configures the adapter.
type Option func(*Venue)

// WithExecutor enables Place through the legacy executor.
func WithExecutor(e Executor) Option {
	return func(v *Venue) {
		if e != nil {
			v.exec = e
		}
	}
}

// WithClock injects the clock (tests).
func WithClock(now func() time.Time) Option { return func(v *Venue) { v.now = now } }

// New builds the adapter over an info client and an optional master address.
func New(client *hlclient.Client, address string, opts ...Option) *Venue {
	v := &Venue{client: client, address: address, now: time.Now}
	for _, o := range opts {
		o(v)
	}
	return v
}

// ID implements venue.Venue.
func (v *Venue) ID() string { return ID }

// Capabilities implements venue.Venue.
func (v *Venue) Capabilities() []string {
	caps := []string{"perps"}
	if v.exec != nil {
		caps = append(caps, "execute")
	}
	return caps
}

// Status implements venue.Venue: connected when the info endpoint answers.
func (v *Venue) Status(ctx context.Context) (venue.VenueStatus, error) {
	st := venue.VenueStatus{
		ID:           ID,
		Kind:         "hyperliquid",
		Chain:        "hyperliquid",
		Status:       "connected",
		Capabilities: v.Capabilities(),
		Positions:    []venue.Position{},
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := v.client.AllMids(cctx); err != nil {
		st.Status = "disconnected"
		st.Error = err.Error()
		return st, nil
	}
	pos, err := v.Positions(cctx)
	if err != nil {
		st.Status = "degraded"
		st.Error = err.Error()
		return st, nil
	}
	st.Positions = pos
	return st, nil
}

// Markets implements venue.Venue from metaAndAssetCtxs + allMids.
func (v *Venue) Markets(ctx context.Context, symbols []string) ([]venue.Market, error) {
	ctxs, err := v.client.MetaAndAssetCtxs(ctx)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: %w", err)
	}
	mids, err := v.client.AllMids(ctx)
	if err != nil {
		mids = nil // mid falls back to mark
	}
	want := symbols
	if len(want) == 0 {
		for coin := range ctxs {
			want = append(want, coin)
		}
	}
	out := make([]venue.Market, 0, len(want))
	for _, sym := range want {
		c, ok := ctxs[sym]
		if !ok {
			continue
		}
		out = append(out, FromAssetCtx(c, mids[sym]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

// FromAssetCtx converts one HL asset context to the venue's Market shape.
// mid 0 falls back to mark; DayChange is derived from prevDayPx when present.
func FromAssetCtx(c metrics.AssetCtx, mid float64) venue.Market {
	if mid == 0 {
		mid = c.MarkPrice
	}
	m := venue.Market{
		Symbol:          c.Coin,
		Mark:            c.MarkPrice,
		Mid:             mid,
		Funding:         c.Funding,
		Premium:         c.Premium,
		OpenInterestUSD: c.OpenInterest * c.MarkPrice,
		DayVolumeUSD:    c.DayVolume,
	}
	if c.PrevDayPrice > 0 && c.MarkPrice > 0 {
		m.DayChange = c.MarkPrice/c.PrevDayPrice - 1
	}
	return m
}

// Positions implements venue.Venue from clearinghouseState; empty without an
// address (no account to read).
func (v *Venue) Positions(ctx context.Context) ([]venue.Position, error) {
	if v.address == "" {
		return []venue.Position{}, nil
	}
	acct, err := v.client.ClearinghouseState(ctx, v.address)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: %w", err)
	}
	out := make([]venue.Position, 0, len(acct.Positions))
	for _, p := range acct.Positions {
		out = append(out, venue.Position{
			Market:  p.Coin,
			SizeUSD: p.Size * p.MarkPrice,
			Entry:   p.EntryPrice,
			Mark:    p.MarkPrice,
			UPnLUSD: p.UnrealPnl,
		})
	}
	return out, nil
}

// Place implements venue.Venue by mapping the order onto a legacy verdict and
// running it through executor.Execute: thesis gate (no-op for strategy
// orders, which carry no Source), risk gates, signing, exchange POST. The
// executor reports acceptance, not the fill price, so Fill.Price is the
// requested mark estimate (0 when unknown).
//
// Note: the legacy submit path always sends reduce-only orders as sells,
// so closing a SHORT through this adapter is refused here rather than sent
// as an order the venue would reject.
func (v *Venue) Place(ctx context.Context, o venue.Order) (venue.Fill, error) {
	if v.exec == nil {
		return venue.Fill{}, fmt.Errorf("%w: hyperliquid Place needs an executor with an agent wallet", venue.ErrNotImplemented)
	}
	if o.SizeUSD <= 0 {
		return venue.Fill{}, fmt.Errorf("hyperliquid: size_usd must be positive")
	}
	var action metrics.Action
	switch {
	case o.ReduceOnly && o.Side == venue.SideSell:
		action = metrics.ActionClose
	case o.ReduceOnly:
		return venue.Fill{}, fmt.Errorf("%w: reduce-only buy (closing a short) is not supported by the legacy submit path", venue.ErrNotImplemented)
	case o.Side == venue.SideBuy:
		action = metrics.ActionOpenLong
	case o.Side == venue.SideSell:
		action = metrics.ActionOpenShort
	default:
		return venue.Fill{}, fmt.Errorf("hyperliquid: side must be buy|sell, got %q", o.Side)
	}
	entry := metrics.Entry{Type: "market"}
	if o.PriceLimit != nil && *o.PriceLimit > 0 {
		entry = metrics.Entry{Type: "limit", Price: *o.PriceLimit}
	}
	verdict := metrics.Verdict{
		Asset:      o.Market,
		Action:     action,
		SizeUSD:    o.SizeUSD,
		Entry:      entry,
		Thesis:     "strategy: " + o.Reason,
		Confidence: clamp01(o.Confidence),
		At:         v.now(),
		Provider:   "strategy",
	}
	if err := v.exec.Execute(ctx, verdict); err != nil {
		return venue.Fill{}, fmt.Errorf("hyperliquid: %w", err)
	}
	return venue.Fill{
		OrderID: o.ID,
		Market:  o.Market,
		Side:    o.Side,
		SizeUSD: o.SizeUSD,
		Price:   entry.Price,
		TS:      v.now(),
	}, nil
}

// Cancel implements venue.Venue. Strategy orders are IOC through the
// executor, so there is nothing resting to cancel by strategy order id.
func (v *Venue) Cancel(context.Context, string) error {
	return fmt.Errorf("%w: cancel by strategy order id", venue.ErrNotImplemented)
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Compile-time check.
var _ Executor = (*executor.Executor)(nil)
