package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy/decider"
)

func questions() map[string]decider.Question {
	return map[string]decider.Question{
		"regime":   decider.Choice("Which regime", map[string]string{"risk_on": "up", "risk_off": "down", "chop": "flat"}),
		"crowding": decider.Score("How crowded", []string{"not crowded", "somewhat", "extremely"}),
		"extreme":  decider.Noul("Funding is extreme"),
	}
}

// documented is the PROTOCOL.md response shape: choice + score (legend as
// strings) + noul (no confidence).
const documented = `{
  "model": "jev-1.13.0",
  "answers": {
    "regime":   { "type": "choice", "choice": "risk_off", "probabilities": { "risk_on": 0.1, "risk_off": 0.82, "chop": 0.08 }, "confidence": 0.71 },
    "crowding": { "type": "score", "score": 1.35, "probabilities": { "0": 0.05, "1": 0.55, "2": 0.4 }, "legend": { "0": "not crowded", "1": "somewhat", "2": "extremely" }, "confidence": 0.6 },
    "extreme":  { "type": "noul", "noul": 0.93 }
  },
  "usage": { "input_tokens": 412, "output_tokens": 30 }
}`

func TestEvaluateDocumentedShapes(t *testing.T) {
	var gotAuth string
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(documented))
	}))
	defer srv.Close()

	c := New(srv.URL, "jev-latest", "", WithKey("k1"))
	res, err := c.Evaluate(context.Background(), map[string]any{"x": 1}, questions())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if gotAuth != "Bearer k1" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotReq["model"] != "jev-latest" {
		t.Errorf("request model = %v", gotReq["model"])
	}
	if _, ok := gotReq["questions"].(map[string]any)["regime"]; !ok {
		t.Errorf("request questions missing: %v", gotReq)
	}
	if res.Model != "jev-1.13.0" || res.Usage.InputTokens != 412 || res.Usage.OutputTokens != 30 {
		t.Errorf("model/usage = %q %+v", res.Model, res.Usage)
	}
	reg := res.Answers["regime"]
	if reg.Choice != "risk_off" || reg.Conf() != 0.71 || reg.Probabilities["risk_off"] != 0.82 {
		t.Errorf("choice answer = %+v", reg)
	}
	cr := res.Answers["crowding"]
	if cr.ScoreValue() != 1.35 || cr.Legend["1"].What != "somewhat" || cr.Legend["1"].Object != nil {
		t.Errorf("score answer = %+v", cr)
	}
	ex := res.Answers["extreme"]
	if ex.NoulValue() != 0.93 || ex.Confidence != nil {
		t.Errorf("noul answer = %+v", ex)
	}
	// Re-encoding a noul answer must not invent a confidence.
	b, _ := json.Marshal(ex)
	if strings.Contains(string(b), "confidence") {
		t.Errorf("noul answer re-encoded with confidence: %s", b)
	}
}

