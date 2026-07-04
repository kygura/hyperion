# Standalone TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `backend/` a control-plane HTTP API (watchlist, execution mode, provider/model settings, thesis) and extract the Bubble Tea TUI out of `backend/internal/tui` into a brand-new `tui/` Go module that talks to `backend/` purely over HTTP+WS — no shared import path — so the backend is a real standalone service with two independent thin clients (`dashboard/`, `tui/`).

**Architecture:** `backend/internal/api` grows five new endpoints plus one bugfix on an existing one (see spec). `backend/src/main.go` drops all TUI wiring and the `-headless` distinction — there is only one run mode now. A new top-level `tui/` module (own `go.mod`) hosts a new `internal/apiclient` package (wire types + `Client` for commands + `Cache` for reads, both HTTP/WS-backed) and `internal/tui` (the moved Bubble Tea `Model`/rendering code, edited to depend on `apiclient.Client`/`apiclient.Cache` instead of backend internals).

**Tech Stack:** Go stdlib `net/http`/`net/url`, `gorilla/websocket` (already a `backend` dependency; `tui/` needs it too for its WS client), `charm.land/bubbletea/v2` + `charm.land/lipgloss/v2` (already a `backend` dependency, moves to `tui/go.mod`). No new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-07-04-standalone-tui-design.md`

## Global Constraints

- Git repo root: `/home/athan/projects/hypertrader`. Commit from repo root.
- Backend module root: `/home/athan/projects/hypertrader/backend` (module `github.com/hyperagent/hyperagent`). Run `go` commands there for backend tasks.
- New TUI module root: `/home/athan/projects/hypertrader/tui` (module `github.com/hyperagent/tui`, created in Task 6). Run `go` commands there for TUI tasks.
- No new third-party dependencies beyond what's already in `backend/go.mod` (moving `bubbletea`/`lipgloss`/`gorilla/websocket` usage to `tui/go.mod` is not "new").
- All error responses: `{"error":"..."}` JSON via the existing `writeErr` helper in `backend/internal/api/server.go`. Domain/validation errors → 422. Missing/nil dependency → 503. Not found → 404.
- Follow existing code style: package doc comments explain the *why*, table-driven tests, no `interface{}` where concrete types work.
- Match existing test conventions: `httptest`-against-`Handler()` for API tests (see `backend/internal/api/act_test.go`, `read_test.go`), table-driven fakes for `reasoner.Provider` (see `backend/internal/reasoner/engine_test.go`).
- Never commit `backend/data/`, `backend/.env`, or `backend/hyperagent` (already gitignored).

---

### Task 1: Fix `handleMarkets` — visualized watchlist + position field

**Files:**
- Modify: `backend/internal/api/read.go:12-47`
- Test: `backend/internal/api/read_test.go`

**Interfaces:**
- Modifies `marketEntry` (add `Position metrics.Position \`json:"position"\`` field).
- `handleMarkets` iterates `s.deps.Cfg.Markets.Visualized` instead of `s.deps.Cfg.Markets.Tracked`, and additionally sets `Position: s.deps.Store.Position(coin)` on each entry (existing `Store.Position` method, `backend/internal/store/store.go:258`, returns the zero value for a flat/unknown position — safe to always call).

- [ ] **Step 1:** In `read_test.go`, find the existing markets test fixture (it seeds `Cfg.Markets.Tracked` and asserts on tracked coins). Add a case: config with `Markets.Visualized = []string{"BTC","ETH"}` and `Markets.Tracked = []string{"BTC"}`, store seeded with bars for both BTC and ETH via `Store.PutBar`, then `GET /api/markets` → assert the response contains **both** BTC and ETH entries (not just BTC). Add a second assertion: seed `Store.PutPosition(metrics.Position{Coin:"BTC", Size: 1.5, MarkPrice: 100})`, assert the BTC entry's `Position.Size == 1.5`.
- [ ] **Step 2:** Run: `cd backend && go test ./internal/api/ -run TestHandleMarkets -v`
      Expected: FAIL (ETH missing from response; `Position` field doesn't exist / is zero).
- [ ] **Step 3:** Implement: add the `Position` field to `marketEntry`; change the loop variable from `s.deps.Cfg.Markets.Tracked` to `s.deps.Cfg.Markets.Visualized`; add `Position: s.deps.Store.Position(coin)` to the constructed `marketEntry`.
- [ ] **Step 4:** Run: `cd backend && go test ./internal/api/ -v`
      Expected: PASS, all existing and new cases.
- [ ] **Step 5:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/internal/api/read.go backend/internal/api/read_test.go
git commit -m "fix(api): /api/markets serves the visualized watchlist, not just tracked; adds position"
```

### Task 2: Watchlist control-plane endpoints

**Files:**
- Modify: `backend/internal/api/server.go` (`Deps` struct, `routes()`)
- Create: `backend/internal/api/watchlist.go`
- Test: `backend/internal/api/watchlist_test.go`

**Interfaces:**
- Adds to `Deps` (`backend/internal/api/server.go:25`):
```go
type Deps struct {
    Bus      *bus.Bus
    Store    *store.Store
    Engine   *reasoner.Engine
    Exec     *executor.Executor
    Ingestor *ingestor.Ingestor // NEW
    Batcher  *batcher.Batcher   // NEW
    Cfg      config.Config
    Version  string
}
```
- Produces (`watchlist.go`):
```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

type subscribeRequest struct {
	Coins []string `json:"coins"`
}

// handleWatchlistSubscribe opens live feeds for new visualized coins.
func (s *Server) handleWatchlistSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.deps.Ingestor == nil {
		writeErr(w, http.StatusServiceUnavailable, "ingestor not configured")
		return
	}
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	s.deps.Ingestor.Subscribe(req.Coins...)
	w.WriteHeader(http.StatusNoContent)
}

type trackRequest struct {
	Coin      string `json:"coin"`
	Timeframe string `json:"timeframe"`
}

// handleWatchlistTrack adds a coin to the batcher's reasoned-over set.
// RequiresConfirmation reads Exec.Mode() live (nil Exec => always confirm)
// rather than a snapshot taken once at startup, so a coin tracked after a
// live mode switch never carries a stale confirm flag.
func (s *Server) handleWatchlistTrack(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req trackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Coin == "" || req.Timeframe == "" {
		writeErr(w, http.StatusBadRequest, "coin and timeframe are required")
		return
	}
	confirm := true
	if s.deps.Exec != nil {
		confirm = s.deps.Exec.Mode() != "autonomous"
	}
	s.deps.Batcher.Track(metrics.AssetStrategy{
		Coin:                 req.Coin,
		Timeframe:            req.Timeframe,
		RequiresConfirmation: confirm,
		MaxPositionUSD:       s.deps.Cfg.Execution.MaxPositionUSD,
	})
	w.WriteHeader(http.StatusNoContent)
}

