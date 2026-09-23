# SPEC — Hyperion v2 prototype: JEV-driven strategy runtime, terminal config, web console

Status: phase 1 scaffold done; phase 2 (Monad venue, web analyst, surface audit) on this branch. Owner: nicolascerrato17@gmail.com.

## Why

Hyperion's current loop reasons with autoregressive LLMs (direct API or spawned
CLI harnesses) and the TUI is a chat cockpit that reads and narrates. That is the
wrong shape for an operator tool: the model writes, the human reads, and the
execution policy is buried in a prompt.

Jev (TypeSafe AI, released 2026-09-15) is a System One model: it takes a state
(string or JSON) plus a map of typed questions and returns typed, calibrated
answers with probabilities in 70–500 ms at $0.042 per million input tokens,
output free. It cannot return anything outside the option set you give it. That
makes it the right primitive for the decision points inside a trading loop:
"which regime is this", "is funding extreme", "does this intent still fit the
mandate". The prose, plans, and explanations go away; the code owns control flow.

## What this prototype is

A chain-agnostic, swappable plug-in layer that runs custom trading strategies
programmatically, driven by Jev decisions and gated by human agency, with two
operator surfaces:

- **Terminal** (`hyperion/tui`, Go, Bubble Tea v2): the configuration and
  operations interface. Enable and parameterize strategies, watch decisions
  land with their probability distributions, approve or reject intents, flip
  the governor, hit the kill switch. Not a chatbot.
- **Web** (`hypertrade`, React + Hono on Vercel): the same surface for laptop
  and phone, proxied to a running Hyperion core.

Both talk to one core over HTTP+WS: the existing Hyperion daemon
(`hyperion/backend`, Go, `:8787`) extended with a `strategy` subsystem. The
core keeps the parts that already work: Hyperliquid client, EIP-712 signer,
risk gates, event bus, NDJSON journal, bearer-token API.

## Non-goals for this scaffold

- Replacing the legacy reasoner/thesis pipeline. It stays; the strategy runtime
  is a parallel subsystem. Deleting the old cockpit is a later decision.
- Real money on a new chain. The first two venues are `hyperliquid` (reusing
  the existing client) and `paper` (in-memory simulator). Other chains are
  interface-only plus a research note.
- Backtesting. `paper` venue plus `dry_run` decisions cover "what would it do
  right now"; historical replay is later.
- Multi-user, hosting, billing.

## Architecture

```
                 ┌───────────────── operator ─────────────────┐
                 │  tui/ (operator console)   hypertrade /strategies │
                 └─────────────┬───────────────────────┬─────┘
                               │ HTTP+WS  /api/strategy/*   (bearer)
                               ▼
  backend/internal/strategy/
    strategy.go     Strategy, Manifest, Intent, Answers      (plugin contract)
    decider/        Decider iface; jev/ HTTP client; fake/ for tests
    venue/          Venue iface; hyperliquid/ adapter; paper/ simulator
    runtime/        Runner (cadence loop), Governor (approval + limits), DecisionStore
    builtin/        funding_skew, regime_rotation (reference strategies)
    registry/       name → Strategy constructor; stdio plugin adapter (stub)
  backend/internal/api/strategy.go   HTTP+WS handlers, mounted on existing mux
  backend/src/strategy.go            `hyperagent strategy list|run --dry-run`
```

### Plugin contract (Go)

```go
type Strategy interface {
    Manifest() Manifest                                   // static: id, params schema, questions, venues
    Snapshot(ctx context.Context, v venue.Venue, p Params) (State, error) // build Jev state
    Decide(state State, a Answers, p Params) []Intent     // deterministic: answers → intents
}
```

- `Manifest.Questions` is the fixed question map the strategy asks each tick.
  Jev evaluates all questions independently in one request.
- `Decide` is pure. All thresholds live in `Params` (typed, defaulted in the
  manifest) so the terminal and web can edit them without code.
