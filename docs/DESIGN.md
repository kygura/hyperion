# Product & Design Decisions

This document records the core design decisions and the trade-offs that shaped Hyperion.

---

## Mandate-Driven Interface (Not Order Tickets)

**Decision:** The interface is a mandate (goal + horizon + risk envelope), not an order ticker.

**Why:** Trading is an extended endeavor with a purpose. A trader doesn't want to place 100 manual orders over 3 months; they want to say "reach 60/40 split between ETH and stablecoins in 90 days, max 8% drawdown" and let an agent do the watching. The mandate is the contract between trader and agent. All reasoning and execution serves the mandate.

**Trade-off:** Harder to build (must bind reasoning to a goal), but more focused and less prone to FOMO (every decision is evaluated against the mandate, not the trader's emotion).

---

## Deterministic Execution (Compiled Gates, Not Inference-Based Checks)

**Decision:** Every order passes **compiled risk gates** (hard rules) before wire. No inference, no "feel," no exceptions.

**Why:** An LLM can reason beautifully about markets but is not suited for risk enforcement. We want traders to be able to audit the rule, know it will never be violated, and sleep at night. A gate is code: read it, verify it, trust it.

**Example gate:** "never exceed 2× notional leverage" → a one-line check in the executor, always enforced.

**Trade-off:** Less flexibility (you can't say "bend the leverage rule today"), but much more safety.

---

## Append-Only Journal (Immutable Proof Layer)

**Decision:** Every candidate, thesis, and fill is journaled append-only. No deletes, no rewrites, no updates.

**Why:** Autonomous trading will build reputation systems. A trader's journal is their proof. Agents that show verifiable decision history (reasoning, confidence, outcome) can command higher capital. The journal is not just audit; it's the foundation of reputation.

**Trade-off:** Storage cost (every decision is ~200 bytes) and no perfect undo, but you get an immutable proof of competence.

---

## Scoped Wallet Signing (Owned, Not SDK-Wrapped)

**Decision:** Hyperion owns its EIP-712 signing implementation (~300 lines). The agent trades with a scoped key that can place orders but cannot withdraw to other addresses.

**Why:** 
- **Ownership:** We control the signing; no hidden features, no SDK bugs that bypass gates
- **Scoping:** The key can trade (place/cancel) but not drain the account
- **Auditability:** The signing logic is readable and verifiable

**Trade-off:** More code to maintain; but no dependency on a third-party signing SDK that might have surprise behavior.

---

## LLM Reasoning, Not Statistical Models

**Decision:** Hyperion uses Claude/OpenAI/Deepseek (not proprietary ML models). Reasoning output is schema-validated; invalid responses are rejected.

**Why:**
- **Transparency:** you know what model is reasoning; you can change it
- **Flexibility:** the model can adapt to new regimes without code changes
- **Audibility:** you can read the market picture and reasoning to understand why an order was placed

**Trade-off:** Adds latency (LLM calls are slower than local inference) and cost (per-token billing), but you get understandable reasoning.

---

## Event Bus Architecture (Not Global Shared State)

**Decision:** The daemon is built on an event bus. All state mutations are explicit events; components subscribe and react.

**Why:**
- **Reasoning audit:** you can trace every decision back to the market events that prompted it
- **Replay:** you can replay the event stream to verify historical decisions
- **Parallelism:** components don't deadlock over mutable state

**Trade-off:** Slightly more complex threading model, but the tradeoff is worth it for auditability.

---

## TUI First (Not Web First)

**Decision:** The operator's interface is a terminal UI (Bubble Tea), not a web dashboard.

**Why:**
- **Latency:** a TUI is instant; web dashboards introduce browser lag
- **Control:** keyboard-driven, no mouse fumbling when you need to halt
- **Accessibility:** works over SSH, no dependencies on a web server
- **Separation:** TUI is the operator's domain; dashboard is for monitoring from afar

**Trade-off:** Less polished UI (text, not graphics), but snappier and more operator-friendly.

---

## MCP (Not Custom SDK)

**Decision:** Hyperion trades via the Model Context Protocol (MCP), not a custom Python/JS SDK.

**Why:**
- **Standard:** MCP is an open protocol; any LLM-compatible client can trade through Hyperion's gates
- **No SDK lock-in:** Claude, Cursor, local Ollama, etc. all work the same way
- **Simple:** MCP is just JSON over stdio; easy to audit and extend

**Trade-off:** MCP is newer and less battle-tested than mature SDKs, but it aligns with the agent-native thesis.

---

## Hyperliquid-First

**Decision:** Hyperion is built for Hyperliquid (on-chain perps) as the primary venue.

**Why:**
- **Liquidity & speed:** Hyperliquid is the dominant on-chain perp venue; billions/day in volume
- **Public API:** REST + WebSocket, no proprietary protocol
- **Signature-gated:** every order is signed by the agent's key; no centralized trust in login credentials

**Trade-off:** Smaller market than Binance/Deribit, but Hyperion's thesis is "agents that trade on-chain."

---

## Testnet as First-Class Mode

**Decision:** The daemon ships with a `-testnet` flag that forces propose-only mode (no live capital execution).

**Why:**
- **Safe onboarding:** new users can run the full loop without risking real money
- **Demo:** YC and investors can see the system work live without creating liability
- **Fidelity:** testnet is real Hyperliquid API; not a mock

**Trade-off:** Testnet funding is required (small stipends from Hyperliquid), but the safety is essential.

---

## Configuration File (Not Env-Only)

**Decision:** `config.toml` holds risk gates, reasoning cadence, and market subscriptions. Environment only for secrets.

**Why:**
- **Auditability:** gates are readable in version control (with secrets redacted)
- **Versioning:** you can see when and how gates changed
- **Deployment:** easier to compare configs across environments

**Trade-off:** More files to manage, but the traceability is important for compliance.

---

## Reasoning Cycle Cadence (Not Continuous)

**Decision:** Reasoning runs on a schedule (default: every 5 minutes), not on every market tick.

**Why:**
- **Cost:** fewer LLM calls means lower bills
- **Decision quality:** batching ticks into 5-min summaries reduces noise
- **Operator awareness:** less noise in the journal; cleaner decision history

**Trade-off:** Slower reaction to regime shifts, but the tradeoff favors quality over speed.

---

## React Dashboard (Optional Monitoring)

**Decision:** Dashboard is a React app, separate from the backend. Optional for traders who want web UX.

**Why:**
- **Choice:** TUI is the hardcore operator interface; dashboard is for monitoring from browser
- **Decoupling:** dashboard can be deployed anywhere; doesn't tie to backend infrastructure
- **Real-time:** connects via WebSocket; live event stream

**Trade-off:** Two UIs to maintain, but they serve different use cases.

---

## No Database (Append-Only Files)

**Decision:** The journal is a flat file (or SQLite for optional indexing), not a Postgres database.

**Why:**
- **Portability:** the journal is a single file; easy to backup, replay, audit
- **No external dependency:** no need to run a database server
- **Schema simplicity:** JSON lines are easy to parse in any language

**Trade-off:** No complex queries (you can't do SQL joins on the journal), but the tradeoff favors simplicity.

---

## Verifiable Signing (Not Trusted Intermediary)

**Decision:** Every order is signed by the agent's key. Hyperliquid verifies the signature; no intermediary.

**Why:**
- **Transparency:** the signature is proof of intent; no room for "the system said I ordered that but I didn't"
- **Custody:** agent key is scoped; account holder retains control
- **Auditability:** every fill has a sig; verifiable history

**Trade-off:** More complex signing logic (we own the EIP-712 impl), but the proof is solid.

---

## Operator Override, Always

**Decision:** The operator can halt, veto, or adjust the mandate at any time. No exceptions.

**Why:**
- **Trust:** humans retain control; the agent is an advisor, not a dictator
- **Safety:** emergency kill switch
- **Liability:** operator's fingerprints are on every decision they don't veto

**Trade-off:** Requires active operator involvement, but that's a feature for a trading system.

---

## Schema-Validated Reasoning Output

**Decision:** Every LLM response is validated against a strict schema (JSON). Malformed responses are rejected.

**Why:**
- **Reliability:** you know every accepted candidate is well-formed
- **Safety:** prevents the model from returning weird instructions that might confuse the executor
- **Auditability:** every candidate in the journal has consistent structure

**Trade-off:** Requires the model to follow format instructions; occasional rejections if the model is confused.

---

## Confidence Scoring (Not Probability)

**Decision:** The reasoning step asks the model for a confidence score (1–10) and a candor check (is this a real conviction or hedging?).

**Why:**
- **Transparency:** you see how sure the agent is
- **Filtering:** you can set a minimum confidence threshold before execution
- **Candor check:** catches hedging ("I don't know, so I'll do both"); real traders are honest about conviction

**Trade-off:** Adds a step to reasoning, but the output is much more interpretable.

---

## Funded Mandates (Not Leverage Alone)

**Decision:** Positions are sized by available collateral and leverage cap, not just leverage.

**Why:**
- **Safety:** no accidental over-leverage if collateral fluctuates
- **Mandate fidelity:** a mandate like "reach 60/40 split" assumes enough capital; if you run out, the mandate fails gracefully

**Trade-off:** Simpler model if you just capped leverage, but funding constraints are realistic.

---

## No Sub-Signing

**Decision:** The agent's key cannot sub-sign for other accounts or inherit signing authority.

**Why:**
- **Containment:** the agent is locked to one account
- **Audit:** no surprise lateral movement

**Trade-off:** Limits flexibility (you can't use the agent as a signing oracle for multiple accounts), but containment is a win.

---

## Funding Rate Awareness

**Decision:** The reasoning loop ingests funding rates and can propose positions that capture or hedge funding.

**Why:**
- **Realistic:** funding is often the largest P&L driver on perpetuals
- **Strategy:** an agent that ignores funding is leaving money on the table

**Trade-off:** More market data to ingest, but it's essential for perp strategies.

---

## Mandate Horizon (Not Live Liquidation Prices)

**Decision:** Risk is measured against the mandate horizon (e.g., 8% max drawdown over 90 days), not live liquidation prices.

**Why:**
- **Intent:** the mandate is the contract; the agent shouldn't panic-exit just because a flash crash touches the liquidation price
- **Composure:** the agent tolerates regime shifts within the mandate bounds

**Trade-off:** Requires trusting that liquidation won't happen during normal operation; a strong assumption if you use high leverage.

---

## No Partial Fills (Or Explicit Handling)

**Decision:** Orders default to all-or-nothing; partial fills are tracked and explicitly reported.

**Why:**
- **Clarity:** you know exactly what executed
- **Journal:** no ambiguity about position size

**Trade-off:** Some orders won't fill if the size is aggressive; operator can retry or adjust.

---

## No Margin Calls or Liquidation Automation

**Decision:** If liquidation approaches, the system alerts and halts; the operator must manually act.

**Why:**
- **Transparency:** liquidation is an operator decision, not automated
- **Safety:** prevents cascading liquidations

**Trade-off:** Requires operator vigilance, but that's appropriate for high-risk trading.

---

## Summary of Trade-Offs

| Goal | Chosen | Cost | Benefit |
|------|--------|------|---------|
| Risk | Compiled gates | Less flexible | Guaranteed enforcement |
| Transparency | Append-only journal | Storage cost | Verifiable history |
| Auditability | Event bus | Complex threading | Full decision trace |
| Safety | Operator override | Active involvement | Human control retained |
| Reasoning | LLM (Claude) | Latency + cost | Adaptable, readable |
| Signing | Owned impl | Maintenance | No SDK surprises |
| Interface | Mandate, not tickets | Harder UX | Aligned with intent |
| Verification | Schema validation | Rare rejections | Consistent output |

---

## Future Expansions

These decisions enable (but don't commit to):

- **Multi-venue routing:** add more exchanges; the mandate and risk gates scale
- **Agent fleets:** multiple agents on one account (with separate scoped keys); coordinated via mandate
- **Reputation markets:** journal → reputation score → automated capital allocation
- **Compliance layers:** journal → audit trail → regulatory reporting
