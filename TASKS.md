# TASKS — JEV strategy runtime prototype (see docs/jev/SPEC.md)

## Destination

A running Hyperion core with a `strategy` subsystem (Jev decider, venue
interface with `hyperliquid` + `paper`, governor, decision store, HTTP+WS API),
a terminal operator console that configures and supervises it, and a web
console in `hypertrade` proxied to it. All three build and test green; a
dry-run decision can be produced end to end with the fake decider.

## Decisions so far

- Jev, not an LLM, makes the in-loop decisions. Strategies declare a fixed
  question map (choice / score / noul); `Decide` is pure code over the typed
  answers. No prompts, no prose in the loop.
- The strategy runtime is a parallel subsystem inside `backend/`; the legacy
  reasoner/thesis pipeline and chat cockpit stay untouched (`--cockpit`).
- Human agency lives in the Governor (manual / threshold / auto, per-strategy
  confidence threshold, notional and open-intent caps, kill switch) and in the
  proposal approve/reject flow. Dry runs never reach the governor or a venue.
- Chain agnosticism is the `venue.Venue` interface. Hyperliquid reuses the
  existing client and signer; `paper` is an in-memory simulator. Other chains
  are a research note, not code, in this scaffold.
- Web console lives in `hypertrade` (already responsive, deployed, authed),
  reaching the core through a server-side proxy. Hyperion's `dashboard/` is
  not extended.
- One wire contract (`docs/jev/PROTOCOL.md`) mirrored in Go, Go (TUI), and zod.
- Not verified this session: a live Jev call (no key, egress blocked). The
  client is tested against the documented response shapes only.

## Tasks

| # | Task | Status |
|---|------|--------|
| P0 | Research Jev API + write docs/jev/SPEC.md, PROTOCOL.md | done |
| P1 | Core: `backend/internal/strategy/*`, api handlers, config, CLI | done (21 pkgs green, dry run works with fake decider) |
| P2 | Terminal: `tui/internal/operator`, apiclient additions, default in main | done |
| P3 | Web: `hypertrade` protocol schemas, `/api/engine` proxy, pages | done (kygura/hypertrade#2) |
| P4 | Research: docs/jev/RESEARCH.md | done |
| P5 | Integrate, verify (go/bun builds + tests), README updates, commit, PRs | done (kygura/hyperion#19, kygura/hypertrade#2) |

## Phase 2 — Monad venue, web analyst, surface audit

Decisions:

- Monad venue speaks Uniswap v3 (QuoterV2 + SwapRouter02) over `ethclient`;
  spot only; enforces the governor's global notional cap itself; key only
  from `MONAD_PRIVATE_KEY`. Perps on Monad (Perpl) need a separate REST/WS
  adapter (docs/jev/RESEARCH-monad.md).
- The operator reversed hypertrade's "no LLM calls" rule for one read-only
  analyst; Jev stays the only model in the trading loop.
- `VenueStatus` gains optional `error`/`meta`; `Fill` gains optional
  `tx_hash`, surfaced as `tx=` in executed verdict reasons.

| # | Task | Status |
|---|------|--------|
| M0 | Research Monad (chain, gas, RPC quirks, venues) → docs/jev/RESEARCH-monad.md | done (docs only; RPC hosts blocked, nothing checked live) |
| M1 | `venue/monad`: status, markets, positions, place, cancel, notional cap, embedded ABIs, JSON-RPC stub tests | done |
| M2 | Config `[strategy.venues.monad]`, wiring in buildStrategyRuntime, regime_rotation venue list, CLI fallback, API venues test | done |
| M3 | PROTOCOL.md evm VenueStatus + meta; TUI decode test and VENUES detail | done |
| A0 | Analyst server: providers (anthropic, openai-compatible), tools, system prompt, SSE route, tests | done (hypertrade) |
| A1 | Analyst UI `/analyst`, nav, DESIGN.md §10.8 | done (hypertrade) |
| A2 | Hypertrade SPEC/README/.env.example | done |
| C0 | Surface audit: TUI last_action fallback, evm venue detail; web last_action/meta schema, venue detail, Overview ENGINE card; README marks dashboard legacy | done |
| D0 | SPEC Phase 2, PROTOCOL, TASKS, READMEs; full checks both repos | done |

Known limits added in phase 2:

- No live Monad call has been made; contract addresses (QuoterV2
  discrepancy, testnet post-reset) must be checked before enabling.
- Monad venue is spot only: short intents (negative weights, `open_short`)
  are refused at Place; `day_change` needs 24h of the venue's own samples.
- Receipts at `latest` (Proposed) are treated as executed.
- Analyst: no live provider call verified here (no key); web search
  availability depends on the provider.

## Known limits of the scaffold

- Hyperliquid `Place` routes through the existing executor when one is wired;
  reduce-only buys (closing a short) return not-implemented because the legacy
  submit path always sends reduce-only as sell. Paper venue covers it.
- `regime_rotation` has no realized-vol source yet (venue exposes no bars).
- Web console polls; WS is not proxied through hypertrade.
- No live Jev call has been made from this environment.

## Open questions for the operator

- Which second venue after Hyperliquid (RESEARCH.md recommends one). Phase 2 added Monad (Uniswap v3 spot); a perps venue on Monad (Perpl) is the open follow-up.
- Whether to retire the chat cockpit once the operator console covers daily use.
- Production model pinning: `jev-latest` vs a pinned `jev-1.13.x`.