- Chain agnosticism comes from `venue.Venue`: `Markets`, `Positions`,
  `Place`, `Cancel`, `Capabilities`. Strategies see markets by symbol and
  sizes in USD; the venue adapter does the translation.
- `registry` maps names to constructors for in-process Go plugins. A `stdio`
  adapter (manifest handshake + `snapshot`/`decide` JSON-RPC over a child
  process) is stubbed so non-Go strategies can plug in later.

### Runtime

`Runner` ticks each enabled strategy on its cadence: snapshot → `Decider.Evaluate`
→ `Decide` → `Governor.Review` → venue (or proposal). Every tick writes one
`DecisionRecord` (state digest, questions, answers, intents, verdicts, model,
usage, latency) to the DecisionStore and publishes it on the bus; the existing
journal gets a summary line.

`Governor` is the human-agency layer:

- `mode`: `manual` (every intent becomes a proposal), `threshold` (intents with
  `confidence >= min_confidence` auto-execute, the rest become proposals),
  `auto` (all execute, still risk-gated).
- Hard limits: `max_notional_usd` per intent, `max_open_intents`, daily loss
  kill reusing the executor's daily-loss logic where possible.
- `kill`: one call disables every strategy and cancels open proposals.

Confidence thresholds are per strategy config, not global, because Jev's
confidence is calibrated in aggregate and each question has its own curve.

### Jev decider

`decider/jev` is a small `net/http` client for `POST https://api.typesafe.ai/v1/systemone`
(`Authorization: Bearer $TYPESAFE_API_KEY`). Request `{model, state, questions}`,
response `{model, answers, usage}`. Three question types:

| type   | criteria                          | answer fields                                  |
|--------|-----------------------------------|------------------------------------------------|
| choice | map option → description (≤255)   | `choice`, `probabilities{opt: p}`, `confidence` |
| score  | ordered list of 2–10 level texts  | `score` (float, may sit between levels), `probabilities{"0":p,...}`, `legend`, `confidence` |
| noul   | none                              | `noul` ∈ [0,1]                                  |

Limits (per RESEARCH.md): 64k tokens per request, 32k for state plus the
longest question, 1,200 requests per minute. Jev is text-trained and weak on
arithmetic, so strategies compute z-scores and buckets in Go and send each
number with a label; `State` stays compact (no raw order books). Timeout 5 s,
one retry honouring `Retry-After` on 408/429/529/5xx/timeout, never on other
4xx. Pin a versioned model (`jev-1.13.0`) in production; the response echoes
the resolved version. `fake` decider returns scripted answers for tests and
for the paper venue when no key is configured. Vercel AI Gateway
(`https://ai-gateway.vercel.sh/typesafe`, model `typesafe-ai/jev`), OpenRouter
(`/api/v1/systemone`, model `typesafe/jev-1.13`) and LiteLLM pass-through are
`base_url` + model swaps; Cloudflare Workers AI and Vercel's native
`experimental_evaluate` use different envelopes and would need adapters.

### Reference strategies

- `funding_skew`: state = per-market funding, premium, OI delta, 24h return.
  Questions: `funding_extreme` (noul), `direction` (choice: fade_long / fade_short / none),
  `crowding` (score 0–2). Intent: open opposite to crowded side, sized by
  `params.size_usd`, only when `funding_extreme >= params.min_p` and confidence
  ≥ config threshold.
- `regime_rotation`: state = BTC/ETH returns over several windows, realized
  vol, breadth. Questions: `regime` (choice: risk_on / risk_off / chop),
  `conviction` (score). Intent: `rebalance` toward the target weights in
  params for that regime.

Both ship with fixture states and fake-decider tests; neither is a claim of
edge. They exist to prove the contract end to end.

### Config

`config.toml` gains:

