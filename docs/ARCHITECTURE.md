# Hyperion Architecture

**Full loop:** ingest → reason → execute → journal

Hyperion is a mandate-driven autonomous trading operator. A trader states a goal ("reach 60/40 ETH–stablecoin, 90 days, 8% max drawdown") and the system watches, reasons, and executes — all decisions journaled and inspectable.

---

## System Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         TRADER (Operator)                              │
│                                                                         │
│  Web App               TUI Cockpit              MCP Client (Claude)   │
│  (React)               (Lipgloss)               (Any LLM)             │
└────────┬─────────────────┬──────────────────────┬──────────────────────┘
         │                 │                      │
         └─────────────────┴──────────────────────┴─────────────────────┐
                                                                        │
                    HTTP + WebSocket (:8787)                           │
                                                                        ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        BACKEND DAEMON (Go)                              │
│                                                                         │
│  ┌───────────────────┐  ┌──────────────────┐  ┌──────────────────────┐ │
│  │  Market Ingestion │  │   Event Bus &    │  │  Reasoning Loop      │ │
│  │                   │  │   State Manager  │  │  (LLM Reasoning)     │ │
│  │ • Hyperliquid WS  │  │                  │  │                      │ │
│  │ • Order books     │  │ • Event pub/sub  │  │ • Schema validation  │ │
│  │ • Funding rates   │  │ • Position       │  │ • Confidence scoring │ │
│  │ • Recent trades   │  │   tracking       │  │ • Candor scoring     │ │
│  └───────────────────┘  └──────────────────┘  └──────────────────────┘
│
│  ┌───────────────────┐  ┌──────────────────┐  ┌──────────────────────┐ │
│  │  Deterministic    │  │   EIP-712 Signer │  │  Append-Only Journal │ │
│  │  Execution        │  │                  │  │                      │ │
│  │                   │  │ • Hyperliquid    │  │ • All candidates     │ │
│  │ • Risk gates      │  │   signing (owned)│  │ • All theses         │ │
│  │ • Order placement │  │ • Scoped agent   │  │ • All fills          │ │
│  │ • Order mgmt      │  │   wallet         │  │ • All decisions      │ │
│  │ • Position mgmt   │  │ • Byte-exact     │  │ • Verifiable proof   │ │
│  │                   │  │   reference      │  │                      │ │
│  └───────────────────┘  └──────────────────┘  └──────────────────────┘
│
│  MCP Server: exposes trading tools (read_markets, read_positions,     │
│  place_order, cancel_order) — Claude or any MCP client trades         │
│  through the same gates and signer                                    │
└─────────────────────────────────────────────────────────────────────────┘
         ▲
         │ REST + WebSocket
         │
         ▼
    Hyperliquid API
    (On-chain markets)
