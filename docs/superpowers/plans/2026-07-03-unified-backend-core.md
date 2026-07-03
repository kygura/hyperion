# Unified Backend Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an HTTP + WebSocket API surface (`internal/api`) to the existing hyperagent daemon so any frontend (web dashboard, future clients) consumes the same core the TUI does: derived metrics, digests, theses, journal, agent chat, and propose/confirm execution.

**Architecture:** The API server is a new bus consumer embedded in the single Go binary, started from `run()` in `tui/src/main.go` for both TUI and headless modes. It subscribes to bus topics for caches + WS push, reads the store/journal for request endpoints, and calls the existing executor for the confirm flow. A new proposal registry in `internal/executor` unifies the Telegram and HTTP approve paths. Spec: `docs/superpowers/specs/2026-07-03-unified-backend-core-design.md`.

**Tech Stack:** Go stdlib `net/http` (mux via `http.ServeMux` with Go 1.22 method patterns), `gorilla/websocket` (already in go.mod), existing internal packages (`bus`, `store`, `journal`, `executor`, `reasoner`, `config`). No new dependencies.

## Global Constraints

- Module root: `/home/athan/projects/hypertrader/tui` (module `github.com/hyperagent/hyperagent`). All Go commands run there.
- Git repo root: `/home/athan/projects/hypertrader` (repo initialized 2026-07-03; commit from repo root).
- **No new Go dependencies.** stdlib + existing go.mod only.
- Default bind `127.0.0.1:8787`. Non-loopback `addr` with empty `token` MUST be a startup error.
- All error responses: `{"error":"..."}` JSON. Risk-gate rejections → HTTP 422.
- Follow existing code style: package doc comments explaining the *why*, table-driven tests, no interface{} where generics/concrete types work.
- Match existing test conventions (see `tui/internal/executor/executor_test.go`, `tui/internal/config/config_test.go`).

---

### Task 1: `[api]` config section

**Files:**
- Modify: `tui/internal/config/config.go` (add `API` struct + field on `Config`, defaults, validation)
- Test: `tui/internal/config/config_test.go` (append tests)

**Interfaces:**
- Produces: `config.API{Enabled bool; Addr string; Token string; CORSOrigins []string}` as `Config.API`, toml section `[api]`. Defaults: `Enabled=true`, `Addr="127.0.0.1:8787"`, `CORSOrigins=["http://localhost:5173"]`.
- Validation rule in `Load`/`applyDefaults` path: if `Enabled` and host of `Addr` is not a loopback address (use `net.SplitHostPort` + `net.ParseIP(...).IsLoopback()`, treating `localhost` as loopback) and `Token == ""` → error `"api: refusing to bind non-loopback %s without [api] token"`.

- [ ] **Step 1:** Write failing tests in `config_test.go`: (a) empty config → API defaults present (`Enabled`, `Addr == "127.0.0.1:8787"`); (b) config with `[api] addr = "0.0.0.0:8787"` and no token → `Load` returns error containing `"without [api] token"`; (c) same addr with `token = "x"` → no error.
- [ ] **Step 2:** `go test ./internal/config/` → FAIL (unknown field `API`).
- [ ] **Step 3:** Implement struct, defaults, validation following the existing defaulting pattern in config.go.
- [ ] **Step 4:** `go test ./internal/config/` → PASS. Also add the `[api]` section (commented defaults) to `tui/config.toml`.
- [ ] **Step 5:** Commit: `feat(config): [api] section with loopback-or-token guard`

### Task 2: Proposal registry in executor

**Files:**
- Create: `tui/internal/executor/proposals.go`
- Test: `tui/internal/executor/proposals_test.go`
- Modify: `tui/internal/executor/executor.go` (`Handle` propose branch registers the proposal; `Executor` gains `Proposals() *ProposalRegistry`)
- Modify: `tui/src/main.go` Telegram wiring — approve/reject callbacks route through the registry (find the existing `OnApprove` wiring; keep the same Telegram UX)

**Interfaces:**
- Produces:

```go
type Proposal struct {
    ID      string           `json:"id"`
    Verdict reasoner.Verdict `json:"verdict"`
    Created time.Time        `json:"created"`
    Expires time.Time        `json:"expires"`
}
// NewProposalRegistry(ttl time.Duration) *ProposalRegistry  (ttl<=0 → 15*time.Minute)
// (r *ProposalRegistry) Add(v reasoner.Verdict) Proposal     // id: 8 hex bytes from crypto/rand
// (r *ProposalRegistry) List() []Proposal                    // unexpired, newest first
// (r *ProposalRegistry) Take(id string) (Proposal, bool)     // removes; false if missing/expired
```

