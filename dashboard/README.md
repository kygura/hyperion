# hyperagent dashboard

Local web client for the `hyperagent` daemon (`backend/`). A React + Vite SPA
you run yourself on your own machine — not a hosted or multi-user product.
It talks to a backend daemon running locally (or on a host you control) over
HTTP+WS; it holds no execution state of its own.

## Build & run

```sh
bun install   # or: npm install
bun run dev   # or: npm run dev — serves on http://localhost:5173
```

The backend daemon must be running for the agent console and chat to work:

```sh
cd ../backend && ./hyperagent -testnet
```

To point the dashboard at a daemon that isn't on `127.0.0.1:8787`, set
`VITE_CORE_URL` (e.g. in `.env.local`):

```
VITE_CORE_URL=http://127.0.0.1:8787
```

Other scripts: `bun run build` (typecheck + Vite build), `bun run lint`,
`bun run preview`, `bun run prices` (fetches OHLCV snapshots from Hyperliquid
into `public/data/prices.json`).

## Pages

Routes are defined in `src/main.tsx`:

- **`/dashboard`** — market view
- **`/dashboard/portfolio`** — portfolio/paper-trading view (no daemon required)
- **`/dashboard/agent`** — live agent console: daemon connectivity, execution
  mode, liquidity regime board, agent theses, pending propose-mode approvals,
  and the day's decision journal; requires the daemon running. See the panel
  breakdown in `docs/ARCHITECTURE.md`.
- **`/dashboard/branches`** — redirects to `/dashboard/portfolio`

A global chat drawer (toggle via the CHAT button in the top nav, or `Esc` to
close) is available from every page and also talks to the daemon.

With the daemon down, the agent console and chat drawer render an explicit
"offline" state rather than spinning or crashing; everything else (prices,
portfolio) works without it.