```

---

## Component Overview

### Backend Daemon (`backend/`)

**Language:** Go  
**Port:** :8787 (HTTP + WebSocket)

#### Event Bus Architecture

Hyperion's core is event-driven. A central `EventBus` carries:
- Market updates (orderbooks, funding, trades)
- Position changes (fill confirmations)
- Reasoning outputs (candidates, theses, confidence)
- Execution events (orders placed, cancelled, filled)

Components subscribe to events, react to them, and publish new events. No shared mutable state outside the bus — all state mutations are explicit and journaled.

#### Market Ingestion

- **Hyperliquid WebSocket:** live orderbooks, funding rates, recent trades on subscribed perpetuals
- **Snapshot strategy:** ingest at 500ms intervals; rebuild full state on each update
- **Market metrics:** compute depth, spread, directional flow (buying/selling pressure)

#### Reasoning Loop

The reasoning step runs on a configurable cadence (every 1–5 minutes). The loop:

1. **Gather state:** pull current market data and positions from the bus
2. **Format context:** write a natural-language market picture (regime, theme, recent action)
3. **Call LLM:** send market picture + mandate to Claude/OpenAI/Deepseek
4. **Parse output:** extract candidate trades (limit orders, market orders, cancellations)
5. **Validate schema:** ensure every candidate parses; reject malformed responses
6. **Score confidence:** ask the model to self-score (1–10) and explain candor check (is this real or hedging?)
7. **Propose or execute:** in propose mode, show candidates; in autonomous mode, gate and execute

#### Execution Layer

Orders run through **compiled risk gates** before hitting the wire:

- Position size limits (per-instrument and gross)
- Leverage caps (never exceed 2× gross notional)
- Loss limits (daily or mandate-horizon drawdown floor)
- Order quality checks (size, price sanity, slippage guards)
- Scoped wallet signing (agent cannot withdraw, only trade)

Every gate is a hard rule: no exceptions, no inference, no fudging. If an order fails any gate, it is rejected with a reason.

#### EIP-712 Signing

Hyperion includes its own ~300-line EIP-712 implementation. Every order is:

1. Serialized to the Hyperliquid order schema
2. Hashed and signed via ECDSA (keccak256 + secp256k1)
3. Verified byte-exact against Hyperliquid's reference vectors
4. Sent to the wire with a scoped agent key (can trade, cannot withdraw)

The signing is owned (not wrapped SDK). This means Hyperion's signing can be audited, the wallet is provably scoped, and the proof layer is tight.

#### Append-Only Journal

Every candidate, thesis, decision, and fill is journaled:

```json
{
  "timestamp": "2026-07-09T14:23:45Z",
  "event_type": "candidate",
  "reasoning_cycle": 42,
  "market_picture": "Funding is high; break above 3200 in ETH looks real...",
  "candidate": {
    "instrument": "ETH-USD-PERP",
    "side": "BUY",
    "size": 5.0,
    "price": 3198,
    "order_type": "LIMIT"
  },
  "confidence": 7,
  "candor": "Real conviction; regime fits our mandate.",
  "status": "PROPOSED"
}
```

The journal is proof for reputation: agents' decisions are traceable, explainable, and verifiable. No rewriting history.

#### Metrics & Observability

- **Prometheus endpoint** (`/metrics`) exports key metrics: reasoning cycle time, orders placed/filled/cancelled, position P&L, drawdown, Sharpe ratio
- **Event logs** per instrument: entry/exit decisions, fill vs. reject reasons, slippage
- **Journal access** via HTTP (`/journal`) for full audit trail

---

### TUI Cockpit (`tui/`)

**Language:** Go (Bubble Tea + Lipgloss v2)

The operator's interface. Five panels + command bar:

1. **MANDATE** — goal, horizon, risk envelope in plain text
2. **MARKET PICTURE** — orderbooks, funding rates, recent trade flow, regime label
3. **EXECUTION** — open orders, position, unrealized P&L, daily/mandate drawdown
4. **THESES** — running agent reasoning; confidence scores; candor notes
5. **DECISION JOURNAL** — scrollable history of proposals and fills

**Command bar** (`/` to toggle):
- Slash commands: `/halt`, `/veto <order_id>`, `/propose`, `/autonomous`
- Mode toggle: `m` to switch between propose and autonomous
- Navigation: arrow keys, `q` to quit

The TUI talks to the backend over HTTP+WS. No local state; all display is derived from the daemon's event stream.

---

### Dashboard (`dashboard/`)

**Language:** React (TypeScript)

A web UI for remote monitoring and mandate adjustment. Can be deployed separately or hosted on the same domain as the landing page.

Key pages:
- **Agent Console:** live event feed from the daemon (WS push)
- **Markets:** order book and depth chart
- **Positions:** portfolio composition, history, P&L breakdown
- **Orders:** open/closed order history with fill details
- **Journal:** searchable decision journal with explanations

Builds on top of the same HTTP+WS API as the TUI.

---

### MCP Server

Exposes trading as a set of tools that any MCP client (Claude, Cursor, etc.) can call:

```typescript
// read_markets(instrument: string) → orderbook, funding, recent trades
read_markets("ETH-USD-PERP")

// read_positions() → current holdings, P&L, exposure
read_positions()

// place_order(side, size, price, order_type) → confirmation or rejection
place_order("BUY", 5.0, 3195, "LIMIT")

// cancel_order(order_id) → confirmation
cancel_order("order-abc123")
```

All calls go through the same risk gates and signing layer. The MCP server is a thin adapter; it doesn't bypass any executor logic.

---

## Data Flow

### Ingest (Continuous)

```
Hyperliquid WS → Market Event → Event Bus → Subscribers
                                              ├─ Storage (metrics)
                                              ├─ Display (TUI/dashboard)
                                              └─ Reasoning (market picture builder)
```

### Reason (On Cadence)

```
Market Picture ─→ LLM (Claude/OpenAI/Deepseek) ─→ Candidates
     ↓                                                  ↓
  (formatted)                                  (schema-validated)
                                                       ↓
                                               Confidence Score
                                                       ↓
                                              Candor Check
                                                       ↓
                                         Proposed Candidates → Event Bus
                                                                   ↓
                                                         [propose mode]
                                                         Show on TUI
                                                         or
                                                         [autonomous mode]
                                                         Execute