type untrackRequest struct {
	Coin string `json:"coin"`
}

func (s *Server) handleWatchlistUntrack(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req untrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Coin == "" {
		writeErr(w, http.StatusBadRequest, "coin is required")
		return
	}
	s.deps.Batcher.Untrack(req.Coin)
	w.WriteHeader(http.StatusNoContent)
}

type scanRequest struct {
	Coins []string `json:"coins"`
}

func (s *Server) handleWatchlistScan(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req scanRequest
	// Empty/missing body is valid: it means "scan everything tracked."
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.deps.Batcher.Scan(req.Coins...)
	w.WriteHeader(http.StatusNoContent)
}
```
- Modify `routes()` (`backend/internal/api/server.go:85-99`) to add:
```go
	s.mux.HandleFunc("POST /api/watchlist/subscribe", s.handleWatchlistSubscribe)
	s.mux.HandleFunc("POST /api/watchlist/track", s.handleWatchlistTrack)
	s.mux.HandleFunc("POST /api/watchlist/untrack", s.handleWatchlistUntrack)
	s.mux.HandleFunc("POST /api/watchlist/scan", s.handleWatchlistScan)
```

- [ ] **Step 1:** Write `watchlist_test.go` with these cases (follow the `httptest.NewServer(NewServer(deps).Handler())` pattern from `act_test.go`):
  - `subscribe`: nil `Ingestor` → 503; real `ingestor.New("", nil, bus.New())` in `Deps.Ingestor`, POST `{"coins":["BTC"]}` → 204, then assert via a second `Subscribe` call idempotency isn't tested here (covered by `ingestor`'s own tests) — just assert 204 and that the call didn't panic.
  - `track`: nil `Batcher` → 503; real `batcher.New(bus.New(), storeFixture, nil, nil, 10)` in `Deps.Batcher`, POST `{"coin":"BTC","timeframe":"1h"}` → 204, then `Batcher.Tracked()` contains `"BTC"`. Repeat with `Deps.Exec` set to an executor in `"autonomous"` mode (build via the existing executor test harness) and assert the tracked strategy's `RequiresConfirmation == false`; with `Deps.Exec == nil`, assert `RequiresConfirmation == true`. Missing `coin` → 400.
  - `untrack`: seed a tracked coin, POST `{"coin":"BTC"}` → 204, `Batcher.Tracked()` no longer contains it.
  - `scan`: track two coins, POST `{}` (empty coins) → 204; assert (via a bus subscription to digests, `bus.SubscribeDigests`) that a digest is published per tracked coin whose ring has a bar.
- [ ] **Step 2:** Run: `cd backend && go test ./internal/api/ -run TestWatchlist -v`
      Expected: FAIL (package doesn't compile — `handleWatchlist*` undefined).
- [ ] **Step 3:** Implement `watchlist.go` and the `Deps`/`routes()` changes exactly as specified above.
- [ ] **Step 4:** Run: `cd backend && go test ./internal/api/... -v`
      Expected: PASS.
- [ ] **Step 5:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/internal/api/watchlist.go backend/internal/api/watchlist_test.go backend/internal/api/server.go
git commit -m "feat(api): watchlist control-plane endpoints (subscribe/track/untrack/scan)"
```

### Task 3: Settings & execution-mode endpoints

**Files:**
- Modify: `backend/internal/api/server.go` (`Deps.SaveConfig` field, `routes()`)
- Create: `backend/internal/api/settings.go`
- Test: `backend/internal/api/settings_test.go`
- Modify: `backend/src/main.go` (delete `buildProvider`, `providerCfgFor`, `setProviderKey`, `maskKey` — they move into `settings.go`; the `persist` closure in `buildControls` becomes `Deps.SaveConfig`, constructed inline in `run()`)

**Interfaces:**
- Adds to `Deps`:
```go
    SaveConfig func(apply func(*config.Config)) error // NEW — guarded persist-to-disk
```
- Produces (`settings.go`):
```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

type settingsResponse struct {
	Mode           string              `json:"mode"`
	Batch          roleSettings        `json:"batch"`
	Chat           roleSettings        `json:"chat"`
	ProviderNames  []string            `json:"provider_names"`
	ProviderModels map[string][]string `json:"provider_models"`
	KeyHints       map[string]string   `json:"key_hints"`
	// Visualized/Tracked/Timeframes let a client with no local config.toml
	// (the standalone TUI) bootstrap its initial watchlist and per-coin
	// timeframe at startup — the same three things tui.Config used to read
	// straight from cfg.Markets/cfg.Timeframe before the TUI moved out of
	// process.
	Visualized []string          `json:"visualized"`
	Tracked    []string          `json:"tracked"`
	Timeframes map[string]string `json:"timeframes"` // coin -> configured display tf
	Risk       riskSettings      `json:"risk"`
}

// riskSettings mirrors the TUI's read-only risk display (tui.RiskView),
// sourced from cfg.Execution — static per daemon run, no live-mutation
// endpoint exists for these today (nor did one exist for the embedded TUI).
type riskSettings struct {
	MaxPositionUSD      float64 `json:"max_position_usd"`
	MaxTotalExposureUSD float64 `json:"max_total_exposure_usd"`
	MaxConcurrent       int     `json:"max_concurrent"`
	DailyLossKillUSD    float64 `json:"daily_loss_kill_usd"`
}

type roleSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	reg := s.deps.Engine.Registry()
	batchP, batchM := reg.Active(reasoner.RoleBatch)
	chatP, chatM := reg.Active(reasoner.RoleChat)
	mode := s.deps.Cfg.Execution.Mode
	if s.deps.Exec != nil {
		mode = s.deps.Exec.Mode()
	}
	hints := map[string]string{}
	for _, name := range reg.Names() {
		if pc, ok := providerCfgFor(s.deps.Cfg, name); ok {
			hints[name] = maskKey(pc.Key(strings.ToUpper(name) + "_API_KEY"))
		}
	}
	tfs := make(map[string]string, len(s.deps.Cfg.Markets.Visualized))
	for _, coin := range s.deps.Cfg.Markets.Visualized {
		tfs[coin] = s.deps.Cfg.Timeframe.For(coin)
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		Mode:           mode,
		Batch:          roleSettings{batchP, batchM},
		Chat:           roleSettings{chatP, chatM},
		ProviderNames:  reg.Names(),
		ProviderModels: reg.ProviderModels(),
		KeyHints:       hints,
		Visualized:     s.deps.Cfg.Markets.Visualized,
		Tracked:        s.deps.Cfg.Markets.Tracked,
		Timeframes:     tfs,
		Risk: riskSettings{
			MaxPositionUSD:      s.deps.Cfg.Execution.MaxPositionUSD,
			MaxTotalExposureUSD: s.deps.Cfg.Execution.MaxTotalExposureUSD,
			MaxConcurrent:       s.deps.Cfg.Execution.MaxConcurrent,
			DailyLossKillUSD:    s.deps.Cfg.Execution.DailyLossKillUSD,
		},
	})
}

type putSettingsRequest struct {
	ChatProvider  string `json:"chat_provider"`
	ChatModel     string `json:"chat_model"`
	BatchProvider string `json:"batch_provider"`
	BatchModel    string `json:"batch_model"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	var req putSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	reg := s.deps.Engine.Registry()
	if req.ChatProvider != "" {
		if err := reg.SetProvider(reasoner.RoleChat, req.ChatProvider); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
	}
	if req.ChatModel != "" {
		if err := reg.SetModel(reasoner.RoleChat, req.ChatModel); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
	}
	if req.BatchProvider != "" {
		if err := reg.SetProvider(reasoner.RoleBatch, req.BatchProvider); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
	}
	if req.BatchModel != "" {
		if err := reg.SetModel(reasoner.RoleBatch, req.BatchModel); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
	}
	if s.deps.SaveConfig != nil {
		chatP, chatM := reg.Active(reasoner.RoleChat)
		batchP, batchM := reg.Active(reasoner.RoleBatch)
		_ = s.deps.SaveConfig(func(c *config.Config) {
			c.Reasoner.ChatProvider, c.Reasoner.ChatModel = chatP, chatM
			c.Reasoner.BatchProvider, c.Reasoner.BatchModel = batchP, batchM
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

type putModeRequest struct {
	Mode string `json:"mode"`
}

func (s *Server) handlePutMode(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "executor not configured")
		return
	}
	var req putModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if err := s.deps.Exec.SetMode(req.Mode); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
		return
	}
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) { c.Execution.Mode = req.Mode })
	}
	w.WriteHeader(http.StatusNoContent)
}