- Executor: propose branch in `Handle` (executor.go:121-128) calls `e.proposals.Add(v)` and includes `id=<id>` in the published JournalEvent summary. New method `(e *Executor) Approve(ctx context.Context, id string) error` = `Take` + `Execute`; `(e *Executor) Reject(id string) error` = `Take` + journal a `"rejected"` alert.
- Consumes: existing `Execute(ctx, v)` (risk gates inside), `journal.Record`.

- [ ] **Step 1:** Failing tests: Add→List contains it; Take removes; expired proposal absent from List and Take fails (inject clock via `now func() time.Time` field, default `time.Now`); Approve on registered proposal reaches submit path (reuse the fake exchange `httptest` server pattern from `executor_test.go`); Approve on unknown id → error `"no such proposal"`.
- [ ] **Step 2:** `go test ./internal/executor/` → FAIL.
- [ ] **Step 3:** Implement registry + executor methods + Handle wiring.
- [ ] **Step 4:** `go test ./internal/executor/` → PASS.
- [ ] **Step 5:** Rewire Telegram callbacks in `tui/src/main.go` to `exec.Approve/Reject`; delete Telegram's private pending map if one exists (check `internal/telegram/telegram.go` and its wiring). `go build ./...` clean.
- [ ] **Step 6:** Commit: `feat(executor): shared proposal registry; telegram + api use one confirm flow`

### Task 3: API server scaffold — health, auth, CORS, error envelope

**Files:**
- Create: `tui/internal/api/server.go`, `tui/internal/api/middleware.go`
- Test: `tui/internal/api/server_test.go`

**Interfaces:**
- Produces:

```go
// Package api is the daemon's HTTP+WS surface: the unified backend core any
// frontend attaches to. It is a bus consumer like the TUI — subscribe, cache,
// serve — and never touches component internals.
type Deps struct {
    Bus      *bus.Bus
    Store    *store.Store
    Engine   *reasoner.Engine   // nil → chat endpoint returns 503
    Exec     *executor.Executor // nil → execution endpoints return 503
    Cfg      config.Config
    Version  string
}
func NewServer(d Deps) *Server            // builds mux, starts cache goroutine
func (s *Server) Handler() http.Handler   // auth+CORS-wrapped mux (for httptest)
func (s *Server) ListenAndServe(ctx context.Context) error // honors ctx cancel
```

- `GET /api/health` → `{"connected":bool,"mode":"propose|autonomous","providers":{"batch":"...","chat":"..."},"version":"..."}` — connected/mode from a status cache fed by `Bus.SubscribeStatus(8)`; providers via `Engine` registry accessors (`reasoner.Registry.Active`) — pass what's needed through Deps.
- Middleware: if `Cfg.API.Token != ""`, require `Authorization: Bearer <token>` on every `/api/` route → else 401 `{"error":"unauthorized"}`. CORS: allow origins in `Cfg.API.CORSOrigins`, methods GET/POST/DELETE, header Authorization+Content-Type; answer OPTIONS preflight 204.
- Error helper: `writeErr(w, code, format, args...)` emitting `{"error":"..."}` — every handler uses it.

- [ ] **Step 1:** Failing tests (httptest against `Handler()`): health 200 + JSON shape; token set → no header 401, correct header 200; OPTIONS preflight from allowed origin → 204 with `Access-Control-Allow-Origin` echoed; disallowed origin → no CORS headers.
- [ ] **Step 2:** `go test ./internal/api/` → FAIL (package missing).
- [ ] **Step 3:** Implement. Route table uses Go 1.22 patterns (`mux.HandleFunc("GET /api/health", ...)`).
- [ ] **Step 4:** `go test ./internal/api/` → PASS.
- [ ] **Step 5:** Commit: `feat(api): server scaffold — health, bearer auth, CORS`

### Task 4: Read endpoints — markets, bars, digests, verdicts, journal

**Files:**
- Create: `tui/internal/api/read.go`
- Modify: `tui/internal/journal/journal.go` (add `ReadDay`)
- Test: `tui/internal/api/read_test.go`, `tui/internal/journal/journal_test.go` (new file)