```

### Execute (Gate → Wire)

```
Candidate ─→ Risk Gates ─→ (pass/fail) ─→ [PASS] ─→ EIP-712 Sign ─→ Place Order ─→ Hyperliquid
                              ↓                                            ↓
                            [FAIL]                                     Success → Journal
                              ↓                                            ↓
                           Reject                                      Event Bus
                             ↓
                           Journal
                             ↓
                          Event Bus
```

### Journal (Append-Only)

```
Every event → JSON line append to journal file → No deletes, no updates
                                                  Read by: auditors, replay, reputation layer
```

---

## Risk Architecture

### Gates (Compiled, Not Inferred)

Every gate is a **hard rule**:

- **Position limits:** max 50 ETH per instrument, 100 BTC gross exposure
- **Leverage:** never exceed 2× notional vs. collateral
- **Daily drawdown:** don't exceed 5% of account on any calendar day
- **Mandate-horizon drawdown:** respect the 8% (or user-specified) floor over the entire mandate period
- **Order quality:**
  - Limit orders: price within 10% of mid
  - Market orders: max 2% slippage vs. mark
  - Rejected if size = 0 or price is nonsense

### Scoped Wallet

The agent trades with a **scoped ECDSA key** that:
- ✅ CAN place orders (perps only)
- ✅ CAN cancel orders
- ✅ CAN withdraw from account if needed (for rebalance, not programmed)
- ✗ CANNOT withdraw to external addresses (prevented by Hyperliquid signing)
- ✗ CANNOT change leverage or risk settings globally
- ✗ CANNOT sub-sign for other accounts

### Operator Override

The operator is always in control:

- **Halt:** stop all reasoning and cancel all open orders
- **Veto:** reject a candidate before it executes (propose mode only)
- **Mandate adjustment:** change the goal/horizon/risk envelope and re-run the loop
- **Mode switch:** toggle between propose (show candidates) and autonomous (execute)

---

## Deployment

### Backend

```bash
cd backend
cp .env.example .env
# Set: HL_AGENT_KEY, HL_ACCOUNT_KEY, HL_PUBLIC_MASTER_ACCOUNT, 
#      OPENAI_API_KEY or similar for reasoning backend, LOG_LEVEL

go build -o hyperagent ./src
./hyperagent -testnet     # testnet mode, propose-only
# or
./hyperagent              # mainnet (if keys are mainnet)
```

Server runs on `:8787`.

### TUI

```bash
cd tui
go build -o hyperagent-tui ./src
./hyperagent-tui -core-url http://127.0.0.1:8787
```

### Dashboard

```bash
cd dashboard
npm install
npm run build
npm run dev    # local dev
# or
npm run preview   # production preview
# Deploy dist/ to Vercel/Cloudflare
```

### MCP Registration

```bash
claude mcp add hyperion -- ./backend/hyperagent mcp -address 0x...
```

Then use Claude or any MCP client.

---

## Security Model

### What We Trust

- **Hyperliquid's API:** we assume order placement and fills are accurate
- **Market data feed:** we ingest live orderbooks; stale/stale=wrong data breaks assumptions
- **LLM reasoning:** we assume Claude/OpenAI output is a good-faith trade candidate (we validate schema; we gate on risk)
- **Operator judgment:** the operator can halt, veto, or adjust the mandate

### What We Don't Trust

- **LLM to enforce risk:** every candidate passes compiled gates, never inference-based checks
- **Free-text responses:** all reasoning output is schema-validated; rejected if malformed
- **Scoped wallet signing:** we own the signing; no hidden features
- **Agent wallet to steal funds:** signing is scoped to trading only; cannot withdraw to other addresses

---

## Testing

The project includes unit tests for:
- EIP-712 signing (byte-exact verification against Hyperliquid reference)
- Risk gates (position limits, leverage caps, drawdown floors)
- Event bus pub/sub mechanics
- Schema validation for candidates
- Journal writes and reads

Run tests:

```bash
cd backend
go test ./...
```

---

## Monitoring

- **Prometheus metrics** on `:8787/metrics` — integrate with Grafana
- **Event logs** streamed to the TUI and dashboard in real time
- **Journal** available via HTTP (`/journal`) and on disk

Key alerts:
- Reasoning cycle timeout
- Risk gate rejection spike
- Execution latency (order → wire)
- Funding rate divergence (might indicate market stress)