type putKeyRequest struct {
	Key string `json:"key"`
}

func (s *Server) handlePutProviderKey(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	name := r.PathValue("name")
	pc, ok := providerCfgFor(s.deps.Cfg, name)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown provider %q", name)
		return
	}
	var req putKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if err := s.deps.Engine.Registry().Replace(name, buildProvider(name, pc, req.Key)); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
		return
	}
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) { setProviderKey(c, name, req.Key) })
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers moved from backend/src/main.go: these were the TUI settings
// modal's plumbing; now the API server's, since it owns settings persistence.

func providerCfgFor(cfg config.Config, name string) (config.ProviderCfg, bool) {
	switch name {
	case "anthropic":
		return cfg.Providers.Anthropic, true
	case "openai":
		return cfg.Providers.OpenAI, true
	case "deepseek":
		return cfg.Providers.Deepseek, true
	}
	pc, ok := cfg.Providers.Custom[name]
	return pc, ok
}

func setProviderKey(c *config.Config, name, key string) {
	switch name {
	case "anthropic":
		c.Providers.Anthropic.APIKey = key
	case "openai":
		c.Providers.OpenAI.APIKey = key
	case "deepseek":
		c.Providers.Deepseek.APIKey = key
	default:
		if c.Providers.Custom == nil {
			c.Providers.Custom = map[string]config.ProviderCfg{}
		}
		pc := c.Providers.Custom[name]
		pc.APIKey = key
		c.Providers.Custom[name] = pc
	}
}

func buildProvider(name string, pc config.ProviderCfg, key string) reasoner.Provider {
	if name == "anthropic" || pc.Kind == "anthropic" {
		return reasoner.NewAnthropic(key, pc.Model, pc.BaseURL)
	}
	return reasoner.NewOpenAICompatible(name, key, pc.Model, pc.BaseURL)
}

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "•••"
	}
	return k[:6] + "…" + k[len(k)-4:]
}
```
- Modify `routes()` to add:
```go
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.mux.HandleFunc("PUT /api/execution/mode", s.handlePutMode)
	s.mux.HandleFunc("PUT /api/providers/{name}/key", s.handlePutProviderKey)
```

- [ ] **Step 1:** Write `settings_test.go`:
  - `GET /api/settings`: nil `Engine` → 503; real `Engine` (fake `reasoner.Provider`, see `engine_test.go`'s pattern) with two providers registered, `Deps.Cfg.Markets = config.Markets{Visualized: []string{"BTC","ETH"}, Tracked: []string{"BTC"}}`, `Deps.Cfg.Execution.MaxPositionUSD = 5000` → 200, body has `provider_names` containing both, `batch`/`chat` reflecting the registry's `Active()`, `visualized == ["BTC","ETH"]`, `tracked == ["BTC"]`, `timeframes` has an entry for both BTC and ETH, `risk.max_position_usd == 5000`.
  - `PUT /api/settings`: switch `chat_model` only → 204, then `GET /api/settings` reflects the new model, batch role untouched; unknown `chat_provider` → 422.
  - `PUT /api/execution/mode`: nil `Exec` → 503; real `Exec` in propose mode, PUT `{"mode":"autonomous"}` with no signer configured → 422 (the existing `SetMode` guard); PUT `{"mode":"propose"}` → 204.
  - `PUT /api/providers/{name}/key`: unknown provider name → 404; known provider, PUT `{"key":"sk-test"}` → 204, then `GET /api/settings`'s `key_hints[name]` is non-empty and does not contain the literal raw key.
- [ ] **Step 2:** Run: `cd backend && go test ./internal/api/ -run TestSettings -v`
      Expected: FAIL (package doesn't compile).
- [ ] **Step 3:** Implement `settings.go` and the `Deps`/`routes()` changes exactly as specified above.
- [ ] **Step 4:** Run: `cd backend && go test ./internal/api/... -v`
      Expected: PASS.
- [ ] **Step 5:** Remove `buildProvider`, `providerCfgFor`, `setProviderKey`, `maskKey` from `backend/src/main.go` (they now live in `settings.go`); `go build ./...` will fail here until Task 5 finishes rewiring `main.go` — that's expected and fine, this task's own package (`internal/api`) already builds and tests green in isolation.
- [ ] **Step 6:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/internal/api/settings.go backend/internal/api/settings_test.go backend/internal/api/server.go backend/src/main.go
git commit -m "feat(api): settings, execution-mode, and provider-key endpoints"
```

### Task 4: Thesis passthrough endpoint

**Files:**
- Create: `backend/internal/api/thesis.go`
- Test: `backend/internal/api/thesis_test.go`
- Modify: `backend/internal/api/server.go` (`Deps.RestClient` field, `routes()`)

