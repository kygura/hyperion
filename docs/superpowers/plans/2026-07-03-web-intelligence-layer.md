# Web Intelligence Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agent console page (`/dashboard/agent`) + global chat drawer in the React dashboard, consuming the unified core API (`127.0.0.1:8787`) for regime board, theses, decision log, proposals, and chat.

**Architecture:** Pure frontend sub-project. Extend `core-client.ts` with typed fetchers; one shared WS fan-out hook; new `components/agent/*` panels composed in `AgentPage`; drawer mounted in `AppShell`. Spec: `docs/superpowers/specs/2026-07-03-web-intelligence-layer-design.md` (read it fully first).

**Tech Stack:** React 19, react-router-dom 7, Tailwind v4 tokens from `src/index.css`, Geist. **No new dependencies.**

## Global Constraints

- Work in `/home/athan/projects/hypertrader/dashboard`; use **bun** (`bun run build`, `bun run lint`).
- Git root `/home/athan/projects/hypertrader`; commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- **Type fidelity:** TS types for core responses MUST be derived by reading the Go source of truth: `tui/internal/api/read.go` + `act.go` + `server.go` (response shapes), `tui/internal/metrics/types.go` + `verdict.go` (Bar/AssetCtx/Verdict JSON tags — note Bar fields marshal with Go field names, e.g. `Coin`, `Close`, `OpenTime`), `tui/internal/journal/journal.go` (Entry), `tui/internal/executor/proposals.go` (Proposal). Do not guess field names.
- Verification bar per task: `bun run build` clean; `bun run lint` adds **zero new findings** vs the pre-existing 26 errors (compare counts before/after).
- Match existing styling idioms — read `src/index.css`, `components/shell/TopNav.tsx`, `components/dashboard/MarketStatsStrip.tsx`, `components/portfolio/TradesTable.tsx` before writing any JSX. Uppercase micro-labels + monospace ticks like the landing console; existing color tokens only.
- Offline daemon is a first-class rendered state everywhere (use `useCoreHealth`'s `online`).

---

### Task 1: Core client extensions + `useCoreStream`

**Files:**
- Modify: `dashboard/src/lib/core-client.ts`
- Create: `dashboard/src/hooks/useCoreStream.ts`

**Interfaces (produces):**
- Types: `CoreVerdict`, `CoreJournalEntry`, `CoreProposal`, `CoreMarket`, `CoreBar`, `ChatTurn {role:'user'|'assistant'; text:string}`, `ChatReply {reply:string; provider:string; model:string}`.
- Fetchers (all with `AbortSignal.timeout(5000)`, JSON `{error}` bodies surfaced): `fetchVerdicts(): Promise<CoreVerdict[] | null>`, `fetchJournal(date?: string): Promise<CoreJournalEntry[] | null>`, `fetchProposals(): Promise<CoreProposal[] | null>`, `approveProposal(id: string): Promise<string | null>` (returns error message or null on success), `rejectProposal(id: string): Promise<string | null>`, `fetchCoreMarkets(): Promise<CoreMarket[] | null>`, `fetchCoreBars(coin: string, tf?: string, n?: number): Promise<CoreBar[] | null>`, `postChat(message: string, history: ChatTurn[]): Promise<ChatReply | {error: string}>` (60s timeout — LLM call).
- `useCoreStream(topic: 'bar'|'verdict'|'journal'|'status'|'mids', handler: (data: unknown) => void): void` — module-level shared socket via the existing `coreWS`, refcounted (lazy connect on first subscriber, close on last), handler registry keyed by topic; handler identity changes must not reconnect (keep handler in a ref).

- [ ] **Step 1:** Read the Go sources listed in Global Constraints; write the TS types with a comment naming the Go source file each mirrors.
- [ ] **Step 2:** Implement fetchers + `useCoreStream`.
- [ ] **Step 3:** `bun run build` clean; lint count unchanged.
- [ ] **Step 4:** Commit: `feat(dashboard): typed core fetchers + shared WS stream hook`

### Task 2: Agent console page — route, StatusStrip, RegimeBoard

**Files:**
- Create: `dashboard/src/pages/AgentPage.tsx`, `dashboard/src/components/agent/StatusStrip.tsx`, `dashboard/src/components/agent/RegimeBoard.tsx`
- Modify: `dashboard/src/main.tsx` (route `/dashboard/agent`), `dashboard/src/components/shell/TopNav.tsx` (nav link `AGENT`)

**Interfaces:**
- Consumes Task 1 fetchers/hook + existing `useCoreHealth`.
- `AgentPage` composes panels vertically; passes shared health.
- StatusStrip: `AGENT · RUNNING` (green) / `AGENT · OFFLINE` (dim) + mode + `batch/chat` providers + version; landing-console typography.
- RegimeBoard: table, one row per market from `fetchCoreMarkets()` (30s repoll + roll forward via `useCoreStream('bar', ...)`): coin, last close + Δ%, funding (numeric + inline SVG sparkline of last 32 bars' funding), OI Δ spark, CVD signed bar (diverging around zero), basis, liq proximity. Sparks seeded via `fetchCoreBars(coin, undefined, 32)` per coin on mount (parallel, tolerate individual failures). Offline → skeleton rows + banner. Inline SVG sparklines hand-rolled (~20 lines) — no chart lib on this page.

- [ ] **Step 1:** Read styling reference files (Global Constraints). Build StatusStrip + page scaffold + route + nav link; verify in build.
- [ ] **Step 2:** Build RegimeBoard with seed + live roll-forward.
- [ ] **Step 3:** `bun run build` clean; lint count unchanged. Manual smoke if daemon reachable: `cd tui && ./hyperagent -headless -testnet &`, `bun run dev`, check /dashboard/agent renders board with live data; kill daemon → offline banner within 10s. Record what you saw.
- [ ] **Step 4:** Commit: `feat(dashboard): agent console — status strip + liquidity regime board`

### Task 3: ThesesPanel, DecisionLog, ProposalsPanel

**Files:**
- Create: `dashboard/src/components/agent/ThesesPanel.tsx`, `dashboard/src/components/agent/DecisionLog.tsx`, `dashboard/src/components/agent/ProposalsPanel.tsx`
- Modify: `dashboard/src/pages/AgentPage.tsx` (compose)

**Interfaces:**
- ThesesPanel: seed `fetchVerdicts()`, live `useCoreStream('verdict', ...)` (replace per asset, re-rank by confidence desc). Row: action chip (color by action: long=green, short=red, close/scale=amber, hold/alert=neutral), asset, confidence, size, entry/stop/TP, thesis text (wrap, full text — the written judgment is the product). Empty: "no theses yet — agent reasons on batch closes".
- DecisionLog: seed `fetchJournal()` (today), newest-first, live-prepend via `useCoreStream('journal', ...)`; monospace timestamp, kind badge (candidate/fill/open/close/alert/error with existing token colors), summary. Prev/next day buttons refetch `fetchJournal(date)`; live-prepend only on today.
- ProposalsPanel: `fetchProposals()` on mount + 15s repoll + refetch after any action; per row: verdict summary + expires countdown + Approve/Reject buttons calling Task 1 helpers; error string from 404/422 rendered inline under the row verbatim; buttons disabled while in flight. Empty state: "no pending proposals".

- [ ] **Step 1:** Build the three panels; compose in AgentPage (order: StatusStrip, RegimeBoard, ThesesPanel, ProposalsPanel, DecisionLog).
- [ ] **Step 2:** `bun run build` clean; lint count unchanged.
- [ ] **Step 3:** Smoke with daemon: journal panel shows today's entries (there are real entries in `tui/data/journal/2026-07-03.ndjson`); proposals empty state renders. Record output.
- [ ] **Step 4:** Commit: `feat(dashboard): theses, decision log, proposals panels`

### Task 4: ChatDrawer + integration + full verification

**Files:**
- Create: `dashboard/src/components/agent/ChatDrawer.tsx`
- Modify: `dashboard/src/components/shell/AppShell.tsx` (mount drawer + state), `dashboard/src/components/shell/TopNav.tsx` (toggle button beside CORE pill)

**Interfaces:**
- Drawer: fixed right panel (~380px, full height, over content, existing surface tokens + border), header `AGENT CHAT` + provider/model of last reply + close (Esc and ×). Messages from `sessionStorage['hypertrader-chat']` (JSON `ChatTurn[]`), user right-aligned/agent left, auto-scroll to bottom on append. Input: Enter sends (`postChat(message, history)` with history = stored turns), disabled + "agent is thinking…" while pending, `{error}` rendered as system row. Offline (`useCoreHealth`) → input disabled, hint "daemon offline — start hyperagent".
- TopNav toggle: chat glyph button; drawer open state in AppShell via useState (not router).

- [ ] **Step 1:** Build drawer + wiring.
- [ ] **Step 2:** Full verification: `bun run build` clean; `bun run lint` — zero new findings vs baseline 26; daemon smoke: send a chat message (DEEPSEEK key exists in `tui/.env`; if the call fails, capture the exact error JSON and report — do not mask).
- [ ] **Step 3:** Commit: `feat(dashboard): global agent chat drawer`
- [ ] **Step 4:** Update `dashboard/README.md` (or create a short one if the Vite boilerplate remains): one section "Agent console" — what it shows, that it needs the daemon running, VITE_CORE_URL override. Commit: `docs(dashboard): agent console notes`