```toml
[strategy]
  enabled = true
  decisions_dir = "data/decisions"     # NDJSON, one file per day
[strategy.decider]
  kind = "jev"                          # jev | fake
  model = "jev-latest"
  base_url = "https://api.typesafe.ai"
  api_key_env = "TYPESAFE_API_KEY"
[strategy.governor]
  mode = "manual"
  max_notional_usd = 1000
  max_open_intents = 5
[[strategy.configs]]
  id = "funding_skew"
  enabled = false
  venue = "paper"
  [strategy.configs.params]
  size_usd = 250
  min_p = 0.8
  [strategy.configs.governor]
  min_confidence = 0.75
```

### Terminal (tui/)

New package `tui/internal/operator`, default program in `tui/src/main.go`
(`--cockpit` keeps the legacy chat UI). Four tabs, keyboard driven, same
Lipgloss theme as the cockpit:

1. **STRATEGIES** — table of manifests × configs: enabled, venue, cadence,
   last decision, last intent. `e` toggles, `enter` opens a param form generated
   from the manifest (`number`/`bool`/`enum`/`string`), `r` runs a dry-run and
   jumps to the resulting decision.
2. **DECISIONS** — live list from WS plus history. Detail pane renders each
   question with a horizontal probability bar per option, the score position
   on its rubric, intents beneath with governor verdict. `a`/`x` approve /
   reject a proposed intent.
3. **VENUES** — status, capabilities, positions per venue.
4. **GOVERNOR** — mode selector, limits, kill switch (typed confirmation).

`tui/internal/apiclient` gains the `/api/strategy/*` calls and WS event types.

### Web (hypertrade)

- `src/shared/strategy-protocol.ts`: zod schemas for every wire type in
  PROTOCOL.md, plus fixtures under `src/shared/fixtures/strategy/`.
- `src/server/routes/engine.ts`: authenticated proxy `/api/engine/*` →
  `${ENGINE_URL}/api/strategy/*` with `Authorization: Bearer ${ENGINE_TOKEN}`.
  Unset `ENGINE_URL` yields a typed 503 so the UI renders the OfflineBlock.
  WS is not proxied in this scaffold; the UI polls decisions every 5 s.
- Pages `/strategies`, `/strategies/:id`, `/decisions`, `/decisions/:id`,
  `/governor` following DESIGN.md (dense, mobile-first, state vocabulary).
  New components: `ProbBar`, `ScoreRubric`, `IntentRow`, `ParamForm`.

## Verification bar for the scaffold

- `cd backend && go build ./... && go vet ./... && go test ./...` green.
- `cd tui && go build ./... && go test ./...` green; operator model has
  smoke tests for each tab's view.
- `cd hypertrade && bun run typecheck && bun test src && bun run build` green.
- `hyperagent strategy run funding_skew --dry-run --decider fake` prints a
  DecisionRecord.
- Live Jev call not verified here: no `TYPESAFE_API_KEY` in this session and
  `typesafe.ai` is egress-blocked. The client is tested against the documented
  response shapes; first live run is the operator's.

## Phase 2 — Monad venue, web analyst, surface audit

### Paradigm (unchanged, restated)

The terminal is the configuration and execution interface for strategies run
programmatically with Jev; it is not a chatbot. The web console is its
companion (same four surfaces through the `/api/engine` proxy) and is the one
place a classic LLM appears: a read-only analyst. Jev remains the only model
inside the trading loop; no LLM output reaches a venue.

### Monad venue (`backend/internal/strategy/venue/monad`)

Research, sources and the unverified list: [RESEARCH-monad.md](./RESEARCH-monad.md).
Chosen protocol: **Uniswap v3 on Monad** (QuoterV2 prices, SwapRouter02
`exactInputSingle` swaps), because its ABI is immutable and identical across
chains, quoting and swapping are plain JSON-RPC calls go-ethereum's
`ethclient` already speaks, and slippage bounds are native
(`amountOutMinimum`). Perps on Monad (Perpl) need a REST/WS adapter with
Ed25519 request signing and are deferred; Kuru's CLOB is the natural second
protocol inside this venue.