**Interfaces:**
- Adds to `Deps`:
```go
    RestClient *hlclient.Client // NEW — for GET /api/thesis passthrough
```
- Produces (`thesis.go`):
```go
package api

import (
	"net/http"

	"github.com/hyperagent/hyperagent/internal/thesis"
)

func (s *Server) handleThesis(w http.ResponseWriter, r *http.Request) {
	if s.deps.RestClient == nil {
		writeErr(w, http.StatusServiceUnavailable, "hl rest client not configured")
		return
	}
	coin := r.PathValue("coin")
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = s.deps.Cfg.Timeframe.For(coin)
	}
	ctx, err := thesis.FetchContext(r.Context(), s.deps.RestClient, coin, tf)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"context": ctx})
}
```
- Modify `routes()` to add: `s.mux.HandleFunc("GET /api/thesis/{coin}", s.handleThesis)`

- [ ] **Step 1:** Write `thesis_test.go`: nil `RestClient` → 503. For the happy path, stand up an `httptest.Server` that answers HL's `candleSnapshot`/`metaAndAssetCtxs` shapes (mirror the fixture already used in `backend/internal/hlclient`'s own tests, or `backend/internal/thesis` if it has one — check `thesis.go`'s existing tests first via `ls backend/internal/thesis/`; if none exist, build the fixture inline: a `httptest.NewServer` returning one candle and one asset ctx), point `hlclient.New(fixtureURL)` at it, assert `GET /api/thesis/BTC?tf=1h` → 200 with a `context` field containing `"BTC"`. No-data case (fixture returns empty candles) → 502.
- [ ] **Step 2:** Run: `cd backend && go test ./internal/api/ -run TestThesis -v`
      Expected: FAIL (package doesn't compile).
- [ ] **Step 3:** Implement `thesis.go` and the `Deps`/`routes()` change.
- [ ] **Step 4:** Run: `cd backend && go test ./internal/api/... -v`
      Expected: PASS.
- [ ] **Step 5:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/internal/api/thesis.go backend/internal/api/thesis_test.go backend/internal/api/server.go
git commit -m "feat(api): thesis passthrough endpoint for multi-TF HL context"
```

### Task 5: Rewire `backend/src/main.go` — drop the embedded TUI

**Files:**
- Modify: `backend/src/main.go`

**Interfaces:**
- Consumes: `api.Deps` as it now stands after Tasks 2-4 (`Ingestor`, `Batcher`, `SaveConfig`, `RestClient` fields added).
- No new exported interfaces — this task only removes code and rewires construction.

- [ ] **Step 1:** In `run()`, delete the `if headless { return runHeadless(...) }` branch and the `controls := buildControls(...)` / `return runTUI(...)` tail; instead, after starting the pipeline goroutines (the existing `go in.Run(ctx)` etc. block), block on context the way `runHeadless` did:
```go
	<-ctx.Done()
	return nil
```
- [ ] **Step 2:** Delete `buildControls`, `runTUI`, `runHeadless`, `redrawMsg`, and the `tui` import. Delete the `headless` flag from `main()`'s `flag.String`/`flag.Bool` block and its parameter from `run()`'s signature (and its one call site in `main()`).
- [ ] **Step 3:** In the `api.NewServer(api.Deps{...})` construction, add the four new fields: `Ingestor: in`, `Batcher: bt`, `RestClient: rest`, and:
```go
		SaveConfig: func(apply func(*config.Config)) error {
			cfgMu.Lock()
			defer cfgMu.Unlock()
			apply(&cfg)
			return config.Save(configPath, cfg)
		},
```
  (this requires hoisting a `var cfgMu sync.Mutex` to the same scope as the `api.NewServer` call — it replaces the one that lived inside `buildControls`).
- [ ] **Step 4:** Run: `cd backend && go build ./... && go vet ./...`
      Expected: clean. (`internal/tui` may now be unused by `main.go` but still compiles standalone until Task 8 deletes it — that's fine.)
- [ ] **Step 5:** Run: `cd backend && go test ./... -race`
      Expected: PASS (all existing suites, since no package's public behavior changed, only `main.go`'s wiring).
- [ ] **Step 6:** Manual smoke: `cd backend && go build -o hyperagent ./src && ./hyperagent -testnet &`, then:
```bash
curl -s localhost:8787/api/health
curl -s -X POST localhost:8787/api/watchlist/scan -d '{}'
curl -s localhost:8787/api/settings
kill %1
```
  Expected: `/api/health` returns JSON with `"connected"`; `/api/watchlist/scan` returns empty body with HTTP 204; `/api/settings` returns JSON with `mode`/`batch`/`chat`; the daemon exits cleanly on `kill` (no goroutine panic in stderr).
- [ ] **Step 7:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/src/main.go
git commit -m "refactor(backend): drop embedded TUI and -headless mode — daemon always runs pipeline+API"
```

### Task 6: Scaffold the `tui/` module and `apiclient.Client`

**Files:**
- Create: `tui/go.mod`
- Create: `tui/internal/apiclient/types.go`
- Create: `tui/internal/apiclient/client.go`
- Test: `tui/internal/apiclient/client_test.go`

**Interfaces:**
- Produces (`types.go`) — hand-derived wire types, one doc comment per type naming its backend source, mirroring `dashboard/src/lib/core-client.ts`'s convention:
```go
// Package apiclient is the tui module's only dependency on backend/: a thin
// HTTP+WS client. No import of backend/internal/* — every wire type here is
// hand-derived from the JSON the daemon's API actually produces.
package apiclient

import "time"

// Bar mirrors backend/internal/metrics.Bar (untagged Go struct — wire fields
// are the exported Go names verbatim; Open/CloseTime marshal as RFC3339).
type Bar struct {
	Coin        string
	Timeframe   string
	OpenTime    time.Time
	CloseTime   time.Time
	Open, High, Low, Close float64
	Volume      float64
	TradeCount  int
	Final       bool
	Return      float64
	Funding     float64
	OIDelta     float64
	RelStrength float64
}

// AssetCtx mirrors backend/internal/metrics.AssetCtx.
type AssetCtx struct {
	Coin         string
	MarkPrice    float64
	OraclePrice  float64
	Funding      float64
	OpenInterest float64
	Premium      float64
	DayVolume    float64
	Time         time.Time
}

// Position mirrors backend/internal/metrics.Position.
type Position struct {
	Coin      string
	Size      float64
	MarkPrice float64
}

func (p Position) IsFlat() bool { return p.Size == 0 }

// MarketEntry mirrors the marketEntry JSON shape of GET /api/markets
// (backend/internal/api/read.go).
type MarketEntry struct {
	Coin     string   `json:"coin"`
	Bar      Bar      `json:"bar"`
	Mid      float64  `json:"mid"`
	AssetCtx AssetCtx `json:"asset_ctx"`
	Position Position `json:"position"`
}

// Verdict mirrors backend/internal/metrics.Verdict.
type Verdict struct {
	Asset                string
	Action               string
	Thesis               string
	Confidence           float64
	SizeUSD              float64
	Stop, TakeProfit     float64
	Entry                Entry
	RequiresConfirmation bool
}

// Entry mirrors backend/internal/metrics.Entry.
type Entry struct {
	Type  string
	Price float64
}

// ChatTurn mirrors backend/internal/reasoner.ChatTurn (json tags role/text).
type ChatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// SettingsResponse mirrors GET /api/settings's body.
type SettingsResponse struct {
	Mode           string              `json:"mode"`
	Batch          RoleSettings        `json:"batch"`
	Chat           RoleSettings        `json:"chat"`
	ProviderNames  []string            `json:"provider_names"`
	ProviderModels map[string][]string `json:"provider_models"`
	KeyHints       map[string]string   `json:"key_hints"`
	Visualized     []string            `json:"visualized"`
	Tracked        []string            `json:"tracked"`
	Timeframes     map[string]string   `json:"timeframes"`
	Risk           RiskSettings        `json:"risk"`
}

type RoleSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// RiskSettings mirrors backend/internal/api/settings.go's riskSettings.
type RiskSettings struct {
	MaxPositionUSD      float64 `json:"max_position_usd"`
	MaxTotalExposureUSD float64 `json:"max_total_exposure_usd"`
	MaxConcurrent       int     `json:"max_concurrent"`
	DailyLossKillUSD    float64 `json:"daily_loss_kill_usd"`
}
```
- Produces (`client.go`):
```go
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is a thin HTTP client for the backend daemon's control-plane API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client against baseURL (e.g. "http://127.0.0.1:8787").
// token is sent as "Authorization: Bearer <token>" when non-empty.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var errBody struct {
			Error string `json:"error"`
		}
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		_ = json.Unmarshal(buf, &errBody)
		if errBody.Error != "" {
			return fmt.Errorf("%s", errBody.Error)
		}
		return fmt.Errorf("request failed: status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Subscribe(ctx context.Context, coins ...string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/subscribe", map[string]any{"coins": coins}, nil)
}

func (c *Client) Track(ctx context.Context, coin, timeframe string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/track", map[string]any{"coin": coin, "timeframe": timeframe}, nil)
}

func (c *Client) Untrack(ctx context.Context, coin string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/untrack", map[string]any{"coin": coin}, nil)
}

func (c *Client) Scan(ctx context.Context, coins ...string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/scan", map[string]any{"coins": coins}, nil)
}

func (c *Client) SetMode(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPut, "/api/execution/mode", map[string]any{"mode": mode}, nil)
}

func (c *Client) Settings(ctx context.Context) (SettingsResponse, error) {
	var out SettingsResponse
	err := c.do(ctx, http.MethodGet, "/api/settings", nil, &out)
	return out, err
}

func (c *Client) SaveSettings(ctx context.Context, chatProvider, chatModel, batchProvider, batchModel string) error {
	return c.do(ctx, http.MethodPut, "/api/settings", map[string]any{
		"chat_provider": chatProvider, "chat_model": chatModel,
		"batch_provider": batchProvider, "batch_model": batchModel,
	}, nil)
}

func (c *Client) SetProviderKey(ctx context.Context, name, key string) error {
	return c.do(ctx, http.MethodPut, "/api/providers/"+name+"/key", map[string]any{"key": key}, nil)
}

func (c *Client) Thesis(ctx context.Context, coin, tf string) (string, error) {
	var out struct {
		Context string `json:"context"`
	}
	err := c.do(ctx, http.MethodGet, "/api/thesis/"+coin+"?tf="+tf, nil, &out)
	return out.Context, err
}

func (c *Client) Chat(ctx context.Context, message string, history []ChatTurn) (reply, provider, model string, err error) {
	var out struct {
		Reply    string `json:"reply"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	err = c.do(ctx, http.MethodPost, "/api/chat", map[string]any{"message": message, "history": history}, &out)
	return out.Reply, out.Provider, out.Model, err
}

