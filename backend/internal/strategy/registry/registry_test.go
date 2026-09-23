package registry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
	"github.com/hyperagent/hyperagent/internal/strategy/venue/paper"
)

type noop struct{}

func (noop) Manifest() strategy.Manifest {
	return strategy.Manifest{ID: "noop", Venues: []string{"paper"}}
}
func (noop) Snapshot(context.Context, venue.Venue, strategy.Params) (strategy.State, error) {
	return map[string]any{}, nil
}
func (noop) Decide(strategy.State, strategy.Answers, strategy.Params) []strategy.Intent { return nil }

func TestRegisterGetNames(t *testing.T) {
	Register("test_noop", func() strategy.Strategy { return noop{} })
	defer Unregister("test_noop")
	s, err := Get("test_noop")
	if err != nil || s.Manifest().ID != "noop" {
		t.Fatalf("Get = %v, %v", s, err)
	}
	if _, err := Get("missing"); err == nil {
		t.Error("unknown name should error")
	}
	found := false
	for _, n := range Names() {
		if n == "test_noop" {
			found = true
		}
	}
	if !found {
		t.Errorf("Names = %v", Names())
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate Register should panic")
		}
	}()
	Register("test_noop", func() strategy.Strategy { return noop{} })
}

// fakeProcess answers the stdio protocol in-process.
func fakeProcess(t *testing.T, calls *[]string) Runner {
	return func(ctx context.Context, request []byte) ([]byte, error) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		*calls = append(*calls, req.Method)
		switch req.Method {
		case "manifest":
			return []byte(`{"result":{"id":"ext","name":"External","version":"0.0.1","venues":["paper"],"cadence":"1m","markets":["ETH"],
			  "params":[{"key":"size_usd","type":"number","default":100}],
			  "questions":{"go":{"type":"noul","instructions":"Should we go"}}}}`), nil
		case "snapshot":
			var p struct {
				Markets []venue.Market `json:"markets"`
				Params  map[string]any `json:"params"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if len(p.Markets) != 1 || p.Markets[0].Symbol != "ETH" {
				t.Errorf("snapshot markets = %+v", p.Markets)
			}
			return []byte(`{"result":{"eth_mark":` + jsonNum(p.Markets[0].Mark) + `}}`), nil
		case "decide":
			var p struct {
				Answers map[string]decider.Answer `json:"answers"`
				Params  map[string]any            `json:"params"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Answers["go"].NoulValue() < 0.5 {
				return []byte(`{"result":[]}`), nil
			}
			return []byte(`{"result":[{"market":"ETH","action":"open_long","size_usd":` + jsonNum(p.Params["size_usd"].(float64)) + `,"reason":"go","confidence":0.9}]}`), nil
		}
		return []byte(`{"error":"unknown method"}`), nil
	}
}

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

func TestStdioPluginHandshakeSnapshotDecide(t *testing.T) {
	var calls []string
	p, err := NewStdio(context.Background(), fakeProcess(t, &calls))
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest()
	if m.ID != "ext" || m.Questions["go"].Type != decider.TypeNoul || len(m.Params) != 1 {
		t.Fatalf("manifest = %+v", m)
	}
	pv := paper.New()
	pv.SetMarks(venue.Market{Symbol: "ETH", Mark: 3000})
	params := strategy.Merge(m, strategy.Params{"size_usd": 150})
	state, err := p.Snapshot(context.Background(), pv, params)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := state.(map[string]any); st["eth_mark"] != 3000.0 {
		t.Errorf("state = %+v", state)
	}
	intents := p.Decide(state, strategy.Answers{"go": {Type: "noul", Noul: decider.F(0.9)}}, params)
	if len(intents) != 1 || intents[0].Action != "open_long" || intents[0].SizeUSD != 150 {
		t.Errorf("intents = %+v", intents)
	}
	if none := p.Decide(state, strategy.Answers{"go": {Type: "noul", Noul: decider.F(0.1)}}, params); len(none) != 0 {
		t.Errorf("low noul intents = %+v", none)
	}
	if strings.Join(calls, ",") != "manifest,snapshot,decide,decide" {
		t.Errorf("calls = %v", calls)
	}
}

func TestStdioPluginHandshakeErrors(t *testing.T) {
	bad := func(ctx context.Context, request []byte) ([]byte, error) {
		return []byte(`{"error":"boom"}`), nil
	}
	if _, err := NewStdio(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want plugin error surfaced", err)
	}
	dead := func(ctx context.Context, request []byte) ([]byte, error) { return nil, errors.New("exec: not found") }
	if _, err := NewStdio(context.Background(), dead); err == nil {
		t.Error("runner failure should fail handshake")
	}
	noID := func(ctx context.Context, request []byte) ([]byte, error) {
		return []byte(`{"result":{"name":"x"}}`), nil
	}
	if _, err := NewStdio(context.Background(), noID); err == nil {
		t.Error("manifest without id should fail")
	}
}
