# Financial Plan & Unit Economics

Hyperion's revenue model and path to profitability.

---

## Revenue Streams

### 1. Subscription (SaaS)

**Model:** Monthly recurring revenue (MRR) per active agent.

| Tier | Monthly | Annual | Allocation | Target User |
|------|---------|--------|-----------|-------------|
| Starter | $99 | $1,188 | $10k max | Retail traders, students |
| Professional | $299 | $3,588 | $100k max | Semi-pro, small funds |
| Enterprise | $999 | $11,988 | $1M+ max | Trading firms, funds |

**Pricing logic:**
- Lower tier = lower allocation limit (less capital at risk) = lower pricing
- Higher tier = higher support + API priority
- Volume discounts at 10+ seats per organization

### 2. Flow Fee (Performance)

**Model:** Basis points on autonomously executed notional volume.

| Tier | Percentage | Example |
|------|-----------|---------|
| Starter | 20 bps | $100k executed → $200 fee |
| Professional | 10 bps | $1M executed → $1,000 fee |
| Enterprise | 5 bps | $10M executed → $5,000 fee |

**Monthly revenue:** (total notional executed) × (bps rate) ÷ 10,000

Example: 50 professional users with $50M/month execution each = $25M notional = $25,000 MRR.

### 3. Enterprise Licenses

**Model:** Custom pricing for trading firms, funds, or crypto platforms.

**Example deals:**
- $20k/month for a fund's agent fleet (10 agents, custom risk gates)
- $5k/month for a crypto exchange (embed Hyperion as a user feature)

**Typical terms:** 1–3 year contracts, volume discounts, dedicated support.

---

## Unit Economics

### Customer Acquisition Cost (CAC)

**Assumption:** organic growth + word-of-mouth + events + content marketing.

| Channel | Spend/Year | Expected Users/Year | CAC |
|---------|-----------|-----------------|-----|
| Content (blog, docs) | $5k | 50 | $100 |
| Events + speaking | $10k | 30 | $333 |
| Paid ads (Twitter/Reddit) | $10k | 20 | $500 |
| Partnerships (exchanges) | $0 | 50 | $0 |
| **Total** | **$25k** | **150** | **$167** |

### Lifetime Value (LTV)

**Subscription stream:** assume 3-year average customer lifetime, 10% annual churn.

```
Customer pays: $299/month × 12 = $3,588/year
Lifetime: $3,588 × 3 years = $10,764
```

**Flow fee stream:** assume same customer, $5M/month execution, 10 bps rate.

```
Flow fee: $5M × 0.10% = $5,000/month
Annual: $60,000
Lifetime: $60,000 × 3 years = $180,000
```

**Total LTV:** $10,764 + $180,000 = ~$190,764 per professional customer.

**LTV:CAC ratio:** $190,764 ÷ $333 = **573:1** ← excellent (target >3:1).

### Gross Margin

**Cost of goods sold (COGS):**
- API calls to Claude: $3 per user per month (reasoning)
- Hyperliquid API: $0 (free for agents)
- Hosting (backend + dashboard): $50/month (fixed, scales to ~500 users)
- Storage (journal): $0.10 per user per month (cheap)

**Total COGS per user:** ~$3.10/month + proportional hosting.

**Gross margin on subscription:**
```
Revenue: $299/month
COGS: $3.10/month
Gross profit: $295.90/month
Margin: 98.9%
```

**Gross margin on flow fee:**
```
Flow fee: $5,000/month (10 bps on $5M)
COGS: $0 (Claude cost already paid by subscription)
Gross profit: $5,000/month
Margin: 100%
```

**Blended gross margin:** ~95% (conservative, assumes higher COGS scaling).

---

## Financial Projections (5-Year)

**Assumptions:**
- Year 1: 50 active customers (mostly Starter, some Professional)
- Growth: 200% YoY (typical for early-stage SaaS)
- CAC: $200 (paid ads scale)
- LTV:CAC ratio: 500:1 (sustainable)
- Churn: 10% annually (professional users, sticky product)

### Year 1 (Launch)

| Metric | Value |
|--------|-------|
| Active customers | 50 |
| Avg subscription tier | Professional ($299) |
| Subscription MRR | $14,950 |
| Flow fee MRR (assumption: $2M/user/month) | $10,000 |
| **Total MRR (end of year)** | **$24,950** |
| **Annual recurring revenue (ARR)** | **$299,400** |
| COGS | $50k |
| R&D + Ops | $200k |
| Sales + Marketing | $50k |
| **Operating loss** | **-$500k** |

### Year 2

| Metric | Value |
|--------|-------|
| Active customers | 150 |
| Avg customer flow | $3M/month |
| Subscription MRR | $44,850 |
| Flow fee MRR | $45,000 |
| **Total MRR (end of year)** | **$89,850** |
| **ARR** | **$1,078,200** |
| COGS | $100k |
| R&D + Ops | $300k |
| Sales + Marketing | $150k |
| **Operating loss** | **-$172.8k** |

### Year 3 (Breakeven)

| Metric | Value |
|--------|-------|
| Active customers | 450 |
| Subscription MRR | $134,550 |
| Flow fee MRR | $225,000 |
| **Total MRR** | **$359,550** |
| **ARR** | **$4,314,600** |
| COGS | $150k |
| R&D + Ops | $500k |
| Sales + Marketing | $300k |
| **Operating profit** | **~$2.86M** |

### Year 4–5

Scaling: 1000+ customers, $10M+ ARR, operating margin 60%+.

---

## Addressable Market (SAM)

