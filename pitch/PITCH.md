# Hyperion: an autonomous trading operator, built on Hyperliquid

**One-liner:** An autonomous trading operator, built on Hyperliquid.
Hyperion runs a trading desk that never sleeps: it ingests markets, reasons about them in writing, and executes toward the financial goals you state in plain language.

---

## Thesis

**Three things a trader runs out of: attention, presence, discipline. Judgment is never the one that runs out first.**

### Attention

On-chain markets solved access. Anyone, anywhere, can hold a position on a venue that never closes, with custody in their own hands.

What they didn't solve is attention. Someone still has to read the order book, funding, news, and flow at three in the morning, and still has to be at the screen when a limit order needs replacing before the market moves past it.

And the surface keeps widening. Complexity compounds; the hours in a day don't. A single post can reprice a position before you've finished reading it, true or not, because the market takes it at face value first and verifies later.

### Presence

A position is not a decision made once. The narrative moving a market on Thursday is rarely the one that opened the trade on Monday, and an exit price fixed at entry stops describing the risk actually being carried. Holding a position properly means managing and hedging it against what the market believes today, which is work that never pauses.

The value an agent adds is capacity, not intelligence. It ingests order book state, news, and liquidity flows around the clock, and holds all of it against a **mandate**: a goal, a horizon, and risk limits a trader already holds, not a bot's fixed strategy.

It's the medium through which a trader expresses a thesis. You say what you want to hold, over what period, at what risk, and the agent does the watching.

### Discipline

Theses fail less often than the people holding them. Conviction is cheap at entry and expensive halfway through a drawdown, and most positions are closed by discomfort rather than by evidence. An operator carrying the mandate has nothing to feel: it sizes, waits, hedges, and executes the trade you said you wanted. When the read is that nothing should be done, it does nothing.

This is why Hyperion automates, and never delegates. An agent asked to form and hold its own view has no persistent stake in it: every prompt re-derives a probability distribution over whatever narrative is loudest in its context, which is why these systems converge to the median and reverse the moment a headline shifts that distribution. That isn't autonomy, it's panic wearing better prose. Automation was never asked to have a view. The thesis gets fixed once, at the point of maximum clarity, when you set it, and the system's only job is holding that line against noise — a tractable engineering problem, not the unsolved one of machine conviction.

---

State the outcome, automate the mechanics — never the thesis. The onboarding surface of trading is becoming language, because the execution surface no longer needs a person babysitting it.

Travel platforms ship agents that search, book, and pay for a trip in one pass, no human touching the transaction. Software holds a crypto wallet and moves funds without passing KYC at a bank, so agents show up as counterparties on-chain.

Markets are the sharpest edge of that shift: liquid, quantifiable, adversarial, and open around the clock. Trading is the leading edge of the agentic economy, not a side case of it. Hyperion sits at that edge: an autonomous trading operator between a trader's mandate and the venue.

## Product: one loop, running continuously

1. **Ingest.** A constant read of order books, funding, open interest, positions, and flow on Hyperliquid, normalized into one live picture of the market.
2. **Reason.** Judgment in writing. The agent weighs the picture against your mandate — never against a thesis of its own — and reasons every decision in writing before acting on it.
3. **Execute.** Direct to the venue. The system sizes, stages, and places orders on Hyperliquid through hard-coded risk gates, and results feed back into the picture as the loop continues.

The loop is inspectable end to end. You can read every decision the agent has made, and you can stop it at any time.

**The interface is a mandate, not an order ticket.** *"Reach a 60/40 ETH–stablecoin split over 90 days. Keep drawdown under 8%. Leverage capped at 2×."* The agent works that mandate: tranching entries, reading funding regimes, staging limit orders instead of taking wide spreads, and writing down why at every step.

## What's built

The full loop exists and runs as a single Go binary, built as my own working prototype:

- **Live ingest & aggregation** across 10–30 Hyperliquid markets: multi-timeframe bars with perp-native metrics (CVD, basis, funding trajectory, OI delta, liquidation proximity, cross-asset correlation).
- **Model-agnostic reasoning**: timeframe-batched digests in, schema-validated trade candidates with written theses out. Never free text.
- **Deterministic execution layer.** Every order passes compiled risk gates: max position, max exposure, max concurrency, price sanity vs. live mark, post-stop cooldown, daily-loss kill-switch. No model output bypasses them.
- **Owned signing.** The master key signs exactly one `approveAgent` transaction. The daemon holds only a scoped agent wallet that can trade but **cannot withdraw**. The EIP-712 signing module is ~300 lines I own, verified byte-exact against Hyperliquid's reference vectors, with no SDK dependency.
- **Append-only journal.** Every candidate, thesis, and fill in one place: audit trail, backtest corpus, and the agent's memory.
- **MCP server:** any agent speaking MCP can read markets and place orders through the same gates. Every client shares one path to the wire.
- **Terminal UI.** The operator's cockpit for the personal-tool deployment.

