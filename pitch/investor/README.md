# Investor Materials — Hyperion

This directory contains documentation for potential investors (YC, angels, funds, etc.).

---

## What is Hyperion?

An autonomous trading operator on Hyperliquid. Traders state a mandate in plain language — "reach 60/40 ETH–stablecoin, 90 days, max 8% drawdown" — and the agent does the watching, reasoning, and executing. Every decision is journaled and inspectable.

---

## Key Documents

### 1. **PITCH.md** (Founder Narrative)

The authored pitch copy. Do not modify. This is what goes in the deck and landing page.

### 2. **FINANCIAL-PLAN.md** (Revenue Model & Unit Economics)

How Hyperion makes money:
- Subscription (per agent per month)
- Basis points on executed flow
- Enterprise licenses for funds

Addressable market, unit economics, and margin model.

### 3. **../YC-APPLICATION.md** (YC Batch Prep)

Timeline, application requirements (demo, founder video, etc.), and draft answers for YC's form.

---

## Public Materials

- **Landing page:** `pitch/pitch.html` (deployed URL TBD)
- **Live demo:** recorded TUI demo + dashboard screen capture (URL TBD)
- **Founder video:** unlisted YouTube link (to be recorded)
- **GitHub repo:** public repo with MIT license (optional; YC doesn't require it)

---

## Private Materials (Not Shared)

- `.env` files (API keys) — rotate before sharing repo
- Full journal (daily trades) — share only with deep due diligence partners
- Backend signing implementation — share only to trusted engineers for security audit

---

## Messaging Guidelines

### For Investors (What to Emphasize)

1. **Working product in daily use** — the founders trade using this system themselves
2. **Verifiable proof layer** — append-only journal with byte-exact signatures
3. **Wedge market** — Hyperliquid's prosumer base (~$50M/yr near-term addressable)
4. **Expanding TAM** — agents that trade; eventually enterprise agent fleets for funds
5. **Competitive moat** — owned signing, verifiable journal, single executor (no SDK lock-in)

### What NOT to Say

- "AI trading bot" (too generic; every bot claims AI)
- "We predict markets" (you don't; you manage to a mandate)
- "Unrealized P&L" (focus on realized; unrealized is just mark price)
- "We'll be the Robinhood of crypto" (focus on agent infrastructure, not retail brokerage)

---

## Timeline

| By | Milestone | Owner |
|----|-----------|----|
| July 11 | Landing page deployed; demo URL live | — |
| July 18 | Founder video recorded and uploaded | Founders |
| July 21 | YC application submitted | Founders |
| July 27 | YC application deadline | — |
| Aug 28 | YC decision day | — |

---

## Contact

**Email:** nicolascerrato17@gmail.com

**Discord/Twitter:** [@hyperion](https://twitter.com/...) (TBD)

---

## FAQ (For Investors)

### How does Hyperion make money?

Three streams:
1. **Subscription:** hosted agent ($99–$999/month depending on allocation limit)
2. **Flow fee:** 5–20 bps on autonomously executed notional
3. **Enterprise:** white-label licenses for trading firms with agent fleets

### What's your initial customer?

You. The founders. The system trades real capital daily (testnet and mainnet). The journal is your proof.

### How is this different from 3Commas, TradingView Pine Script bots, etc.?

Those ship **static strategies** (grid, DCA, etc.). Hyperion is **reasoning-driven** — Claude reasons about markets and adapts. Plus: verifiable journal (they don't have this), deterministic risk gates (they rely on inference), and no SDK lock-in (MCP is open).

### What about regulatory risk?

Hyperion operates within Hyperliquid's terms. Hyperliquid is a fully licensed venue (FTX's on-chain successor). No margin lending, no custody risk (traders hold their own keys). Compliance is simpler than tradfi.

### Can you get sued?

Unlikely. The system:
- Never loses money the operator doesn't authorize (risk gates are deterministic)
- Provides full decision journal (transparency defense)
- Gives operator override at all times (user choice, not forced automation)

### What if the LLM gives bad trades?

Two layers of defense:
1. Schema validation (malformed responses rejected)
2. Risk gates (even good trades can be rejected if they violate the mandate)

In propose mode, the operator reviews before accepting.

### How long until profitability?

Depends on customer acquisition. Unit economics are attractive: $0.50–$2 per user per month in API costs, $99–$999 monthly subscription, 5–20 bps on flow. Breakeven is ~100 active users.

### Why Hyperliquid and not Binance/Deribit?

Hyperliquid is where the crypto-native traders are. On-chain, public API, no middleman. Easier API integration, higher uptime. Binance/Deribit are traditional-finance-style (centralized, slow APIs, margin lending).

### How do you compete with Wintermute, Jump, etc.?

You don't — not directly. Those are quant trading desks with humans, HFT, and market-making. Hyperion is agent infrastructure: for the 50k traders on Hyperliquid who want an AI assistant. Different market.

### What's the core IP?

1. **Mandate-driven interface** (not order tickets)
2. **Append-only journal** (verifiable proof)
3. **Deterministic gates** (not inference-based risk)
4. **Owned signing** (auditability)
5. **MCP integration** (standard LLM protocol)

Hard to copy all five; easy to copy one or two. The moat is the combination.

---

## Next Steps

1. **Demo:** record TUI walk-through (VHS tool, 30 min setup)
2. **Landing page:** deploy `pitch.html` to Vercel
3. **Video:** all founders on camera, ~1 min, no script
4. **Application:** fill YC form; submit by July 24
5. **Follow-up:** be ready to discuss with partners; demo to early users

---

## Investor Deck (Outline)

If you're prepping a deck, use this structure:

1. **Problem:** attention, not judgment, is the trading bottleneck
2. **Solution:** Hyperion (agent + mandate + journal)
3. **Traction:** founders trade daily using the system
4. **Market:** Hyperliquid prosumers, $50M/yr near-term
5. **Business Model:** subscription + flow fees + enterprise
6. **Competitive Edge:** owned signing, verifiable journal, single executor
7. **Ask:** $500k seed (or your target)
8. **Use of Funds:** 6–12 months product/customer dev

---

Authored: 2026-07-09  
Do not modify or rewrite this guidance.
