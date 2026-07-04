# hyperagent-tui

Standalone terminal client for the `hyperagent` daemon (`backend/`). Holds no
market-data or execution state of its own — everything comes from the
daemon's unified HTTP+WS core API (`backend/internal/api`). Bubble Tea UI,
same look and workflow as the TUI that used to run in-process inside the
daemon; the only thing that changed is the transport.

## Build & run

Requires a running `hyperagent` daemon with its HTTP API enabled
(`[api] enabled = true` in `config.toml` — on by default).

```sh
# in one terminal
cd ../backend && ./hyperagent -testnet

# in another
go build -o hyperagent-tui ./src
./hyperagent-tui -core-url http://127.0.0.1:8787
```

| Flag | Default | Does |
|---|---|---|
| `-core-url` | `http://127.0.0.1:8787` | daemon base URL |
| `-token` | `$HYPERAGENT_TOKEN` | bearer token, only needed if the daemon sets `[api] token` |

On start, the TUI fetches `/api/settings` once to seed provider/model lists,
watchlist, timeframes and risk limits, then opens `/api/ws` for the live
push stream (bars, verdicts, journal, status, mids) and polls `/api/markets`
for the table. If the daemon isn't reachable it fails fast with
`could not reach daemon at <url>: ...` instead of drawing a blank UI.

All in-TUI actions — `/watch`, `/track`, `/scan`, settings edits, mode
toggles, order placement, chat — are just calls through
`internal/apiclient` onto the daemon's control-plane endpoints; the daemon
does the actual work (risk gates, journaling, execution) exactly as it does
for `curl` or the web dashboard.

## Module layout

- `src/` — entrypoint (flag parsing, wiring `apiclient` + `internal/tui` +
  Bubble Tea program).
- `internal/apiclient/` — typed HTTP+WS client for the daemon's `/api/*`
  surface; the only thing that talks to the network.
- `internal/tui/` — Bubble Tea model/views; takes an `apiclient.Client` as
  its `Controls` dependency, never dials the network directly.

This is its own Go module (`github.com/hyperagent/tui`) with no dependency
on `backend/`'s internals — only on the JSON shapes the daemon's HTTP API
returns, mediated through `internal/apiclient`.