func (c *Client) Markets(ctx context.Context) ([]MarketEntry, error) {
	var out []MarketEntry
	err := c.do(ctx, http.MethodGet, "/api/markets", nil, &out)
	return out, err
}

func (c *Client) Bars(ctx context.Context, coin, tf string, n int) ([]Bar, error) {
	var out []Bar
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/bars/%s?tf=%s&n=%d", coin, tf, n), nil, &out)
	return out, err
}
```

- [ ] **Step 1:** Create `tui/go.mod`:
```
module github.com/hyperagent/tui

go 1.23
```
  (match the Go version pinned in `backend/go.mod` — check it first with `head -3 /home/athan/projects/hypertrader/backend/go.mod` and use the same version.)
- [ ] **Step 2:** Write `client_test.go` against an `httptest.Server`: for each `Client` method, register a handler asserting the request method/path/body match what's documented above, and returning a canned response; assert the method decodes it correctly. Include one error case: handler returns `422 {"error":"unknown provider"}`, assert `SetProviderKey` returns an error with that exact message.
- [ ] **Step 3:** Run: `cd tui && go mod init 2>/dev/null; go test ./internal/apiclient/... -v`
      Expected: FAIL (package doesn't exist yet).
- [ ] **Step 4:** Implement `types.go` and `client.go` exactly as specified above. Run `cd tui && go get github.com/gorilla/websocket` is NOT needed yet (that's Task 7's bridge) — this task only needs stdlib.
- [ ] **Step 5:** Run: `cd tui && go build ./... && go test ./internal/apiclient/... -v`
      Expected: PASS.
- [ ] **Step 6:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add tui/go.mod tui/go.sum tui/internal/apiclient/types.go tui/internal/apiclient/client.go tui/internal/apiclient/client_test.go
git commit -m "feat(tui): new standalone module scaffold + HTTP control-plane client"
```

### Task 7: `apiclient.Cache` — client-side read mirror

**Files:**
- Create: `tui/internal/apiclient/cache.go`
- Test: `tui/internal/apiclient/cache_test.go`

