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

## Known limits of the scaffold

- Hyperliquid `Place` routes through the existing executor when one is wired;
  reduce-only buys (closing a short) return not-implemented because the legacy
  submit path always sends reduce-only as sell. Paper venue covers it.
- `regime_rotation` has no realized-vol source yet (venue exposes no bars).
- Web console polls; WS is not proxied through hypertrade.
- No live Jev call has been made from this environment.

## Open questions for the operator

- Which second venue after Hyperliquid (RESEARCH.md recommends one).
- Whether to retire the chat cockpit once the operator console covers daily use.
- Production model pinning: `jev-latest` vs a pinned `jev-1.13.x`.
