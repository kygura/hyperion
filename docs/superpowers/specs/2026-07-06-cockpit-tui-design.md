# Cockpit TUI — replace the multi-view TUI with the pitch cockpit layout

**Date:** 2026-07-06
**Status:** Approved (option C — fresh view package, reused plumbing)

## Goal

Replace the current multi-view TUI in `tui/` with the four-panel operator
cockpit prototyped in `pitch/mock-tui/`, connected to real backend data over
the existing HTTP+WS control plane (`:8787`). The chat survives as a bottom
input bar. `pitch/mock-tui/` itself stays untouched as a pitch artifact.

## Non-goals

- No backend changes. Every panel renders only data the daemon already
  exposes; nothing is simulated.
- No mandate/allocation/horizon concept — the mock's MANDATE panel is
  re-scoped to the real risk envelope (see below).
- No in-TUI settings or provider-key editors in this pass. The API keeps
  those endpoints; only the overlay UIs are dropped.

## Approach

Keep the tested data layer of the `tui/` module unchanged:

- `tui/internal/apiclient` — HTTP client, wire types, cache. Untouched.
- The WS bridge (`PumpWS`, `PollMarkets`, reconnect backoff, message types
  `barMsg` / `verdictMsg` / `journalMsg` / `statusMsg` / `positionMsg`) —
  reused as-is; it is view-agnostic by design.

Build a new, small cockpit model+view inside the `tui/` module by porting
the mock's rendering (`pitch/mock-tui/view.go`, ~340 lines, Bubbletea v1 /
lipgloss v1) to the module's Bubbletea v2 / lipgloss v2 stack. The mock's
pure helpers (`box`, `spread`, `padR`, `padL`, `signed`, `fnum`,
`priceDec`) port near-verbatim — they use only `lipgloss.Width` and string
building.

Once the cockpit runs against the daemon, delete the old view layer:
`markets.go`, `detail.go`, `settings.go`, `signalview.go`, `ideas.go`,
`overlays.go`, `helpview.go`, old `view.go` / `render.go` / `layout.go`,
and their tests. Chat client logic and slash-command handling are carried
over, trimmed to the commands that work without dedicated views.

## Layout

The mock's geometry, ported: header, 2×2 panel grid, footer — plus a chat
input bar above the footer.

```
┌ header: logo · conn/mode status · phase spinner · uptime ┐
│ MANDATE (risk envelope) │ MARKET PICTURE (live ingest)   │
│ EXECUTION (positions +  │ DECISION JOURNAL (append-only  │
│   gates)                │   tagged event log)            │
│ chat input bar                                           │
└ footer: key hints                                        ┘
```

Minimum terminal 96×28 (mock's guard, kept). When a chat conversation is
active, a reply pane expands over the DECISION JOURNAL panel; `Esc`
collapses it back to the journal.

## Data wiring

| Panel | Source | Notes |
|---|---|---|
| Header | `statusMsg` (WS conn, mode), last journal tag as phase | Uptime is client-side since program start. Mock's fake `h` halt key replaced by `m` = mode toggle via `PUT /api/execution/mode`. |
| MANDATE | `GET /api/settings` risk block + live positions | Caps: max position USD, max total exposure USD, max concurrent, daily-loss kill. Live utilization computed client-side: exposure Σ\|size×mark\| vs cap, open count vs max concurrent, total uPnL. No fake allocation %, no day-N horizon. |
| MARKET PICTURE | cache fed by WS `bar`/`mids` + 5s `/api/markets` poll | Columns: MKT, LAST, FUND/8H, OIΔ 1H, CVD 1H. Mock's 24H column dropped — no backend field. |
| EXECUTION | positions from `/api/markets` entries | Gates rendered from the risk-settings block (four real gates), pass/state derived from the same utilization math as MANDATE. |
| DECISION JOURNAL | WS `journal` + `verdict` topics | Tag mapping: `candidate`→REASON, `fill`→FILL, `open`/`close`→EXECUTE, `alert`→RISK, `error`→ERROR, status notices→OPERATOR. Ring buffer capped (200 entries, mock's `maxJournal`). |
| Chat bar | existing chat client (`POST /api/chat`) | Slash commands kept: `/scan`, `/watch`, `/track`, `/tf`, `/mode`, `/clear`, `/help`. Dropped from UI: `/settings`, `/keys`, `/model`, `/g` (overlay- or view-dependent). |

## Package structure

```
tui/
  src/main.go                — flags/env, apiclient.New, cockpit model,
                               PumpWS + PollMarkets goroutines (unchanged wiring)
  internal/apiclient/        — UNCHANGED
  internal/tui/              — rebuilt: cockpit model, update, view
    model.go                 — cockpit state: cache ref, journal ring, chat
                               state, mode/conn, width/height
    update.go                — small: WS msgs, keys (q, m, /, Esc, tab focus),
                               chat submit
    view.go                  — ported cockpit rendering (panels, box, helpers)
    theme.go                 — mock palette on lipgloss v2
    bridge.go                — UNCHANGED (message types + PumpWS/PollMarkets)
    chat.go                  — trimmed: agent conversation rendering + input
    commands.go              — trimmed to surviving slash commands
```

## Error handling

- WS drop: existing reconnect backoff; header shows disconnected state
  (amber ●) until `statusConn` asserts reconnect.
- `/api/settings` or `/api/markets` failures: panels render last-known data
  with a dim staleness hint; errors surface as OPERATOR journal entries,
  never crash the render loop.
- Chat errors: rendered inline in the reply pane (existing behavior).

## Testing

- `apiclient` and bridge tests survive untouched.
- Ported pure helpers get unit tests in the existing render-test style.
- Journal tag-mapping and MANDATE utilization math get table-driven tests.
- A smoke visual test (like the current `smoke_visual_test.go`) renders the
  cockpit at 96×28 with seeded cache data and asserts panel titles and
  no panic.
- Manual verification: run against the live daemon on `:8787`.

## Migration / cleanup

1. Cockpit built alongside old views (module compiles throughout).
2. `src/main.go` switched to the cockpit model.
3. Old view files + their tests deleted; unused deps tidied (`go mod tidy`).
4. `tui/README.md` updated to describe the cockpit.
5. `pitch/mock-tui/` untouched.
