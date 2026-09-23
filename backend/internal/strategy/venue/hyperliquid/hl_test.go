package hyperliquid

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// infoServer serves canned HL info responses keyed by request type.
func infoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req["type"] {
		case "metaAndAssetCtxs":
			_, _ = w.Write([]byte(`[{"universe":[{"name":"BTC","szDecimals":5},{"name":"ETH","szDecimals":4}]},
			  [{"funding":"0.0001","openInterest":"1000","markPx":"50000","oraclePx":"49900","premium":"0.002","dayNtlVlm":"1e9","prevDayPx":"48000"},
			   {"funding":"-0.0002","openInterest":"20000","markPx":"3000","oraclePx":"3001","premium":"-0.0003","dayNtlVlm":"5e8","prevDayPx":"3100"}]]`))
		case "allMids":
			_, _ = w.Write([]byte(`{"BTC":"50010","ETH":"2999.5"}`))
		case "clearinghouseState":
			_, _ = w.Write([]byte(`{"assetPositions":[{"position":{"coin":"ETH","szi":"-0.1","entryPx":"3100","positionValue":"300","unrealizedPnl":"10"}}],
			  "marginSummary":{"accountValue":"10000","totalNtlPos":"300"},"withdrawable":"9000"}`))
		default:
			http.Error(w, "unknown", 400)
		}
	}))
}

func TestMarketsAndPositions(t *testing.T) {
	srv := infoServer(t)
	defer srv.Close()
	v := New(hlclient.New(srv.URL), "0xabc")
	ctx := context.Background()

	ms, err := v.Markets(ctx, []string{"ETH", "BTC", "NOPE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Symbol != "BTC" || ms[1].Symbol != "ETH" {
		t.Fatalf("markets = %+v", ms)
	}
	btc := ms[0]
	if btc.Mark != 50000 || btc.Mid != 50010 || btc.Funding != 0.0001 || btc.OpenInterestUSD != 1000*50000 {
		t.Errorf("btc = %+v", btc)
	}
	if dc := btc.DayChange; dc < 0.0416 || dc > 0.0417 {
		t.Errorf("btc day_change = %v, want ~0.04167", dc)
	}
	pos, err := v.Positions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 || pos[0].Market != "ETH" || pos[0].SizeUSD >= 0 || pos[0].UPnLUSD != 10 {
		t.Errorf("positions = %+v", pos)
	}
	st, _ := v.Status(ctx)
	if st.Status != "connected" || st.Kind != "hyperliquid" || len(st.Positions) != 1 {
		t.Errorf("status = %+v", st)
	}
	// No address → no positions, no error.
	anon := New(hlclient.New(srv.URL), "")
	if pos, err := anon.Positions(ctx); err != nil || len(pos) != 0 {
		t.Errorf("anon positions = %+v, %v", pos, err)
	}
}

type stubExec struct {
	got []metrics.Verdict
	err error
}

func (s *stubExec) Execute(_ context.Context, v metrics.Verdict) error {
	s.got = append(s.got, v)
	return s.err
}

func TestPlaceRoutesThroughExecutorOrRefuses(t *testing.T) {
	srv := infoServer(t)
	defer srv.Close()
	ctx := context.Background()

	bare := New(hlclient.New(srv.URL), "")
	if _, err := bare.Place(ctx, venue.Order{Market: "ETH", Side: venue.SideSell, SizeUSD: 100}); !errors.Is(err, venue.ErrNotImplemented) {
		t.Fatalf("Place without executor = %v, want ErrNotImplemented", err)
	}
	if caps := bare.Capabilities(); len(caps) != 1 || caps[0] != "perps" {
		t.Errorf("caps = %v", caps)
	}

	ex := &stubExec{}
	v := New(hlclient.New(srv.URL), "", WithExecutor(ex))
	fill, err := v.Place(ctx, venue.Order{ID: "i1", Market: "ETH", Side: venue.SideSell, SizeUSD: 250, Reason: "fade", Confidence: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.got) != 1 || ex.got[0].Action != metrics.ActionOpenShort || ex.got[0].SizeUSD != 250 || ex.got[0].Entry.Type != "market" {
		t.Errorf("verdict = %+v", ex.got)
	}
	if ex.got[0].Source != "" {
		t.Errorf("strategy verdicts must not claim a reasoning tier: %q", ex.got[0].Source)
	}
	if fill.OrderID != "i1" || fill.SizeUSD != 250 {
		t.Errorf("fill = %+v", fill)
	}
	// Buy → open_long; reduce-only sell → close.
	_, _ = v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideBuy, SizeUSD: 100})
	_, _ = v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideSell, SizeUSD: 100, ReduceOnly: true})
	if ex.got[1].Action != metrics.ActionOpenLong || ex.got[2].Action != metrics.ActionClose {
		t.Errorf("actions = %v %v", ex.got[1].Action, ex.got[2].Action)
	}
	// Executor refusal (risk gate) surfaces as an error, no fill.
	ex.err = errors.New("risk gate: size exceeds max position")
	if _, err := v.Place(ctx, venue.Order{Market: "BTC", Side: venue.SideBuy, SizeUSD: 1e9}); err == nil {
		t.Error("gate refusal should fail Place")
	}
}
