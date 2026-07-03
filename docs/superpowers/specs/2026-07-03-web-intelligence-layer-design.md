# Web Intelligence Layer — agent console in the dashboard

**Date:** 2026-07-03
**Status:** Approved direction ("Exactly. Proceed"); UI-shape choice defaulted to the recommended option while user was away — flagged for review.
**Sub-project 2 of 4.** Depends on: unified backend core (shipped 2026-07-03, `tui/internal/api`, `127.0.0.1:8787`).

## Goal

Give the React dashboard the intelligence surface the daemon already computes:
market state at a glance with its liquidity drivers (funding, OI, CVD, basis,
liquidation proximity), the agent's written theses, a live decision log,
pending-proposal confirmation, and a chat line to the agent — all consumed from
the unified core API. This is the demo-able "agentic interface" product surface
the pitch and landing describe.

## Decisions

- **Shape:** new `/dashboard/agent` console page mirroring the landing's agent
  console (status strip, regime board, theses, decision log, proposals) **plus
  a global chat drawer** toggleable from the TopNav on every page.
- **Data path stays hybrid** (per sub-project-1 spec): raw prices keep flowing
  direct from HL; everything on the agent page comes from the core API.
- **Offline is a first-class state:** core daemon down → console renders with
  an explicit "daemon offline" banner and skeleton panels; no crashes, no
  infinite spinners. `useCoreHealth` (exists) is the source of truth.
- **No new test framework.** The dashboard has none; the bar is `bun run build`
  clean, `bun run lint` introducing zero new findings, and a scripted smoke
  against a running daemon. (26 pre-existing lint errors are out of scope.)

## Components

### 1. Core client extensions — `dashboard/src/lib/core-client.ts`

Typed fetchers against the core API (all return `null`/throw-safe patterns
consistent with the existing `fetchHealth`):

- `fetchVerdicts(): Promise<CoreVerdict[]>`
- `fetchJournal(date?: string): Promise<JournalEntry[]>`
- `fetchProposals(): Promise<Proposal[]>`
- `approveProposal(id): Promise<{ok:true} | {error:string}>` (422 body surfaced)
- `rejectProposal(id)`
- `fetchCoreMarkets(): Promise<CoreMarket[]>` (`GET /api/markets`)
- `fetchCoreBars(coin, tf?, n?): Promise<CoreBar[]>`
- `postChat(message, history): Promise<{reply,provider,model} | {error}>`

Types mirror the Go JSON exactly (field names from `internal/api` handlers and
`internal/metrics` / `internal/journal` structs — workers must read the Go
source, not guess).

### 2. Live stream hook — `dashboard/src/hooks/useCoreStream.ts`

One shared `coreWS` connection (client exists), fan-out by topic:
`useCoreStream(topic, handler)` registers a handler for
`bar|verdict|journal|status|mids` frames; connection is created lazily on first
subscriber, torn down on last unsubscribe, reconnects with the existing
backoff. Late subscribers don't restart the socket.

### 3. Agent console — `dashboard/src/pages/AgentPage.tsx` + `dashboard/src/components/agent/*`

Route `/dashboard/agent`, link in TopNav. Panels, top to bottom:

- **StatusStrip** — landing-style: `AGENT · RUNNING/OFFLINE`, mode
  (propose/autonomous), providers (batch/chat), core connectivity. Data:
  `useCoreHealth` + WS `status` frames.
- **RegimeBoard** — one row per tracked asset (from `/api/markets`): price +
  24h move, funding (value + spark of recent bars), OI delta spark, CVD bar
  (signed/diverging), basis, liquidation proximity. Sparks come from
  `fetchCoreBars(coin, tf, 32)` on mount and roll forward via WS `bar` frames.
  This panel is the "liquidity drivers at a glance" requirement.
- **ThesesPanel** — latest verdict per asset ranked by confidence: action chip
  (open_long/open_short/close/hold/alert), size, entry/stop/target, the written
  thesis. Seed from `/api/verdicts`, live-update from WS `verdict` frames.
- **DecisionLog** — today's journal, newest first, live-append via WS `journal`
  frames; kind-colored rows (candidate/fill/alert/error), monospace timestamps —
  the landing's DECISION LOG element made real. Date picker: simple prev/next
  day fetching `/api/journal?date=`.
- **ProposalsPanel** — pending propose-mode candidates with Approve/Reject.
  Errors (404 expired, 422 gate rejection) render inline verbatim — the gate
  name is the product story, never swallow it. Empty state: "no pending
  proposals".

### 4. Chat drawer — `dashboard/src/components/agent/ChatDrawer.tsx`

Global right-side drawer mounted in `AppShell`, toggled by a TopNav button
(next to the CORE pill). Message list + input; history kept in
`sessionStorage` (key `hypertrader-chat`), sent as the `history` array to
`POST /api/chat`. Busy state while awaiting reply; provider/model shown under
each agent reply. Core offline → input disabled with hint. Esc closes.

## Data flow

```
core API (:8787)
  /api/markets ∙ /api/bars ─────→ RegimeBoard (seed)
  /api/verdicts ────────────────→ ThesesPanel (seed)
  /api/journal?date ────────────→ DecisionLog (seed + history)
  /api/proposals ∙ approve/reject → ProposalsPanel
  /api/chat ────────────────────→ ChatDrawer
  /api/ws  (bar∙verdict∙journal∙status) → useCoreStream → live updates
```

## Error handling

- Every fetcher: network failure → `null`/empty + panel-level offline state.
- HTTP error bodies `{error}` surfaced in the owning panel, not toasts that
  vanish.
- WS drop → panels keep last data; StatusStrip flips to OFFLINE; reconnect
  restores.

## Styling

Match the existing dashboard design system (read `index.css` tokens,
`TopNav.tsx`, `MarketStatsStrip.tsx`, portfolio tables first; Geist fonts,
existing color tokens, table idioms). The console should visually echo the
landing's agent console (uppercase micro-labels, monospace numerals) without
importing new fonts or libraries. **No new dependencies.**

## Out of scope

Mandate authoring/editing UI (product roadmap, needs backend it doesn't have),
auth flows, mobile layout beyond basic responsiveness, streaming chat, tests
framework introduction, fixing pre-existing lint errors.
