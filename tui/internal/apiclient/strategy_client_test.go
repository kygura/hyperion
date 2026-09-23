package apiclient

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestManifests(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/strategy/manifests", nil, http.StatusOK,
		map[string]any{"manifests": []any{mustAny(t, protoManifest)}})
	defer srv.Close()
	got, err := New(srv.URL, "").Manifests(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "funding_skew" {
		t.Fatalf("Manifests = %+v, %v", got, err)
	}
}

func TestStrategyConfigsAndConfig(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/strategy/configs", nil, http.StatusOK,
		map[string]any{"strategies": []any{mustAny(t, protoStatus)}})
	got, err := New(srv.URL, "tok").StrategyConfigs(context.Background())
	srv.Close()
	if err != nil || len(got) != 1 || got[0].Config.Venue != "paper" {
		t.Fatalf("StrategyConfigs = %+v, %v", got, err)
	}

	srv = newTestServer(t, http.MethodGet, "/api/strategy/configs/funding_skew", nil, http.StatusOK, mustAny(t, protoStatus))
	defer srv.Close()
	one, err := New(srv.URL, "").StrategyConfig(context.Background(), "funding_skew")
	if err != nil || one.Manifest.Name != "Funding skew fade" {
		t.Fatalf("StrategyConfig = %+v, %v", one, err)
	}
}

func TestPutStrategyConfig(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/strategy/configs/funding_skew", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["id"] != "funding_skew" || m["enabled"] != true || m["venue"] != "paper" {
			t.Errorf("body = %v", m)
		}
		params, _ := m["params"].(map[string]any)
		if params["size_usd"] != float64(500) {
			t.Errorf("params = %v", m["params"])
		}
		if _, has := m["governor"]; has {
			t.Errorf("nil governor override should be omitted: %v", m)
		}
	}, http.StatusOK, mustAny(t, protoStatus))
	defer srv.Close()
	cfg := StrategyConfig{ID: "funding_skew", Enabled: true, Venue: "paper", Params: map[string]any{"size_usd": 500}}
	got, err := New(srv.URL, "").PutStrategyConfig(context.Background(), cfg)
	if err != nil || got.Manifest.ID != "funding_skew" {
		t.Fatalf("PutStrategyConfig = %+v, %v", got, err)
	}
}

func TestPutStrategyConfigValidationError(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/strategy/configs/funding_skew", nil, http.StatusBadRequest,
		map[string]any{"error": "size_usd above max", "field": "params.size_usd"})
	defer srv.Close()
	_, err := New(srv.URL, "").PutStrategyConfig(context.Background(), StrategyConfig{ID: "funding_skew", Params: map[string]any{}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v, want *APIError", err, err)
	}
	if apiErr.Status != 400 || apiErr.Field != "params.size_usd" || apiErr.Error() != "size_usd above max" {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestRunStrategyDryRun(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/strategy/configs/funding_skew/run", func(t *testing.T, buf []byte) {
		if m := decodeBody(t, buf); m["dry_run"] != true {
			t.Errorf("body = %v, want dry_run true", m)
		}
	}, http.StatusOK, mustAny(t, protoDecision))
	defer srv.Close()
	got, err := New(srv.URL, "").RunStrategy(context.Background(), "funding_skew", true)
	if err != nil || got.ID != "01J…" || len(got.Intents) != 1 {
		t.Fatalf("RunStrategy = %+v, %v", got, err)
	}
}

func TestRunStrategyDeciderUnavailable(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/strategy/configs/x/run", nil, http.StatusServiceUnavailable,
		map[string]any{"error": "decider unavailable"})
	defer srv.Close()
	_, err := New(srv.URL, "").RunStrategy(context.Background(), "x", true)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 || err.Error() != "decider unavailable" {
		t.Fatalf("err = %v", err)
	}
}

