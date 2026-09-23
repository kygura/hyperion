package paper

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

func fixed() func() time.Time {
	t := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestFillsAtMarkWithSlippageAndTracksUPnL(t *testing.T) {
	v := New(WithSlippageBps(10), WithClock(fixed()))
	v.SetMarks(venue.Market{Symbol: "ETH", Mark: 3000})
	ctx := context.Background()

	fill, err := v.Place(ctx, venue.Order{Market: "ETH", Side: venue.SideSell, SizeUSD: 300})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(fill.Price-2997) > 1e-9 {
		t.Errorf("sell fill price = %v, want 2997 (10 bps below mark)", fill.Price)
	}
	if fill.OrderID != "paper-1" || !fill.TS.Equal(fixed()()) {
		t.Errorf("fill id/ts = %+v", fill)
	}
	pos, _ := v.Positions(ctx)
	if len(pos) != 1 || pos[0].SizeUSD >= 0 {
		t.Fatalf("positions = %+v, want one short", pos)
	}
	// Mark drops 3% → short gains.
	v.SetMarks(venue.Market{Symbol: "ETH", Mark: 2910})
	pos, _ = v.Positions(ctx)
	if pos[0].UPnLUSD <= 0 || pos[0].Mark != 2910 {
		t.Errorf("after mark drop: %+v, want positive upnl", pos[0])
	}
	if math.Abs(pos[0].SizeUSD-(-300.0/2997*2910)) > 0.01 {
		t.Errorf("size_usd = %v", pos[0].SizeUSD)
	}
}

func TestReduceOnlyClipsAndNeverFlips(t *testing.T) {
	v := New(WithSlippageBps(0), WithClock(fixed()))
	v.SetMarks(venue.Market{Symbol: "BTC", Mark: 50000})
	ctx := context.Background()
	if _, err := v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideBuy, SizeUSD: 1000}); err != nil {
		t.Fatal(err)
	}
	// Reduce-only sell bigger than the position: clipped to flat.
	fill, err := v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideSell, SizeUSD: 5000, ReduceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(fill.SizeUSD-1000) > 0.01 {
		t.Errorf("reduce-only fill = %v, want clipped to 1000", fill.SizeUSD)
	}
	pos, _ := v.Positions(ctx)
	if len(pos) != 0 {
		t.Errorf("positions after close = %+v, want flat", pos)
	}
	// Reduce-only on a flat book is refused.
	if _, err := v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideSell, SizeUSD: 10, ReduceOnly: true}); err == nil {
		t.Error("reduce-only on flat should fail")
	}
}

func TestUnknownMarketAndSource(t *testing.T) {
	ctx := context.Background()
	v := New()
	if _, err := v.Place(ctx, venue.Order{Market: "XYZ", Side: venue.SideBuy, SizeUSD: 10}); err == nil {
		t.Error("place without mark should fail")
	}
	calls := 0
	src := func(ctx context.Context, symbols []string) ([]venue.Market, error) {
		calls++
		if calls == 1 {
			return []venue.Market{{Symbol: "ETH", Mark: 3000, Funding: 0.0001}}, nil
		}
		return nil, errors.New("down")
	}
	v = New(WithSource(src))
	ms, err := v.Markets(ctx, []string{"ETH"})
	if err != nil || len(ms) != 1 || ms[0].Mid != 3000 {
		t.Fatalf("markets = %+v, %v", ms, err)
	}
	// Source failure with cached marks: stale marks served, no error.
	ms, err = v.Markets(ctx, nil)
	if err != nil || len(ms) != 1 {
		t.Fatalf("stale markets = %+v, %v", ms, err)
	}
	st, _ := v.Status(ctx)
	if st.ID != "paper" || st.Kind != "paper" || st.Chain != "none" || st.Status != "connected" {
		t.Errorf("status = %+v", st)
	}
}