### Hyperliquid Prosumers (Near-Term TAM)

**Population:** ~50,000 active traders on Hyperliquid.

**Addressable:** 20% willing to try an agent = 10,000 traders.

**Revenue potential:**
- 1,000 paying customers (10% conversion)
- Average $500/month (subscription + flow fees blended)
- = **$6M ARR**

### Broader On-Chain Perps (5-Year TAM)

**Population:** All perpetual traders on on-chain exchanges (Hyperliquid, dYdX, etc.).

**Estimated:** 200,000+ traders across venues.

**Addressable:** 10% adopt agent-based trading = 20,000 customers.

**Revenue potential:**
- 5,000 paying customers
- Average $1,000/month (larger accounts, more enterprise)
- = **$60M ARR**

### Enterprise Agents (10-Year TAM)

**Population:** Cryptocurrency hedge funds, proprietary trading firms, market makers.

**Estimated:** 500+ firms globally.

**Addressable:** 50 firms integrate Hyperion for agent fleets.

**Revenue potential:**
- 50 enterprise contracts
- Average $50k/month
- = **$30M ARR**

**Total SAM (conservative):** $10–$100M by 2030.

---

## Path to Profitability

### Milestones

| Milestone | Timeline | Status |
|-----------|----------|--------|
| Product-market fit (50 active users) | Jul–Sep 2026 | In progress |
| First $10k MRR | Oct 2026 | Target |
| First paying customers | Aug 2026 | Target |
| Breakeven (operating) | Q2 2027 | Projected |
| $1M ARR | Q1 2027 | Projected |

### Unit Economics Sensitivity

**If CAC = $500** (more aggressive marketing):
- LTV:CAC still 381:1 ← profitable at scale

**If flow fees drop to 5 bps** (competitive pressure):
- LTV still $100k+ ← healthy margin

**If churn rises to 20% annually** (product risk):
- LTV drops to ~$95k, LTV:CAC still ~190:1 ← still works

---

## Use of Funds ($500k Seed)

Requested: $500k (via YC SAFE).

Allocation:

| Item | Amount | Duration |
|------|--------|----------|
| Salaries (2 founders × 6 months) | $200k | 6 months |
| Infrastructure + ops (servers, APIs, services) | $50k | 12 months |
| Sales + marketing (content, ads, events) | $100k | 12 months |
| Buffer (tax, legal, contingency) | $50k | Ongoing |
| **Total** | **$400k** | **12 months** |

**Runway:** 12 months to reach $10k MRR (breakeven on burn).

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Market adoption slow (TAM smaller than expected) | High | Early user feedback; pivot to enterprise if needed |
| Regulatory crackdown on on-chain trading | High | Legal review; diversify to multiple venues |
| Competition from established bots (3Commas, etc.) | Medium | Owned signing + journal moat; focus on agents, not bots |
| Claude API price hikes | Medium | Multi-model support (fallback to OpenAI, Deepseek) |
| Key person risk (founders leave) | Medium | Build strong engineering culture; document everything |
| Security breach or signing bug | High | Intensive security audit; insurance; incident response plan |

---

## Key Metrics to Track

### Growth Metrics

- Active users (end of month)
- New user signups
- Churn rate (users lost per month)

### Revenue Metrics

- MRR (subscription)
- Flow fee MRR (execution volume × bps)
- ARPU (average revenue per user)
- LTV:CAC ratio

### Product Metrics

- Median mandate lifetime (days until mandate is closed)
- Avg orders per user per month
- Win rate (% of executed orders that are profitable)
- Median user confidence in reasoning (from journal)

### Operational Metrics

- CAC payback period (months)
- Gross margin
- Burn rate
- Runway (months until cash-out)

---

## Investor FAQs

### Will Hyperion be profitable?

Yes. Unit economics are strong (LTV:CAC = 500:1), and gross margins are ~95%. Breakeven is achievable in 12–18 months with disciplined customer acquisition.

### What happens if Hyperliquid shuts down or bans bots?

Hyperion can integrate other on-chain perp venues (dYdX, etc.). The architecture supports multiple exchanges; Hyperliquid is the initial wedge, not the entire market.

### Can an LLM really trade profitably?

In this model, yes — because:
1. The LLM is not expected to beat the market
2. It's expected to execute on a **mandate** (a goal, not a return target)
3. Risk gates prevent catastrophic losses
4. The journal provides proof and feedback for improvement

### What's the competition?

- **Crypto trading bots** (3Commas, TradingView): static strategies, not reasoning
- **Crypto copilots** (ChatGPT plugins, etc.): advice only, no execution
- **Traditional prop trading firms**: humans + HFT, not agents

Hyperion is unique in combining reasoning + execution + verifiable proof.

### Can you scale to $100M ARR?

Yes, but margins compress at scale:
- Reasoning cost drops (models get cheaper)
- Infrastructure scales, but support costs rise
- Enterprise deals introduce custom development

Path to $100M ARR requires 10,000+ users or 50+ enterprise deals. Achievable in 5–7 years.

---

## Summary

| Metric | Value |
|--------|-------|
| **Gross Margin** | ~95% |
| **LTV:CAC** | 500:1 |
| **Breakeven** | 12–18 months |
| **Year 3 ARR** | $4–5M |
| **TAM (5 years)** | $10–100M |
| **Risk Level** | Medium (adoption + regulation) |

Hyperion is a venture-scale opportunity with strong unit economics and a clear path to profitability.

---

*Prepared: July 2026*  
*Reviewed by: [Founders]*