This prototype is the proving ground, not the product. It de-risks the hard parts (signing, gating, continuous reasoning) and generates the journal evidence the product story rests on.

## The product this raise builds

The **hosted trading operator**: a web application where a user states a mandate in plain language and reads the agent's work (decision log, position, risk against mandate, progress), with one-click scoped-wallet onboarding and the ability to halt at any time. The backend core exists. The raise productizes it.

## Why now

Two curves, one crossing.

- **The venue.** Hyperliquid put a full perpetuals exchange behind a public, signature-gated API and became the dominant on-chain venue while doing it. There is no broker to onboard with and no key custodian to trust: an order needs a signature, not a relationship.
- **The models.** Tool-calling models became reliable enough to carry a position's context across days instead of turns, and MCP standardized how they reach anything outside themselves. Both landed inside the same eighteen months.

The curves cross exactly at "agents that trade." What's missing at the crossing was never intelligence. It's execution worth handing a key to, and an interface pitched at a mandate instead of an order ticket.

## Why this isn't already solved

Agentic trading went mainstream this cycle. Several brokerages and exchanges now ship their own version of "an agent trades for you" — usually pitched as the agent forming its own thesis, not carrying yours. That's the harder, unsolved problem (machine conviction) sold as the easier one (execution): an LLM asked to hold a view has no persistent stake in it, so it re-derives a probability distribution over the loudest narrative on every prompt and reverses the moment a headline shifts it. Automation doesn't have that problem, because it was never asked to have a view.

That approach also ties the agent to the platform: to use it, a trader has to trust the exchange underneath — its custody, its listing desk, its order book — not just the agent sitting on top of it.

Hyperion takes a different path. It expresses and executes through Hyperliquid, a venue that's already proven itself at scale: $4.4T cleared, no broker, no listing desk, no key custodian to win over. The trust question is already settled there.

Past Hyperliquid, the system is infra-agnostic on purpose. The mandate, the reasoning loop, and the risk gates don't care which venue sits on the other end of the signed order. Hyperliquid is where the thesis gets expressed first — not the only place it's allowed to be.

## Moat

There isn't one in the idea itself. Enough traders have started experimenting with autonomous agents that the concept alone won't hold up as a moat.

What's defensible is narrower: I sell execution of a trader's judgment, not the judgment itself — automation, not delegation. That's a smaller, more concrete claim than a recommendation engine — and the scoped signing, the hard risk gates, and a track record somebody else can verify are things that get built and proven, not just described. Whoever proves it first, with real capital and a public journal, holds the position.

It's also a lighter regulatory bucket than a service that generates recommendations: no custody, no advice, just an operator executing a mandate the trader already holds.

## Market

**The venue is already there.** No forecast is needed to size this one. The exchange, the liquidity, and the trader all exist today, in public, at volume. What doesn't exist yet is anything sitting between them that can hold a mandate.

**$4.4T** cleared in cumulative perpetuals volume on Hyperliquid to date, running at a multi-hundred-billion-dollar monthly clip (DefiLlama).

- **The venue:** a full exchange behind a public, signature-gated API. No broker, no listing desk, no API-key custodian. A signature is the only thing between an order and the book.
- **The trader:** on-chain traders who want representation, not another terminal. People who already hold a view, and are tired of being the one who has to sit with it at three in the morning. Starting with Hyperliquid's prosumer base.
- **The model:** subscription for the hosted operator, basis points on the flow it executes autonomously, and licences for funds running fleets of agents.
- **The layer:** every agent that trades needs the same thing underneath: scoped signing, a per-mandate risk envelope, and a track record somebody else can verify. That layer is the durable position.

*Volume figures as reported by DefiLlama. There are no projections in this document.*

## The ask

I'm pre-launch, raising a pre-seed to (1) ship the hosted trading operator, (2) harden the scoped-signing service, (3) run supervised live capital to build the public journal that proves the loop.

**ncerratoanton@gmail.com**