**Interfaces:**
- Produces (journal): `func ReadDay(dir, date string) ([]Entry, error)` — reads `<dir>/journal/<date>.ndjson` (date `YYYY-MM-DD`, validate with `time.Parse`), decodes NDJSON, skips malformed lines, `os.IsNotExist` → empty slice, nil error.
- Endpoints:
  - `GET /api/markets` — for each `Cfg.Markets.Tracked` coin: latest bar for its configured timeframe (`Store.LatestBar(coin, Cfg.Timeframe.For(coin))`), mid (`Store.Mid`), asset ctx (`Store.AssetCtx`). 404 `{"error":"store warming up"}` if every coin lacks a bar.
  - `GET /api/bars/{coin}` — query `tf` (default `Cfg.Timeframe.For(coin)`), `n` (default 100, cap 1000) → `Store.History(coin, tf, n)`; empty → 404.
  - `GET /api/digests/{coin}` — latest digest from a cache goroutine consuming `Bus.SubscribeDigests(16)` into `map[coin]metrics.Digest`; missing → 404.
  - `GET /api/verdicts` — cache from `Bus.SubscribeVerdicts(16)`, latest per asset, newest-first array; empty → 200 `[]`.
  - `GET /api/journal?date=2026-07-03` — `journal.ReadDay(Cfg.Storage.Dir, date)`; bad date → 400; default date = today UTC.
- Caches live in one goroutine started by `NewServer`, guarded by one `sync.RWMutex` on a `serverState` struct (status, digests, verdicts) — single owner, no scattered locks.

- [ ] **Step 1:** Failing tests: journal ReadDay round-trip (write temp NDJSON incl. one malformed line → entries minus malformed); markets from a store seeded with `PutBar`/`PutMids`/`PutAssetCtx` fixtures; bars n-cap; digests/verdicts via publishing on a real `bus.New()` then polling handler until cache populated (use `require`-style wait loop ≤1s); journal bad date 400.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** `go test ./internal/api/ ./internal/journal/` → PASS.
- [ ] **Step 5:** Commit: `feat(api): read endpoints — markets, bars, digests, verdicts, journal`

### Task 5: Chat + execution endpoints

**Files:**
- Create: `tui/internal/api/act.go`
- Test: `tui/internal/api/act_test.go`

**Interfaces:**
- `POST /api/chat` body `{"message":"...","history":[{"role":"user|assistant","text":"..."}]}` → calls `Engine.Chat(ctx, message, history, contextText)` where history maps to `[]reasoner.ChatTurn` (check its exact field names in `internal/reasoner`) and `contextText` is built the way the TUI chat pane builds it (find the call site in `internal/tui/` and extract/reuse — if the TUI has a context-builder helper, move it somewhere shared like `reasoner` rather than duplicating). Response `{"reply":"...","provider":"...","model":"..."}` via `Engine.ChatProviderName()/ChatModel()`. Engine nil → 503. Provider error → 502.
- `GET /api/proposals` → `Exec.Proposals().List()`.
- `POST /api/proposals/{id}/approve` → `Exec.Approve(ctx, id)`; unknown id → 404; risk-gate error → 422 with gate message.
- `POST /api/proposals/{id}/reject` → `Exec.Reject(id)`; unknown → 404.
- `POST /api/orders` body `{coin, action, size_usd, order_type, price, stop, take_profit, thesis}` → build `reasoner.Verdict` exactly as `tui/src/mcp.go:363-400` does (Timeframe `"api"`, Provider `"api"`, Confidence 1) → `Exec.Execute`; gate rejection → 422. Exec nil (no signer) → 503 `{"error":"no HL_AGENT_KEY configured"}`.
- `DELETE /api/orders/{coin}/{oid}` → `Exec.Cancel`.

- [ ] **Step 1:** Failing tests: chat happy path with a fake `Provider` registered in a real `reasoner.Registry` (see `reasoner/engine_test.go` for the existing fake-provider pattern); proposals approve flow end-to-end: registry Add → HTTP approve → fake exchange server receives signed envelope (reuse executor test harness); orders gate rejection surfaces 422 + gate name; nil Engine → 503.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** `go test ./internal/api/` → PASS.
- [ ] **Step 5:** Commit: `feat(api): chat + propose/confirm + direct order endpoints`

### Task 6: WebSocket stream

