# PROTOCOL — strategy wire contract (core ↔ terminal ↔ web)

All JSON. Timestamps RFC3339 UTC. Money in USD floats. IDs are opaque strings
(ULIDs for records, snake_case slugs for strategies/venues). Unknown fields must
be ignored by clients. This file is the single source of truth: Go types in
`backend/internal/strategy`, zod schemas in `hypertrade/src/shared/strategy-protocol.ts`,
and `tui/internal/apiclient` types mirror it byte for byte.

## Jev primitives (as sent to and received from TypeSafe)

```jsonc
// Question
{ "type": "choice", "instructions": "Which regime is this market in", "criteria": { "risk_on": "...", "risk_off": "...", "chop": "..." } }
{ "type": "score",  "instructions": "How crowded is the long side", "criteria": ["not crowded", "somewhat", "extremely"] }
{ "type": "noul",   "instructions": "Funding is extreme relative to the 30-day range" }

// Answer
{ "type": "choice", "choice": "risk_off", "probabilities": { "risk_on": 0.1, "risk_off": 0.82, "chop": 0.08 }, "confidence": 0.71 }
{ "type": "score",  "score": 1.35, "probabilities": { "0": 0.05, "1": 0.55, "2": 0.4 }, "legend": { "0": "not crowded", "1": "somewhat", "2": "extremely" }, "confidence": 0.6 }
{ "type": "noul",   "noul": 0.93 }
```

`confidence` is absent on noul answers. `legend` values may be strings or
objects; clients render `what` when it is an object.

## Core types

```jsonc
// ParamSpec
{ "key": "size_usd", "type": "number", "label": "Size per intent (USD)", "default": 250, "min": 10, "max": 100000, "step": 10, "description": "..." }
{ "key": "side_bias", "type": "enum", "options": ["both", "long_only", "short_only"], "default": "both" }
// type ∈ "number" | "string" | "bool" | "enum"

// Manifest (static, from the plugin)
{
  "id": "funding_skew", "name": "Funding skew fade", "version": "0.1.0",
  "description": "...", "venues": ["hyperliquid", "paper"], "cadence": "5m",
  "markets": ["BTC", "ETH"],                 // default universe, overridable in config.params.markets
  "params": [ ParamSpec, ... ],
  "questions": { "funding_extreme": Question, "direction": Question, "crowding": Question }
}

// GovernorSettings (global) / GovernorOverride (per strategy: same shape, all fields optional)
{ "mode": "manual", "min_confidence": 0.75, "max_notional_usd": 1000, "max_open_intents": 5, "killed": false }
// mode ∈ "manual" | "threshold" | "auto"

// StrategyConfig (operator-editable)
{ "id": "funding_skew", "enabled": false, "venue": "paper", "params": { "size_usd": 250, "min_p": 0.8 }, "governor": { "min_confidence": 0.75 } }

// StrategyStatus = Manifest + Config + runtime
{ "manifest": Manifest, "config": StrategyConfig, "last_run_at": "…", "last_decision_id": "…", "last_action": "open_short ETH", "last_error": "", "next_run_at": "…" }
// last_action: short human summary of the newest intent ("hold" when none), optional

// Intent
{
  "id": "01J…", "strategy_id": "funding_skew", "venue": "paper", "market": "ETH",
  "action": "open_short",     // open_long | open_short | close | scale | rebalance | hold
  "size_usd": 250, "target_weight": null, "price_limit": null,
  "reason": "funding_extreme=0.93 direction=fade_short crowding=1.35",
  "confidence": 0.71
}

// Verdict (one per intent, appended as status changes)
{ "intent_id": "01J…", "status": "proposed", "by": "governor", "reason": "mode=manual", "ts": "…" }
// status ∈ proposed | approved | rejected | executed | gated | failed ; by ∈ governor | operator | gate | venue

// DecisionRecord (one per strategy tick)
{
  "id": "01J…", "ts": "…", "strategy_id": "funding_skew", "venue": "paper", "dry_run": false,
  "state_digest": "sha256:…", "state": { … },           // state as sent to Jev (compact)
  "questions": { "…": Question }, "answers": { "…": Answer },
  "intents": [ Intent ], "verdicts": [ Verdict ],
  "model": "jev-1.13.0", "usage": { "input_tokens": 412, "output_tokens": 30 }, "latency_ms": 180,
  "error": ""                                            // non-empty when the decider or venue failed; intents empty
}

// VenueStatus
{ "id": "paper", "kind": "paper", "chain": "none", "status": "connected", "capabilities": ["perps"], "positions": [ { "market": "ETH", "size_usd": -250, "entry": 3100.5, "mark": 3080.2, "upnl_usd": 1.6 } ] }
```

## HTTP (core, bearer token as the existing API)

```
GET    /api/strategy/manifests                      → { "manifests": [Manifest] }
GET    /api/strategy/configs                        → { "strategies": [StrategyStatus] }
GET    /api/strategy/configs/{id}                   → StrategyStatus
PUT    /api/strategy/configs/{id}                   ← StrategyConfig  → StrategyStatus   (validates params against manifest; 400 with {error, field})
POST   /api/strategy/configs/{id}/run               ← { "dry_run": true }  → DecisionRecord (sync; dry_run never touches a venue or the governor)
GET    /api/strategy/decisions?limit=50&strategy=id → { "decisions": [DecisionRecord] }   (newest first)
GET    /api/strategy/decisions/{id}                 → DecisionRecord
POST   /api/strategy/decisions/{id}/intents/{iid}/approve → DecisionRecord
POST   /api/strategy/decisions/{id}/intents/{iid}/reject  → DecisionRecord
GET    /api/strategy/venues                         → { "venues": [VenueStatus] }
GET    /api/strategy/governor                       → GovernorSettings
PUT    /api/strategy/governor                       ← GovernorSettings → GovernorSettings
POST   /api/strategy/kill                           → GovernorSettings (killed=true, all configs enabled=false, open proposals rejected)
// Un-kill: PUT /api/strategy/governor with "killed": false. Strategies stay disabled until re-enabled one by one.
```

Errors: `{ "error": "message", "field": "params.size_usd" }` with 400/404/409/503.
503 `{ "error": "decider unavailable" }` when the Jev key is missing and kind=jev.

## WebSocket (existing `/api/ws`, new event types)

```jsonc
{ "type": "strategy.decision", "data": DecisionRecord }
{ "type": "strategy.verdict",  "data": { "decision_id": "…", "verdict": Verdict } }
{ "type": "strategy.config",   "data": StrategyStatus }
{ "type": "strategy.governor", "data": GovernorSettings }
{ "type": "strategy.venue",    "data": VenueStatus }
```

## hypertrade proxy

`/api/engine/<rest>` ⇄ `${ENGINE_URL}/api/strategy/<rest>`, same methods and
bodies, session-cookie protected on the hypertrade side. 503
`{ "error": "engine not configured" }` when `ENGINE_URL` is unset.