func TestDecisions(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/strategy/decisions?limit=50&strategy=funding_skew", nil, http.StatusOK,
		map[string]any{"decisions": []any{mustAny(t, protoDecision)}})
	got, err := New(srv.URL, "").Decisions(context.Background(), 50, "funding_skew")
	srv.Close()
	if err != nil || len(got) != 1 || got[0].Model != "jev-1.13.0" {
		t.Fatalf("Decisions = %+v, %v", got, err)
	}

	srv = newTestServer(t, http.MethodGet, "/api/strategy/decisions", nil, http.StatusOK, map[string]any{"decisions": []any{}})
	got, err = New(srv.URL, "").Decisions(context.Background(), 0, "")
	srv.Close()
	if err != nil || len(got) != 0 {
		t.Fatalf("Decisions(no params) = %+v, %v", got, err)
	}

	srv = newTestServer(t, http.MethodGet, "/api/strategy/decisions/01J", nil, http.StatusOK, mustAny(t, protoDecision))
	defer srv.Close()
	one, err := New(srv.URL, "").Decision(context.Background(), "01J")
	if err != nil || one.StrategyID != "funding_skew" {
		t.Fatalf("Decision = %+v, %v", one, err)
	}
}

func TestApproveRejectIntent(t *testing.T) {
	for _, verb := range []string{"approve", "reject"} {
		srv := newTestServer(t, http.MethodPost, "/api/strategy/decisions/d1/intents/i1/"+verb, nil, http.StatusOK, mustAny(t, protoDecision))
		c := New(srv.URL, "")
		var (
			got DecisionRecord
			err error
		)
		if verb == "approve" {
			got, err = c.ApproveIntent(context.Background(), "d1", "i1")
		} else {
			got, err = c.RejectIntent(context.Background(), "d1", "i1")
		}
		srv.Close()
		if err != nil || got.ID != "01J…" {
			t.Fatalf("%s = %+v, %v", verb, got, err)
		}
	}
}

func TestVenues(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/strategy/venues", nil, http.StatusOK,
		map[string]any{"venues": []any{mustAny(t, protoVenue)}})
	defer srv.Close()
	got, err := New(srv.URL, "").Venues(context.Background())
	if err != nil || len(got) != 1 || got[0].Positions[0].Market != "ETH" {
		t.Fatalf("Venues = %+v, %v", got, err)
	}
}

func TestGovernorGetPutKill(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/strategy/governor", nil, http.StatusOK, mustAny(t, protoGovernor))
	g, err := New(srv.URL, "").Governor(context.Background())
	srv.Close()
	if err != nil || g.Mode != "manual" || g.MaxOpenIntents != 5 {
		t.Fatalf("Governor = %+v, %v", g, err)
	}

	srv = newTestServer(t, http.MethodPut, "/api/strategy/governor", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["mode"] != "threshold" || m["min_confidence"] != 0.8 || m["max_notional_usd"] != float64(2000) || m["max_open_intents"] != float64(3) || m["killed"] != false {
			t.Errorf("body = %v", m)
		}
	}, http.StatusOK, map[string]any{"mode": "threshold", "min_confidence": 0.8, "max_notional_usd": 2000, "max_open_intents": 3, "killed": false})
	g, err = New(srv.URL, "").PutGovernor(context.Background(), GovernorSettings{Mode: "threshold", MinConfidence: 0.8, MaxNotionalUSD: 2000, MaxOpenIntents: 3})
	srv.Close()
	if err != nil || g.Mode != "threshold" {
		t.Fatalf("PutGovernor = %+v, %v", g, err)
	}

	srv = newTestServer(t, http.MethodPost, "/api/strategy/kill", nil, http.StatusOK,
		map[string]any{"mode": "manual", "min_confidence": 0.75, "max_notional_usd": 1000, "max_open_intents": 5, "killed": true})
	defer srv.Close()
	g, err = New(srv.URL, "").Kill(context.Background())
	if err != nil || !g.Killed {
		t.Fatalf("Kill = %+v, %v", g, err)
	}
}

func mustAny(t *testing.T, raw string) any {
	t.Helper()
	return mustDecode[any](t, raw)
}
