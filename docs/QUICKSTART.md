# Quickstart

Get Hyperion running in 5 minutes.

---

## Prerequisites

- **Go 1.21+** (backend + TUI)
- **Node.js 18+** (dashboard; optional)
- **Hyperliquid testnet or mainnet account** with API keys
- One of: **Claude API key** (recommended), OpenAI API key, or Deepseek API key

## Step 1: Backend (Daemon)

```bash
cd backend

# Copy the example env and add your keys
cp .env.example .env

# Edit .env and fill in:
# - HL_AGENT_KEY and HL_ACCOUNT_KEY (Hyperliquid API)
# - HL_PUBLIC_MASTER_ACCOUNT (your account address)
# - ANTHROPIC_API_KEY (Claude reasoning)
# - (optional) OPENAI_API_KEY, DEEPSEEK_API_KEY for alternate models

# Build
go build -o hyperagent ./src

# Run in testnet mode (propose-only, no live capital)
./hyperagent -testnet

# Or run in autonomous mode on testnet
./hyperagent -testnet -autonomous
```

The daemon starts on `http://localhost:8787`.

### Verify It's Running

```bash
# In another terminal
curl http://localhost:8787/health
# Should return 200 OK

# Check the event feed
curl http://localhost:8787/journal
# Should return JSON array of journal events
```

---

## Step 2: TUI Cockpit (Operator Interface)

```bash
cd tui

# Build
go build -o hyperagent-tui ./src

# Run (backend must be running)
./hyperagent-tui -core-url http://127.0.0.1:8787
```

You should see five panels:
1. **MANDATE** — goal, horizon, risk envelope (edit in propose mode)
2. **MARKET PICTURE** — live orderbooks, funding rates
3. **EXECUTION** — open orders, position, P&L
4. **THESES** — running reasoning candidates
5. **DECISION JOURNAL** — history of proposals and fills

### Keyboard Shortcuts

- **`/`** — open command bar (type `/halt`, `/propose`, `/autonomous`, etc.)
- **`m`** — toggle mode (propose ↔ autonomous)
- **`arrow keys`** — navigate and scroll
- **`q`** — quit

---

## Step 3: Dashboard (Optional)

```bash
cd dashboard

# Install and build
npm install
npm run build
npm run dev    # local dev server on :5173
```

Open `http://localhost:5173`. You'll see:
- Live event feed (connected to backend via WebSocket)
- Markets and positions
- Decision journal with explanations

---

## Step 4: Set a Mandate

In the TUI, edit the mandate (propose mode):

```
GOAL: Reach 60/40 split between ETH and USDC
HORIZON: 90 days
MAX DRAWDOWN: 8%
LEVERAGE CAP: 2×
```

Press Enter to confirm.

---

## Step 5: Run in Propose Mode (Recommended for First Run)

The TUI starts in **propose mode**: the backend reasons about markets, generates candidates, but does NOT execute. You review each candidate and decide whether to:

- **Accept** — the order executes (passes all risk gates first)
- **Veto** — reject it
- **Ask for more candidates** — re-run the reasoning loop

This is how you build confidence in the system.

### Switch to Autonomous

Once you're comfortable, press `m` to toggle to **autonomous mode**. The daemon now:
- Reasons on schedule (every 1–5 min, configurable)
- Automatically executes candidates that pass all risk gates
- All decisions are journaled
- You can still **halt** at any time

---

## Step 6: Watch the Decision Journal

Press `5` in the TUI to jump to the **DECISION JOURNAL** panel. Scroll through proposals and fills:

```
2026-07-09 14:23:45
  CANDIDATE: BUY 5 ETH at 3200 (LIMIT)
  Reasoning: Funding is high; break above 3200 looks real. Vol is reasonable.
  Confidence: 8/10
  Status: PROPOSED → ACCEPTED → FILLED at 3198

2026-07-09 14:48:12
  CANDIDATE: SELL 2.5 ETH at 3220 (LIMIT)
  Reasoning: Take profit; rebalance toward USDC.
  Confidence: 6/10
  Status: PROPOSED → VETO (drawdown check flagged 4.2%)
  Rejection reason: Daily drawdown would exceed 5% if unfilled; skipped.
```

Every decision is append-only and immutable.

---

## Step 7: MCP Integration (Use Claude)

Register the MCP server:

```bash
# Replace 0x... with your Hyperliquid account address
claude mcp add hyperion -- ./backend/hyperagent mcp -address 0x...
```

Then in Claude (web or Claude Code):

```
You: Read the latest markets and current positions, then propose a thesis.

Claude (via MCP):
Tools: read_markets("ETH-USD-PERP"), read_positions()
Result: Current exposure is 10 ETH long, 50k USDC. Funding on ETH is 0.08% annualized.
        Orderbook shows thin depth above 3210. Propose: sell 3 ETH at 3210 to rebalance.

Then place the order:
place_order("SELL", 3.0, 3210, "LIMIT")
```

All orders go through the same risk gates and signer. No bypassing.

---

## Troubleshooting

### "Connection refused" when TUI tries to connect

- Verify backend is running: `curl http://localhost:8787/health`
- Check the `-core-url` flag: `./hyperagent-tui -core-url http://127.0.0.1:8787`

### "Invalid API key" or "Unauthorized"

- Double-check `.env` in the `backend/` folder
- Verify keys are for Hyperliquid (not Binance, etc.)
- Ensure the account is on testnet if you're running with `-testnet`

### "Orders are being rejected (gate failed)"

- Check the rejection reason in the journal
- Likely: position size would exceed limit, or daily drawdown floor is hit
- Adjust the mandate or close existing positions

### TUI is blank or stuck

- Kill it: `Ctrl+C`
- Check backend logs: `./hyperagent -testnet 2>&1 | grep -i error`
- Restart both

### Reasoning loop is not running

- Check the reasoning cadence in `config.toml` (default: 5 minutes)
- Verify LLM API key in `.env` is valid
- Check backend logs for reasoning errors

---

## Next Steps

- **Read** `ARCHITECTURE.md` to understand how the system works
- **Review** `docs/YC-APPLICATION.md` for application context and the demo plan
- **Explore** the decision journal to build intuition for the reasoning quality
- **Customize** `config.toml` in the backend to adjust risk gates, reasoning cadence, and venue subscriptions