**Files:**
- Create: `tui/internal/api/ws.go`
- Test: `tui/internal/api/ws_test.go`

**Interfaces:**
- `GET /api/ws` upgrades via `gorilla/websocket.Upgrader` (`CheckOrigin` = same CORS allowlist; token via `?token=` query param too, since browsers can't set WS headers).
- Frames: `{"topic":"bar|verdict|journal|status|mids","data":<json>}`. One writer goroutine per client draining a `chan []byte` buffer 64; enqueue non-blocking with drop-oldest (mirror `bus.topic.publish` semantics). Server pings every 30s, read deadline 90s; dead client → unsubscribe + close.
- Fan-in: the existing cache goroutine (Task 4) also forwards each bus event it consumes to registered WS clients — extend it rather than adding parallel subscriptions; mids come from `Bus.SubscribeMids(32)` added to the same select. Client registry: `map[*wsClient]struct{}` under the server mutex.

- [ ] **Step 1:** Failing test: connect with `websocket.DefaultDialer` against `httptest.NewServer(s.Handler())`, publish a `metrics.Bar` and a `bus.JournalEvent` on the bus, assert both frames arrive with correct topics within 1s; stalled-client test: fill a client buffer without reading, assert publishes don't block (guard with `t.Deadline`-aware timeout).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** `go test ./internal/api/ -race` → PASS.
- [ ] **Step 5:** Commit: `feat(api): websocket push stream with drop-oldest backpressure`

### Task 7: Daemon wiring

**Files:**
- Modify: `tui/src/main.go` — in `run()` (line ~109), after executor/journal/engine construction, when `cfg.API.Enabled`: build `api.Deps`, start `srv.ListenAndServe(ctx)` in a goroutine, log the bound addr to stderr in headless mode; ensure clean shutdown on ctx cancel (both TUI-exit and SIGINT paths).

- [ ] **Step 1:** Wire it. `go build ./...` clean; `go vet ./...` clean.
- [ ] **Step 2:** Smoke: `./build.sh && ./hyperagent -headless -testnet &` then `curl -s localhost:8787/api/health` → JSON with `"connected"`; `curl -s localhost:8787/api/verdicts` → `[]`; kill daemon, confirm it exits cleanly (no goroutine panic output).
- [ ] **Step 3:** Commit: `feat(daemon): serve unified core API from TUI and headless modes`

### Task 8: Dashboard thin client + status pill

**Files:**
- Create: `dashboard/src/lib/core-client.ts`
- Create: `dashboard/src/hooks/useCore.ts`
- Modify: `dashboard/src/components/shell/TopNav.tsx` (status pill)

**Interfaces:**
- `core-client.ts`: `CORE_URL` (default `http://127.0.0.1:8787`, override via `VITE_CORE_URL`); `fetchHealth(): Promise<CoreHealth|null>` (null on network error, 1.5s timeout via `AbortSignal.timeout`); `coreWS(onFrame)` returning cleanup fn, exponential-backoff reconnect capped 30s. Types: `CoreHealth {connected:boolean; mode:string; providers:{batch:string;chat:string}; version:string}`, `CoreFrame {topic:string; data:unknown}`.
- `useCore.ts`: `useCoreHealth()` polls `fetchHealth` every 10s → `{health: CoreHealth|null, online: boolean}`.
- TopNav pill: `CORE ●` green when online (title shows mode+provider), dim gray `CORE ○` when offline. Match existing TopNav styling idioms (read the file first).
- No other dashboard behavior changes — full intelligence UI is the next sub-project.

- [ ] **Step 1:** Implement; `cd dashboard && bun run build` clean, `bun run lint` clean.
- [ ] **Step 2:** Manual check: `bun run dev` with daemon running → pill green; stop daemon → pill gray within 10s.
- [ ] **Step 3:** Commit: `feat(dashboard): core client + daemon status pill`

### Task 9: Full verification

- [ ] `cd tui && go build ./... && go vet ./... && go test ./... -race` → all PASS.
- [ ] Headless daemon + curl every endpoint (health, markets, bars/BTC, verdicts, journal, proposals; chat only if a provider key is present in env — otherwise assert 502/503 shape).
- [ ] `cd dashboard && bun run build` → clean.
- [ ] Update `tui/README.md`: new "## HTTP API" section documenting the endpoint table, `[api]` config, and token rule (mirror the MCP section's tone/format).
- [ ] Commit: `docs: HTTP API section in README`