**Interfaces:**
- Consumes: `Client.Markets`, `Client.Bars` (Task 6).
- Produces:
```go
package apiclient

import "sync"

// Cache is a client-side mirror of backend/internal/store.Store's read
// surface: same method names/signatures, fed over HTTP+WS instead of
// in-process. Model's render code calls these exactly as it called
// store.Store directly when the TUI shared a process with the daemon.
type Cache struct {
	mu    sync.RWMutex
	bars  map[string]map[string][]Bar // coin -> tf -> bars, oldest-first
	mids  map[string]float64
	ctxs  map[string]AssetCtx
	pos   map[string]Position
}

func NewCache() *Cache {
	return &Cache{
		bars: make(map[string]map[string][]Bar),
		mids: make(map[string]float64),
		ctxs: make(map[string]AssetCtx),
		pos:  make(map[string]Position),
	}
}

func (c *Cache) LatestBar(coin, tf string) (Bar, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	series := c.bars[coin][tf]
	if len(series) == 0 {
		return Bar{}, false
	}
	return series[len(series)-1], true
}

func (c *Cache) History(coin, tf string, n int) []Bar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	series := c.bars[coin][tf]
	if n <= 0 || n >= len(series) {
		return append([]Bar(nil), series...)
	}
	return append([]Bar(nil), series[len(series)-n:]...)
}

func (c *Cache) Mid(coin string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mids[coin]
}

func (c *Cache) AssetCtx(coin string) (AssetCtx, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ctx, ok := c.ctxs[coin]
	return ctx, ok
}

func (c *Cache) Position(coin string) Position {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pos[coin]
}

// PutBar appends/replaces a bar in its (coin, tf) series, keyed by CloseTime —
// a re-published in-progress bar for the same close replaces the prior entry
// rather than duplicating it, mirroring store.Store.PutBar's ring semantics.
func (c *Cache) PutBar(b Bar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bars[b.Coin] == nil {
		c.bars[b.Coin] = make(map[string][]Bar)
	}
	series := c.bars[b.Coin][b.Timeframe]
	if n := len(series); n > 0 && series[n-1].CloseTime.Equal(b.CloseTime) {
		series[n-1] = b
	} else {
		series = append(series, b)
		if len(series) > 512 { // matches backend's default ring_size
			series = series[len(series)-512:]
		}
	}
	c.bars[b.Coin][b.Timeframe] = series
}

func (c *Cache) PutMid(coin string, px float64) {
	c.mu.Lock()
	c.mids[coin] = px
	c.mu.Unlock()
}

// ApplyMarkets overwrites AssetCtx/Position (and seeds Mid) from a full
// GET /api/markets snapshot — the periodic-poll refresh path.
func (c *Cache) ApplyMarkets(entries []MarketEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		c.ctxs[e.Coin] = e.AssetCtx
		c.pos[e.Coin] = e.Position
		if e.Mid != 0 {
			c.mids[e.Coin] = e.Mid
		}
	}
}

// SeedHistory installs a full backfilled series for (coin, tf), oldest-first —
// called once after a successful GET /api/bars/{coin} on /watch.
func (c *Cache) SeedHistory(coin, tf string, bars []Bar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bars[coin] == nil {
		c.bars[coin] = make(map[string][]Bar)
	}
	c.bars[coin][tf] = append([]Bar(nil), bars...)
}
```