- `Status`: `kind "evm"`, `chain "monad"`; chain id (mismatch → degraded),
  head block, native MON balance in the optional `meta` object;
  unreachable RPC → `disconnected`, partial failure → `degraded`.
- `Markets`: one QuoterV2 quote per configured pair (default MON via WMON and
  ETH via WETH against USDC, 0.3 % pools). No funding/OI (spot);
  `day_change` from the venue's own samples once 24h exist.
- `Positions`: ERC-20 balances × quote, long-only, entry unknown.
- `Place`: enforces `max_notional_usd` itself (min of the venue cap and the
  governor's live global cap, because the executor's risk gates never see
  Monad orders), refuses sells beyond the held balance (spot cannot short;
  reduce-only clips), fresh quote → `amountOutMinimum` (slippage bps, tightened
  by `price_limit`), exact ERC-20 approval when short, EIP-1559 tx with a
  15 % gas buffer (Monad charges the gas limit), receipt wait with timeout,
  actual output read from the `Transfer` log; executed verdicts carry `tx=`.
- `Cancel`: not applicable (atomic swaps) → `ErrNotImplemented`.
- Capabilities: `spot`, plus `execute` with a signer.
- Config `[strategy.venues.monad]`, `enabled = false` by default. RPC from
  `MONAD_RPC_URL` (else `rpc_url`, else the network default); signer only
  from `MONAD_PRIVATE_KEY` (config refuses `HL_AGENT_KEY` / `HL_MASTER_KEY`).
  `regime_rotation` lists `monad` (long-only weights); `funding_skew` stays
  perps-only; reference configs default to `paper`.
- Tests: an `httptest` JSON-RPC stub behind a real `ethclient` covers status
  (connected, read-only, chain mismatch, RPC failure → degraded, RPC down →
  disconnected), markets, day change, positions, place (approve + swap signed
  by the configured key with EIP-1559 fees, slippage floor), notional cap,
  spot short refusal and reduce-only clip, price limit, receipt timeout.
- Not verified live: all Monad RPC hosts are egress-blocked from this
  session and no key exists; see RESEARCH-monad.md "Unverified".

### Web analyst (hypertrade `/analyst`)

`POST /api/analyst/query` streams SSE (`text`, `tool_call`, `tool_result`,
`citations`, `error`, `done`) from a bounded tool loop (8 rounds, 90 s) over
read-only tools that reuse hypertrade's own data paths: briefing and history,
sectors, metric summaries and series, Hyperliquid markets, engine strategies
and decisions (through the same upstream as `/api/engine`; degrades when
`ENGINE_URL` is unset), plus Anthropic's server-side web search when the
provider is anthropic. Providers swap by env (`ANALYST_PROVIDER` anthropic |
openai-compatible, `ANALYST_MODEL`, `ANALYST_API_KEY`, `ANALYST_BASE_URL`).
The system prompt carries ROUTINE.md's hard rule and hedge vocabulary
verbatim. No tool writes; approve/reject/kill/config stay operator actions.

### Surface audit (TUI + web)

- TUI STRATEGIES falls back to the status's `last_action` when the decision
  is not in memory; VENUES renders evm `meta` (network, chain, head block,
  balance, signer, protocol) and `error`, and a dash for unknown spot entries.
- Web: `StrategyStatus.last_action` and `VenueStatus.error/meta` in the zod
  schemas; Strategies falls back to `last_action`; the governor's venues panel
  shows evm detail; Overview gains a compact read-only ENGINE card; ANALYST
  joins the nav.
- Hyperion README marks `dashboard/` legacy and points to hypertrade.

### Verification bar (phase 2)

Same commands as phase 1 in both repos, green. Live Monad and live LLM calls
are not verified here (no keys; RPC hosts blocked); the analyst is exercised
end to end against a local fake Chat Completions server and with a fake
Anthropic SSE transport.
