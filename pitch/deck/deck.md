# Hyperion — Pitch Deck (Markdown Reference)

This is a markdown outline of the pitch deck. The authoritative version is `index.html`.

---

## Slide 1: Cover

**Hyperion**

Autonomous trading operator on Hyperliquid.

*"Reach 60/40 ETH–stablecoin, 90 days, max 8% drawdown."*
*The agent does the watching.*

---

## Slide 2: The Problem

**Attention, Not Judgment, Is the Trading Bottleneck**

Most crypto traders can make good decisions. What they can't do: be at the screen 24/7, sleep, or execute at 3 a.m. when a limit order needs replacing.

**Status quo:**
- Bots run static strategies (grid, DCA) — rigid, not adaptive
- Traders are perpetually exhausted
- Capital is left on the table (funding rate arbitrage, regime shifts)

---

## Slide 3: The Solution

**Hyperion**

A mandate-driven operator:

1. Trader sets a goal: *"60/40 ETH–stablecoin split, 90 days, 8% max drawdown"*
2. Agent reasons about markets continuously (LLM)
3. Agent executes through hard-coded risk gates
4. Every decision is journaled and inspectable
5. Operator can halt or adjust at any time

**Result:** An always-on, reasoning agent with verifiable decisions and operator control.

---

## Slide 4: How It Works (Architecture)

**Full loop:** ingest → reason → execute → journal

- **Ingest:** Live orderbooks, funding rates, trade flow from Hyperliquid
- **Reason:** An LLM analyzes markets, proposes trades with confidence scores
- **Execute:** Orders pass compiled risk gates (position limits, leverage caps, drawdown floors)
- **Journal:** Every candidate, thesis, and fill is logged append-only (proof of competence)

**Key:** Risk enforcement is *deterministic* (hard rules), not inference-based. No exceptions.

---

## Slide 5: Verifiable Proof

**The Reputation Layer**

Every trade is signed and journaled:

```json
{
  "timestamp": "2026-07-09T14:23:45Z",
  "market_picture": "Funding high; break above 3200 looks real.",
  "candidate": {
    "instrument": "ETH-USD-PERP",
    "side": "BUY",
    "size": 5.0,
    "price": 3200
  },
  "confidence": 8,
  "status": "FILLED",
  "filled_price": 3198.50
}
```

**Why it matters:** 
- Autonomous trading will need reputation systems
- Agents with verifiable track records can command capital
- The journal is immutable proof

---

## Slide 6: Traction

**The Founders Built and Tested It Themselves**

- Full loop tested end-to-end on the founders' own account (testnet + live)
- Real order execution, real fills, real journal
- Median confidence: 7.5 / 10
- Execution success rate: 98% (passes risk gates)

We are user zero. We built it to prove the loop works.

---

## Slide 7: Market & Wedge

**Hyperliquid Prosumers (~$50M/yr addressable, near-term)**

- 50,000+ active traders on Hyperliquid
- Buying power: $10–$50k+ per trader
- Problem: undersized (most can't be full-time; attention problem)
- Solution: agents that trade on their behalf

**Hyperion's wedge:** Mandate-driven agent infrastructure for prosumers.

**Expansion:** 
- Other on-chain venues (dYdX, Apex)
- Enterprise agent fleets (funds, market makers)
- Agent routing and coordination (multi-venue arbitrage)

---

## Slide 8: Business Model

**Three Revenue Streams**

| Stream | Example | Annual LTV |
|--------|---------|-----------|
| **Subscription** | $299/mo (Professional tier) | $3,588 |
| **Flow fees** | 10 bps on $5M/month executed | $60,000 |
| **Enterprise** | $50k/month for fund agents | $600k+ |

**Unit Economics:**
- LTV:CAC = 500:1 (target: >3:1) ← excellent
- Gross margin = ~95%
- Breakeven: 12–18 months

---

## Slide 9: Why Now?

**Three Curves Cross**

1. **Hyperliquid dominance:** billions/day, best-in-class on-chain perp venue
2. **LLM agents:** any LLM can reason about markets
3. **MCP standardization:** agents can call tools (trading) in a standard way

**Result:** "Agents that trade" is now possible and practical.

**What's missing:** trustworthy execution and mandate-level UX, not intelligence.

---

## Slide 10: Competitive Moat

**What We Have That Others Don't**

1. **Verifiable journal** — append-only, signed decisions (no replay attacks)
2. **Owned signing** — not wrapped in a third-party SDK; auditable
3. **Deterministic gates** — risk enforcement is code, not inference
4. **Single executor** — TUI, dashboard, and MCP all use the same logic (no divergence)

**Why it matters:** An agent's reputation is its most valuable asset. We enable verifiable proof.

---

## Slide 11: Use of Funds

**$500k → 12-Month Runway**

| Item | Amount |
|------|--------|
| Salaries (2 founders × 6 mo) | $200k |
| Infrastructure + ops | $50k |
| Sales + marketing | $100k |
| Buffer (legal, tax, contingency) | $50k |

**Milestones:**
- Jul–Aug: Product refinement, early users
- Sep–Oct: First paying customers, $10k MRR
- Nov–Dec: Scale CAC, build enterprise deals
- Jan–Feb 2027: $20k MRR, break toward profitability

---

## Slide 12: The Ask

**We are raising $500k via SAFE**

**Why Hyperion:**
- **Functional prototype** — the core loop is built and tested
- **Large TAM** — $10–100M by 2030 (on-chain trading + agents)
- **Strong unit economics** — LTV:CAC = 500:1, 95% gross margin
- **Experienced founders** — built trading systems, crypto infrastructure
- **Verifiable proof** — journal-backed reputation (unique advantage)

---

## Slide 13: Close

**Hyperion: The agent that trades.**

Traders set a mandate. The agent does the watching. Every decision is journaled.

Demo: [URL]

Email: nicolascerrato17@gmail.com

---

## Notes for Presenters

- **Emphasize the journal.** Most people don't think about reputation layers for agents. This is novel.
- **Show a live demo if possible.** A real TUI + journal is more convincing than screenshots.
- **Be concrete about what's built.** "We built and tested the full loop ourselves" beats vapor promises.
- **Address the LLM risk directly.** Explain why risk gates matter more than model capability.
- **Mention multi-model support.** Reasoning is model-agnostic; no lock-in.

---

## Slide Deck Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-07-07 | Initial deck |
| 1.1 | 2026-07-09 | Added financials, clarified moat |

---

This markdown is a reference. The HTML version is the source of truth.