- [ ] **Step 1:** Write `cache_test.go`: `PutBar` twice with the same `CloseTime` replaces in place (assert `len(History(...)) == 1` after both, with the second bar's values); `PutBar` with distinct `CloseTime`s appends, `LatestBar` returns the newest; `History(coin, tf, 2)` on a 5-bar series returns exactly the last 2, oldest-first; `ApplyMarkets` sets `AssetCtx`/`Position`/`Mid` for coins present in the slice and leaves untouched coins alone; `SeedHistory` overwrites any prior series for that (coin, tf) wholesale (assert calling it twice with different data leaves only the second).
- [ ] **Step 2:** Run: `cd tui && go test ./internal/apiclient/ -run TestCache -v`
      Expected: FAIL (`Cache` undefined).
- [ ] **Step 3:** Implement `cache.go` exactly as specified above.
- [ ] **Step 4:** Run: `cd tui && go test ./internal/apiclient/... -v`
      Expected: PASS.
- [ ] **Step 5:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add tui/internal/apiclient/cache.go tui/internal/apiclient/cache_test.go
git commit -m "feat(tui): client-side read cache mirroring store.Store's surface"
```

### Task 8: Move the Bubble Tea UI into `tui/internal/tui`

**Files:**
- Create: `tui/internal/tui/*.go` (moved from `backend/internal/tui/*.go`)
- Delete: `backend/internal/tui/` (entire directory, after the move)
- Modify (post-move, in their new location): `tui/internal/tui/model.go`, `tui/internal/tui/markets.go`, `tui/internal/tui/detail.go`, `tui/internal/tui/signalview.go`, `tui/internal/tui/commands.go`, `tui/internal/tui/settings.go`

**Interfaces:**
- `Model` (`model.go`) changes:
```go
type Model struct {
	theme    Theme
	cache    *apiclient.Cache   // was: store *store.Store
	chatFn   ChatFunc           // signature updated below
	controls *apiclient.Client  // was: controls Controls
	risk     RiskView
	// ... all other fields unchanged
}

// ChatFunc now returns provider/model too since apiclient.Client.Chat does;
// callers that only need the reply text can discard the extra returns.
type ChatFunc func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (reply string, err error)

// ThesisFn now hits the backend's passthrough endpoint instead of an
// in-process hlclient.Client.
type ThesisFn func(ctx context.Context, coin, displayTF string) (string, error)
```
- The `Controls` struct type itself is deleted — every call site that did `m.controls.Track(coin, tf)` etc. becomes `m.controls.Track(context.Background(), coin, tf)` (the `apiclient.Client` methods take a `context.Context` first argument; the embedded `Controls` closures didn't). Same for `Subscribe`, `Untrack`, `ScanNow`→`Scan`, `SetMode`, `SetProvider`→ no longer called directly (goes through `SaveSettings`/`Settings` — see below), `SaveSettings`, `SetAPIKey`→`SetProviderKey`, `KeyHint`→read from the last-fetched `apiclient.SettingsResponse` cached on `Model` instead of a per-call closure (add a `settings apiclient.SettingsResponse` field to `Model`, refreshed whenever `settings.go`'s modal opens via a new `tea.Cmd` that calls `controls.Settings(ctx)`).
- The existing `tui.Config` struct (the argument to `tui.New`, previously populated in `main.go`'s `runTUI` from `cfg.Markets.Visualized/Tracked`, `cfg.Timeframe`, `cfg.Execution.Mode`, `engine.ChatProviderName()`, and `cfg.Storage.Dir+"/watchlist.json"`) changes to:
```go
type Config struct {
	Theme    Theme
	Cache    *apiclient.Cache
	Controls *apiclient.Client
	Settings apiclient.SettingsResponse // seeds Visualized/Tracked/Timeframes/Mode/Chat.Provider/Risk — the config.toml-backed fields tui.Config used to take directly
	ChatFn   ChatFunc
	ThesisFn ThesisFn
}
```
  `New(cfg Config)` reads `cfg.Settings.Visualized`/`.Tracked`/`.Timeframes`/`.Mode`/`.Chat.Provider`/`.Risk` wherever the old constructor read the corresponding `cfg.Markets`/`cfg.Timeframe`/`cfg.Execution`/`engine` values (the old standalone `Risk RiskView` constructor field is gone — `RiskView` itself, the display struct `settings.go` already renders from, stays as a type but is now populated from `cfg.Settings.Risk` inside `New`, not passed in separately). `Model` additionally keeps the full `settings apiclient.SettingsResponse` (not just the fields it seeded from) so the settings modal has provider/model lists without a separate fetch on first open.
- `WatchlistPath` (local `watchlist.json` persistence for TUI-side filter/sort state) is dropped, not migrated — the standalone TUI has no local data directory of its own by design (it "holds no backend state of its own," per its new package doc comment in Task 9). Filter/sort/selection state resets on TUI restart; this is a deliberate, minor scope trim, not an oversight.
- Every `m.store.X(...)` call in `markets.go`, `detail.go`, `signalview.go` becomes `m.cache.X(...)` — same method names (`LatestBar`, `History`, `Mid`, `AssetCtx`, `Position`), same signatures, per Task 7's `Cache` design — so only the receiver changes, not the surrounding logic.
- `metrics.Bar`/`metrics.AssetCtx`/`metrics.Position` references throughout become `apiclient.Bar`/`apiclient.AssetCtx`/`apiclient.Position`.

- [ ] **Step 1:** `git mv backend/internal/tui tui/internal/tui` (from repo root) — this preserves file history in one move, git will detect it as a rename once committed even though it's crossing into a new-module directory (module boundary doesn't matter to git, only to Go).
- [ ] **Step 2:** In every moved file, replace the import `"github.com/hyperagent/hyperagent/internal/store"` and any `metrics`/`reasoner`/`bus` imports that are no longer needed with `"github.com/hyperagent/tui/internal/apiclient"` where those types are used. Concretely: grep each file for `store\.`, `metrics\.`, `bus\.`, `reasoner\.Verdict`, `reasoner\.ChatTurn` and replace per the type-mapping table below; delete now-unused imports.

| Old (backend internal) | New (`apiclient`) |
|---|---|
| `*store.Store` (field type) | `*apiclient.Cache` |
| `metrics.Bar` | `apiclient.Bar` |
| `metrics.AssetCtx` | `apiclient.AssetCtx` |
| `metrics.Position` | `apiclient.Position` |
| `reasoner.Verdict` | `apiclient.Verdict` |
| `reasoner.ChatTurn` | `apiclient.ChatTurn` |
| `bus.StatusEvent`, `bus.StatusConn`, `bus.JournalEvent` | see Task 9 (bridge.go rewrite defines local message types) |

- [ ] **Step 3:** Apply the `Model` struct changes and `Controls`-deletion call-site updates described above across `model.go`, `commands.go`, `settings.go`.
- [ ] **Step 4:** `cd tui && go build ./...` — iterate until it compiles. This will surface every remaining backend-internal reference as a compile error; fix each per the mapping table. Do not add a `replace` directive pointing back at `backend/` — a successful build with zero import of `github.com/hyperagent/hyperagent/*` is the actual acceptance criterion for "separate module."
- [ ] **Step 5:** Run: `cd tui && go test ./internal/tui/... -v`
      Expected: the moved test files (`commands_test.go`, `ideas_test.go`, `layout_test.go`, `markdown_test.go`, `render_test.go`, `settings_test.go`, `smoke_visual_test.go`) PASS with no logic changes — only fixture types swapped from `metrics.Bar{...}`/`store.New(...)` to `apiclient.Bar{...}`/`apiclient.NewCache()` per the same mapping table.
- [ ] **Step 6:** `rm -rf backend/internal/tui` if `git mv` in Step 1 left anything behind (it shouldn't — verify with `git status` that `backend/internal/tui` no longer appears and `tui/internal/tui` does, both as tracked changes).
- [ ] **Step 7:** `cd backend && go build ./...` — confirm the backend module still builds with `internal/tui` gone (it should: Task 5 already removed the only import of it).
- [ ] **Step 8:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add -A -- backend/internal/tui tui/internal/tui
git commit -m "refactor(tui): move Bubble Tea UI into standalone tui/ module, depend on apiclient not backend internals"
```

### Task 9: Rewrite `bridge.go` as a WS client; new `tui/src/main.go`

**Files:**
- Modify: `tui/internal/tui/bridge.go` (moved in Task 8, rewritten here)
- Create: `tui/src/main.go`
- Test: `tui/internal/tui/bridge_test.go`

**Interfaces:**
- Produces (`bridge.go`):
```go
package tui

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"
	"github.com/hyperagent/tui/internal/apiclient"
)

// wsFrame mirrors the {"topic":...,"data":...} envelope backend/internal/api/ws.go writes.
type wsFrame struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// PumpWS connects to the daemon's /api/ws, applies bar/mids updates directly
// to cache, and forwards verdict/journal/status frames into the Bubble Tea
// program as the same message types Update already switches on. Reconnects
// with capped exponential backoff on any read/dial error; blocks until ctx
// is cancelled.
func PumpWS(ctx context.Context, wsURL string, cache *apiclient.Cache, p *tea.Program) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		readLoop(ctx, conn, cache, p)
		conn.Close()
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, cache *apiclient.Cache, p *tea.Program) {
	for {
		if ctx.Err() != nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var f wsFrame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.Topic {
		case "bar":
			var b apiclient.Bar
			if json.Unmarshal(f.Data, &b) == nil {
				cache.PutBar(b)
				p.Send(barMsg{})
			}
		case "mids":
			var m struct{ Mids map[string]float64 }
			if json.Unmarshal(f.Data, &m) == nil {
				for coin, px := range m.Mids {
					cache.PutMid(coin, px)
				}
			}
		case "verdict":
			var v apiclient.Verdict
			if json.Unmarshal(f.Data, &v) == nil {
				p.Send(verdictMsg(v))
			}
		case "journal":
			var e journalMsg
			if json.Unmarshal(f.Data, &e) == nil {
				p.Send(e)
			}
		case "status":
			var s statusMsg
			if json.Unmarshal(f.Data, &s) == nil {
				p.Send(s)
			}
		}
	}
}

func wsURLFrom(httpBaseURL string) string {
	u, err := url.Parse(httpBaseURL)
	if err != nil {
		log.Printf("tui: bad base url %q: %v", httpBaseURL, err)
		return httpBaseURL
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/ws"
	return u.String()
}
```
- Produces (`tui/src/main.go`):
```go
// Command hyperagent-tui is the standalone terminal client for the hyperagent
// daemon: it holds no backend state of its own, talking exclusively over
// HTTP+WS to a running daemon's unified core API.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hyperagent/tui/internal/apiclient"
	"github.com/hyperagent/tui/internal/tui"
)

func main() {
	coreURL := flag.String("core-url", "http://127.0.0.1:8787", "hyperagent daemon base URL")
	token := flag.String("token", os.Getenv("HYPERAGENT_TOKEN"), "bearer token, if the daemon requires one")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	client := apiclient.New(*coreURL, *token)
	cache := apiclient.NewCache()

	settings, err := client.Settings(ctx)
	if err != nil {
		log.Fatalf("hyperagent-tui: could not reach daemon at %s: %v", *coreURL, err)
	}

	// ChatFunc (Task 8) is (ctx, string, []ChatTurn) (string, error) — only the
	// reply; provider/model come from the cached Settings, not per-call. This
	// wrapper adapts Client.Chat's 4-return signature to that shape.
	chatFn := func(ctx context.Context, msg string, history []apiclient.ChatTurn) (string, error) {
		reply, _, _, err := client.Chat(ctx, msg, history)
		return reply, err
	}

	hasDarkBG := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	model := tui.New(tui.Config{
		Theme:    tui.NewTheme(hasDarkBG),
		Cache:    cache,
		Controls: client,
		Settings: settings, // Task 8's tui.Config: seeds Visualized/Tracked/Timeframes/Mode/Chat.Provider/Risk from this one fetch
		ChatFn:   chatFn,
		ThesisFn: client.Thesis,
	})

	p := tea.NewProgram(model, tea.WithContext(ctx))
	go tui.PumpWS(ctx, wsURLFromFlag(*coreURL), cache, p)
	go tui.PollMarkets(ctx, client, cache, p)

	if _, err := p.Run(); err != nil {
		cancel()
		log.Fatalf("hyperagent-tui: %v", err)
	}
	cancel()
}

func wsURLFromFlag(base string) string {
	return base // tui.PumpWS derives the ws:// URL internally via wsURLFrom
}
```
- Adds `PollMarkets` to `bridge.go` (the Task 4/spec-mandated 5s poll refreshing `AssetCtx`/`Position`):
```go
// PollMarkets refreshes AssetCtx/Position for the whole visualized watchlist
// every 5s — there is no WS topic for either today. Blocks until ctx is done.
func PollMarkets(ctx context.Context, client *apiclient.Client, cache *apiclient.Cache, p *tea.Program) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			entries, err := client.Markets(ctx)
			if err == nil {
				cache.ApplyMarkets(entries)
				p.Send(barMsg{})
			}
		}
	}
}
```

- [ ] **Step 1:** Run: `cd tui && go get github.com/gorilla/websocket && go get charm.land/bubbletea/v2 && go get charm.land/lipgloss/v2` — pin them to the same versions `backend/go.mod` uses (check with `grep -E 'bubbletea|lipgloss|gorilla/websocket' /home/athan/projects/hypertrader/backend/go.mod` and pass `@<version>` explicitly to each `go get`).
- [ ] **Step 2:** Write `bridge_test.go`: stand up an `httptest.NewServer` with a `websocket.Upgrader` handler that, on connect, writes one `bar` frame and one `status` frame, then closes. Run `PumpWS` in a goroutine with a `tea.Program` built from a minimal test `tea.Model` that records every `Msg` it receives into a channel; assert the channel receives a `barMsg` and a `statusMsg`, and that `cache.LatestBar(...)` reflects the pushed bar. Second test: dial a server address that immediately closes the connection, assert `PumpWS` retries (observe a second connection attempt on the test server within 3s using the first backoff step).
- [ ] **Step 3:** Run: `cd tui && go test ./internal/tui/ -run TestBridge -v`
      Expected: FAIL (`PumpWS` doesn't exist / old bus-based `bridge.go` doesn't compile against `apiclient`).
- [ ] **Step 4:** Implement the `bridge.go` rewrite and `PollMarkets` exactly as specified above.
- [ ] **Step 5:** Run: `cd tui && go test ./... -v`
      Expected: PASS, whole module.
- [ ] **Step 6:** Implement `tui/src/main.go` exactly as specified above. Note: `tui.New(tui.Config{...})`'s exact field names must match whatever `model.go` ended up with in Task 8 (`Cache`, `Controls`, `Settings`, `ChatFn`, `ThesisFn` — reconcile any naming drift from Task 8 here, in this task, since this is the first place they're all used together).
- [ ] **Step 7:** Run: `cd tui && go build -o hyperagent-tui ./src && go vet ./...`
      Expected: clean build.
- [ ] **Step 8:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add tui/internal/tui/bridge.go tui/internal/tui/bridge_test.go tui/src/main.go tui/go.mod tui/go.sum
git commit -m "feat(tui): WS bridge + standalone entrypoint — tui/ now builds and runs independently"
```

### Task 10: Full verification and docs

**Files:**
- Modify: `backend/README.md` (remove the embedded-TUI run instructions; point to `tui/`)
- Create: `tui/README.md` (mirror `backend/README.md`'s tone: what it is, how to run it against a daemon)

- [ ] **Step 1:** `cd backend && go build ./... && go vet ./... && go test ./... -race` → all PASS.
- [ ] **Step 2:** `cd tui && go build ./... && go vet ./... && go test ./... -race` → all PASS.
- [ ] **Step 3:** `cd dashboard && bun run build` → clean (confirms Task 1's `handleMarkets` change didn't break the dashboard's existing consumption — it shouldn't, since it's additive).
- [ ] **Step 4:** Manual end-to-end: `cd backend && go build -o hyperagent ./src && ./hyperagent -testnet &`, then `cd tui && go build -o hyperagent-tui ./src && ./hyperagent-tui`. In the running TUI: `/watch SOL` (a coin not in default `Tracked`) and confirm it appears in the markets table with live price updates within a few seconds; `/track SOL 1h` then `/scan`; open the settings modal and confirm provider/model lists populate; toggle mode; quit the TUI, confirm the daemon is still running (`curl localhost:8787/api/health`), then `kill %1`.
- [ ] **Step 5:** Write `tui/README.md` and update `backend/README.md`'s "Start it from the `backend/` directory" TUI section to instead say the TUI is a separate binary in `tui/`, run as `./hyperagent-tui -core-url http://127.0.0.1:8787`.
- [ ] **Step 6:** Commit:
```bash
cd /home/athan/projects/hypertrader
git add backend/README.md tui/README.md
git commit -m "docs: standalone tui/ module — README for both backend and tui"
```