func TestLegendAsObject(t *testing.T) {
	body := `{"model":"m","answers":{"crowding":{"type":"score","score":2,"probabilities":{"0":0,"1":0,"2":1},
	  "legend":{"0":{"what":"not crowded","why":"x"},"1":{"what":"somewhat"},"2":{"what":"extremely"}},"confidence":0.9}},"usage":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"))
	res, err := c.Evaluate(context.Background(), "s", map[string]decider.Question{"crowding": questions()["crowding"]})
	if err != nil {
		t.Fatal(err)
	}
	l := res.Answers["crowding"].Legend["0"]
	if l.What != "not crowded" || l.Object["why"] != "x" {
		t.Errorf("legend = %+v", l)
	}
	out, _ := json.Marshal(res.Answers["crowding"].Legend)
	if !strings.Contains(string(out), `"why":"x"`) {
		t.Errorf("object legend did not round-trip: %s", out)
	}
}

func TestMissingKeyIsUnavailable(t *testing.T) {
	t.Setenv("JEV_TEST_KEY_UNSET", "")
	c := New("http://127.0.0.1:1", "", "JEV_TEST_KEY_UNSET")
	if err := c.Available(); !errors.Is(err, decider.ErrUnavailable) {
		t.Fatalf("Available = %v, want ErrUnavailable", err)
	}
	if _, err := c.Evaluate(context.Background(), "s", questions()); !errors.Is(err, decider.ErrUnavailable) {
		t.Fatalf("Evaluate = %v, want ErrUnavailable", err)
	}
}

func TestMissingAnswerIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{"regime":{"type":"choice","choice":"chop"}},"usage":{}}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"))
	_, err := c.Evaluate(context.Background(), "s", questions())
	if err == nil || !strings.Contains(err.Error(), "missing answer") {
		t.Fatalf("err = %v, want missing answer", err)
	}
}

func TestNoRetryOn4xxAndBodySurfaced(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"criteria too long"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"))
	_, err := c.Evaluate(context.Background(), "s", questions())
	if err == nil || !strings.Contains(err.Error(), "criteria too long") {
		t.Fatalf("err = %v, want 4xx body", err)
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 400 {
		t.Fatalf("err = %v, want StatusError 400", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on 4xx)", n)
	}
}

func TestOneRetryOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(documented))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"))
	res, err := c.Evaluate(context.Background(), "s", questions())
	if err != nil {
		t.Fatalf("evaluate after retry: %v", err)
	}
	if res.Answers["regime"].Choice != "risk_off" {
		t.Errorf("answers after retry = %+v", res.Answers)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

func TestRetryOn429HonoursRetryAfterAndRequestID(t *testing.T) {
	var calls int32
	var times [2]time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		times[n-1] = time.Now()
		w.Header().Set(RequestIDHeader, "req-abc")
		if n == 1 {
			w.Header().Set("Retry-After", "0.1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(documented))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"))
	if _, err := c.Evaluate(context.Background(), "s", questions()); err != nil {
		t.Fatalf("evaluate after 429: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
	if gap := times[1].Sub(times[0]); gap < 90*time.Millisecond {
		t.Errorf("retry waited %v, want >= Retry-After (100ms)", gap)
	}

	// A failing status carries the request id in its message.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(RequestIDHeader, "req-xyz")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("bad state"))
	}))
	defer srv2.Close()
	c2 := New(srv2.URL, "", "", WithKey("k"))
	_, err := c2.Evaluate(context.Background(), "s", questions())
	if err == nil || !strings.Contains(err.Error(), "req-xyz") || !strings.Contains(err.Error(), "bad state") {
		t.Fatalf("err = %v, want request id and body", err)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	if got := backoff(&StatusError{Status: 529, RetryAfter: 30 * time.Second}); got != MaxRetryAfter {
		t.Errorf("backoff = %v, want cap %v", got, MaxRetryAfter)
	}
	if got := backoff(&StatusError{Status: 500}); got != defaultBackoff {
		t.Errorf("backoff without header = %v, want %v", got, defaultBackoff)
	}
	if !retryable(&StatusError{Status: 529}) || !retryable(&StatusError{Status: 408}) || retryable(&StatusError{Status: 422}) {
		t.Error("retryable set wrong for 529/408/422")
	}
	if parseRetryAfter("2") != 2*time.Second || parseRetryAfter("junk") != 0 {
		t.Error("parseRetryAfter")
	}
}

func TestRetryOnTimeoutThenGiveUp(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(documented))
	}))
	defer srv.Close()
	c := New(srv.URL, "", "", WithKey("k"), WithHTTPClient(&http.Client{Timeout: 30 * time.Millisecond}))
	_, err := c.Evaluate(context.Background(), "s", questions())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("calls = %d, want 2 (one retry on timeout)", n)
	}
}

func TestRejectsMalformedQuestion(t *testing.T) {
	c := New("http://127.0.0.1:1", "", "", WithKey("k"))
	bad := map[string]decider.Question{"q": {Type: "choice", Instructions: "x", Criteria: map[string]string{"only": "one"}}}
	if _, err := c.Evaluate(context.Background(), "s", bad); err == nil {
		t.Fatal("expected validation error")
	}
}
