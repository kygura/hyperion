# hyperagent-tui

Standalone terminal client for the `hyperagent` daemon (`backend/`). Holds no
market-data or execution state of its own — everything comes from the
daemon's unified HTTP+WS core API (`backend/internal/api`).

Two programs share one binary and one Lipgloss theme (`internal/theme`):

## Operator console (default)

`internal/operator` — the configuration and operations surface for the
strategy runtime (`docs/jev/SPEC.md`, wire contract in
`docs/jev/PROTOCOL.md`). Four keyboard-driven tabs, `1`–`4` or
`tab`/`shift+tab` to switch, minimum terminal size 60×16:

1. **STRATEGIES** — id, enabled, venue, cadence, last run, last
   intent/verdict, error. `e` toggles enabled (PUT config), `enter` opens the
   param form generated from the manifest (`number`/`string`/`bool`/`enum`
   with min/max/step validation; `ctrl+s` saves, `esc` cancels), `r` runs a
   dry-run and jumps to the resulting decision.
2. **DECISIONS** — live list (WS `strategy.decision` prepends) plus history.
   The detail pane renders every question with a probability bar per
   option, the score position on its rubric, the noul bar, confidence, then
   the intents with their latest verdict coloured by state. `a` approves /
   `x` rejects the selected proposed intent (`h`/`l` select intents,
   `pgup`/`pgdn` scroll).
3. **VENUES** — status, capabilities and positions per venue.
4. **GOVERNOR** — mode selector (manual/threshold/auto), min confidence,
   max notional, max open intents (`enter` edits, `ctrl+s` PUTs), and the
   kill switch: `K`, type `KILL`, `enter`. A killed governor shows a banner
   on every tab.

The console never blocks on the daemon: if it is unreachable it renders an
OFFLINE panel and retries every 5 s; when the WS push link drops it polls
until it is back. There is no chat anywhere in it.

## Legacy cockpit (`--cockpit`)

`internal/cockpit` — the earlier five-panel chat cockpit, unchanged:
minimum 96×28.

- **MANDATE** — risk envelope (exposure, open count, uPnL, compiled gate
  states).
- **MARKET PICTURE** — live ingest for the visualized watchlist.
- **EXECUTION** — the same compiled risk gates as MANDATE, rendered as
  pass/breach state.
- **THESES** — latest thesis per tracked asset (direction, confidence,
  invalidation level, targets, reviewed-ago).
- **DECISION JOURNAL** — streamed candidate/fill/open/close/alert/error
  events; swaps out for the **AGENT** chat panel while chat is open.

Keys: `/` opens chat, `m` toggles propose/autonomous mode, `q` (or
`ctrl+c`) quits.

## Build & run

Requires a running `hyperagent` daemon with its HTTP API enabled
(`[api] enabled = true` in `config.toml` — on by default).

```sh
# in one terminal
cd ../backend && ./hyperagent -testnet

# in another
go build -o hyperagent-tui ./src
./hyperagent-tui -core-url http://127.0.0.1:8787            # operator console
./hyperagent-tui -core-url http://127.0.0.1:8787 --cockpit  # legacy cockpit
```

| Flag | Default | Does |
|---|---|---|
| `-core-url` | `http://127.0.0.1:8787` | daemon base URL |
| `-token` | `$HYPERAGENT_TOKEN` | bearer token, only needed if the daemon sets `[api] token` |
| `--cockpit` | off | run the legacy chat cockpit instead of the operator console |

The operator console fetches `/api/strategy/{configs,manifests,decisions,venues,governor}`
on start and after every WS reconnect, then consumes the `strategy.*`
frames on `/api/ws`.

The legacy cockpit fetches `/api/settings` once to seed the watchlist,
timeframes and risk limits, then opens `/api/ws` for the live push stream
(bars, verdicts, journal, status, mids) and polls `/api/markets` for the
table. If the daemon isn't reachable it fails fast with
`could not reach daemon at <url>: ...` instead of drawing a blank UI.

All in-TUI actions — `/watch`, `/track`, `/scan`, mode toggles, chat — are
just calls through `internal/apiclient` onto the daemon's control-plane
endpoints; the daemon does the actual work (risk gates, journaling,
execution) exactly as it does for `curl` or the web dashboard.

## Module layout

- `src/` — entrypoint (flag parsing, wiring `apiclient` + `internal/operator`
  or `internal/cockpit` + Bubble Tea program).
- `internal/apiclient/` — typed HTTP+WS client for the daemon's `/api/*`
  surface, including the `/api/strategy/*` types and routes mirrored from
  `docs/jev/PROTOCOL.md`; the only thing that talks to the network.
- `internal/theme/` — the shared palette and layout primitives.
- `internal/operator/` — the operator console: model/update/views per tab,
  the param and governor forms, and its WS bridge (`PumpWS`).
- `internal/cockpit/` — Bubble Tea model/views for the cockpit layout above,
  plus the WS bridge (`PumpWS`/`PollMarkets`) that turns daemon push frames
  into Bubble Tea messages; takes an `apiclient.Client` as its `Controls`
  dependency, never dials the network directly itself.

This is its own Go module (`github.com/hyperagent/tui`) with no dependency
on `backend/`'s internals — only on the JSON shapes the daemon's HTTP API
returns, mediated through `internal/apiclient`.
