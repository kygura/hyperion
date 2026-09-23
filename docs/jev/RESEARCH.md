# RESEARCH — Jev integration facts and venue survey for the strategy runtime

Status: research note, 2026-09-23. Companion to [SPEC.md](./SPEC.md) and [PROTOCOL.md](./PROTOCOL.md).
Sources are linked inline. Items marked **unverified** could not be confirmed from a primary source in this session (typesafe.ai is egress-blocked for direct fetch; docs were read through Context7, Exa and Firecrawl mirrors).

---

## Part A — Jev integration facts

### A1. HTTP API

| Item | Fact | Source |
|---|---|---|
| Endpoint | `POST https://api.typesafe.ai/v1/systemone`, `Content-Type: application/json` | [API reference](https://docs.typesafe.ai/api) |
| Auth | `Authorization: Bearer <API_KEY>` (env `TYPESAFE_API_KEY` in SDKs) | [API reference](https://docs.typesafe.ai/api) |
| Request | `{ "state": string\|object\|array, "model": string, "questions": { key: Question } }`; all three required | [API reference](https://docs.typesafe.ai/api) |
| Response | `{ "model": "jev-1.13.0", "answers": { key: Answer }, "usage": { "input_tokens", "output_tokens" } }`. `model` echoes the versioned ID even when an alias was sent | [API reference](https://docs.typesafe.ai/api), [Models](https://docs.typesafe.ai/models) |
| Question keys | Chosen by the caller; **not sent to the model, not used in inference**; answers come back under the same key | [API reference](https://docs.typesafe.ai/api), [Choice](https://docs.typesafe.ai/primitives/choice) |
| Error codes | `401` missing/invalid key; `422` body failed validation ("the body details the offending field"); `429` rate limit; `529` overloaded. The API page lists only these four. The SDKs additionally model 400 (`BadRequestError`), 403, 404, 408 and 5xx (`InternalServerError`), so treat those as possible | [API reference](https://docs.typesafe.ai/api), [JS SDK errors](https://docs.typesafe.ai/sdk/javascript/api), [Python retries](https://docs.typesafe.ai/sdk/python/api/retries) |
| Error body | "JSON body describing what went wrong". The only concrete example is from Vercel's TypeSafe-compatible endpoint, which states it uses TypeSafe's shape: `{ "message": "...", "error_type": "invalid_request" }`. **Unverified** that TypeSafe's own responses always carry `error_type`; parse `message` defensively and keep the raw body | [API reference](https://docs.typesafe.ai/api), [Vercel TypeSafe API](https://vercel.com/docs/ai-gateway/sdks-and-apis/typesafe) |
| Request ID | Response header `x-typesafe-request-id` (log it on every DecisionRecord error) | [Python exceptions](https://docs.typesafe.ai/sdk/python/api/exceptions) |
| Retry guidance | On 429/529 back off exponentially; honor `retry-after` / `retry-after-ms`. SDK defaults: 2 retries, 0.5 s initial, 5 s max, 25 % jitter, retry on `{408, 429, 5xx}` plus connection/timeout errors, 30 s timeout | [API reference](https://docs.typesafe.ai/api), [Python retries](https://docs.typesafe.ai/sdk/python/api/retries) |
| Rate limits | 250,000 tokens/s and 1,200 requests/min per account; "adjusting dynamically" and may change without notice; higher on enterprise plans | [Models](https://docs.typesafe.ai/models) |
| Context | **64k tokens per request** (state + all questions); **32k for state + the single longest question**. ~32k tokens ≈ 150,000 characters of English | [Models](https://docs.typesafe.ai/models), [Primitives](https://docs.typesafe.ai/primitives) |
| State tokenization | JSON objects/arrays are accepted natively (SDKs pass the dict/list directly, not a string). No documented rule for how JSON structure is tokenized; assume keys, punctuation and numbers all cost tokens. **Unverified**: exact tokenizer; measure `usage.input_tokens` on fixture states | [State](https://docs.typesafe.ai/concepts/state) |
| Questions per request | No fixed cap; limited only by the shared token budget. Questions are evaluated independently and in parallel; extra questions "barely change" latency but cost input tokens | [Primitives](https://docs.typesafe.ai/primitives), [Fan-out](https://docs.typesafe.ai/patterns/fan-out) |
| Choice options | Max **255** per Choice; `criteria` values may be `null` when no description is needed | [API reference](https://docs.typesafe.ai/api) |
| Score levels | **2 to 10** ordered levels | [API reference](https://docs.typesafe.ai/api) |
| Models / aliases | `jev-1.13.0` is the only current model. `jev-latest` → `jev-1.13.0`; `jev-preview` → `jev-1.13.0` (no preview build now). Versioned IDs are accepted whether or not `GET /v1/models` lists them. Docs explicitly say: if you tuned thresholds against a version, pin it | [Models](https://docs.typesafe.ai/models) |
| Pricing | $0.042 per million input tokens; output tokens free | [Models](https://docs.typesafe.ai/models) |
| Latency | "Most queries complete in about 100 ms"; marketing range 70–500 ms. Independent measurement (LiteLLM router benchmark): median 127 ms, p95 231 ms | [How to build](https://docs.typesafe.ai/concepts/how-to-build-with-system-one), [LiteLLM benchmark](https://docs.litellm.ai/blog/jev-auto-router-benchmark) |
| Data retention | Not trained on customer requests. ZDR is an **enterprise contract feature** (contact privacy@typesafe.ai); no per-request ZDR header or flag is documented | [Models](https://docs.typesafe.ai/models), [Legal](https://docs.typesafe.ai/legal) |
| Input | Text only (string, object, array of text). English is the primary training language | [Models](https://docs.typesafe.ai/models) |

Answer shapes match PROTOCOL.md exactly. One note for the Go client: the Score `legend` value is either a string or the same object you passed as a level (`{"what": ..., "examples": [...]}`), so the type must be `json.RawMessage` or a string/object union ([Score](https://docs.typesafe.ai/primitives/score)). `instructions` and every `criteria` entry accept string, object, array or null ([Advanced: structure](https://docs.typesafe.ai/primitives/advanced)).

### A2. Semantics and design guidance

**Confidence.** For Choice and Score, `confidence` ∈ [0,1] is a statistic of the spread of `probabilities`: 1.0 when all mass is on one option/level, lower as it flattens. The docs' interactive demo approximates it for three options as `(3·max_p − 1)/2`, but the exact formula is not published and may change; the full `probabilities` are returned precisely so you can compute your own measure ([Confidence](https://docs.typesafe.ai/confidence)). Noul carries no `confidence` because the yes/no distribution is fully described by the single `noul` value ([Noul](https://docs.typesafe.ai/primitives/noul)).

**Thresholds.** Docs recommend a three-band pattern (act / confirm / do not act) with bands scaled to risk; the worked examples use `< 0.5` as an "unsure" floor, `> 0.9` for destructive actions, and a `0.6–0.85` confirm band in the routing pattern. `noul >= 0.8` is used in the Vercel changelog example. All are illustrative: "start with conservative thresholds, test with your own data". The self-consistency cookbook requires top probability ≥ 0.60 before auto-acting and shows agreement rising from 90.8 % to 99.2 % with that gate ([Confidence](https://docs.typesafe.ai/confidence), [Confidence-gated routing](https://docs.typesafe.ai/patterns/confidence-routing), [Consistency: choices](https://docs.typesafe.ai/cookbooks/consistency_choice_cookbook)). Third-party guidance: hand-label a few hundred states, plot confidence against agreement, and set the auto threshold from that curve ([Simon Willison](https://simonwillison.net/2026/Sep/21/jev/)). Do not carry a threshold tuned on a Noul over to a Choice, and do not expect `P(noul)` and `1 − P(not noul)` to sum to 1 ([Jaggedness](https://docs.typesafe.ai/model-jaggedness/jev-1.13)).

**Explicit "other".** Add an `other` / `none_of_the_above` option whenever the option list might not cover every input; a Choice is *relative* (it must pick something), a Noul is *absolute* and can be low for all options. Use a Choice to pick and a Noul to decide whether to act at all ([Choice](https://docs.typesafe.ai/primitives/choice), [Jaggedness](https://docs.typesafe.ai/model-jaggedness/jev-1.13)).

**Criteria richness.** Option and level descriptions may be objects such as `{"what": "...", "does_not_cover": "...", "examples": [...]}`; the docs say this sharpens boundaries between options. Structured `instructions` (`{"question": "...", "reference": {...}}`) let a question point at data by name using backticked paths like `` `markets.ETH.funding_z` `` ([Advanced: structure](https://docs.typesafe.ai/primitives/advanced), [How to build](https://docs.typesafe.ai/concepts/how-to-build-with-system-one)).

**Numeric and market data.** No official cookbook on numeric or time-series input exists (the cookbook index lists consistency, date extraction, RAG, hierarchical classification, SDE cascade; nothing on markets). The jaggedness page is explicit: Jev "struggles with tasks that require numeric precision", is "not a calculator", "will perform better on semantic representations than numeric", and Score expectations have "weak numerical calibration" so do not interpolate a magnitude between levels ([Jaggedness](https://docs.typesafe.ai/model-jaggedness/jev-1.13)). Practical rules for `State`:

1. Compute everything numeric in Go: z-scores, percentiles, returns, OI deltas. Send both the number *and* a named bucket (`"funding_z": 2.7, "funding_bucket": "extreme_high"`); the bucket is what the model reads well.
2. Prefer short qualitative labels over raw floats where a label exists (`"trend_1d": "strong_up"`, `"vol_regime": "elevated"`), and keep 2–3 significant digits when a number must be sent.
3. Do not send raw order books, candle arrays, or timestamps for comparison; compare dates and windows in code (failure modes 2, 3, 5).
4. Filter to the fields each question needs; irrelevant detail measurably reduces accuracy ("context rot").
5. Reference fields by backticked path in `instructions` to remove indirection.
6. Community trading projects follow the same pattern: Jev gets compact labelled state and returns a typed judgement; thresholds, risk vetoes and sizing stay in code ([finance survey gist](https://gist.github.com/drillan/6916b16e8ea31a8ec36c8f59d6483150), [jev-trade on Hyperliquid](https://github.com/Waxmell114514/jev-trade), [50 financial use cases](https://github.com/IslamBaraka90/jev-typesafe-real-financial-use-cases)). None of these publishes a calibration study on market state; treat that as an open question (Part C).

**Known failure modes (jev-1.13, reviewed 2026-09-17):** literal reading; math/counting; date comparison; multi-hop indirection; large irrelevant state; adversarial content in state (no prompt-injection hardening by default); contradictory instructions vs criteria; no guaranteed structural invariants across separate questions; no generation ([Jaggedness](https://docs.typesafe.ai/model-jaggedness/jev-1.13)). Self-consistency is high but not perfect: in the cookbook, 2 of 8 Choice labels flipped across 15 repeats on a borderline input; adding a throwaway `uid` field to state is their way of defeating caching when measuring this ([Consistency: choices](https://docs.typesafe.ai/cookbooks/consistency_choice_cookbook)).

### A3. Alternative transports

The SPEC claim "a `base_url` + model change, not new code" is **true for three of four** gateways and **false for two others**:

| Transport | Endpoint | Auth | Model ID | Request/response deltas |
|---|---|---|---|---|
| TypeSafe direct | `POST https://api.typesafe.ai/v1/systemone` | `Bearer` TypeSafe key | `jev-latest` / `jev-1.13.0` | baseline |
| Vercel AI Gateway (TypeSafe-compatible) | `POST https://ai-gateway.vercel.sh/typesafe/v1/systemone`, `GET .../typesafe/v1/models` | `Bearer` AI Gateway key or OIDC | `typesafe-ai/jev` | Same body and answer shapes; `model` in response echoes `typesafe-ai/jev`, not the versioned ID. Errors `{message, error_type}`. Base URL is `.../typesafe` (path prefix) ([docs](https://vercel.com/docs/ai-gateway/sdks-and-apis/typesafe), [changelog 2026-09-21](https://vercel.com/changelog/ai-gateway-now-supports-typesafe-clients-and-http-api-for-jev)) |
| Vercel AI Gateway (native evaluation) | `POST https://ai-gateway.vercel.sh/v1/evaluate` or AI SDK ≥ 7 `experimental_evaluate` | same | `typesafe-ai/jev` | **Different schema**: Noul is `type: "boolean"` and returns `{probability}`; answers omit `confidence` in the documented examples; usage is `inputTokens/outputTokens`. Not a base_url swap ([evaluation docs](https://vercel.com/docs/ai-gateway/modalities/evaluation)) |
| OpenRouter (System One path) | `POST https://openrouter.ai/api/v1/systemone` | `Bearer` OpenRouter key | `typesafe/jev-1.13` or `~typesafe/jev-latest` | Documented as "switch the TypeSafe SDK by changing the base URL"; adds `usage.cost` (USD). Context advertised as 32k ([Jev on OpenRouter](https://openrouter.ai/docs/guides/community/jev), [model page](https://openrouter.ai/typesafe/jev-1.13)) |
| OpenRouter (Decisions API) | `POST https://openrouter.ai/api/alpha/decisions` | same | same | Same `{model, state, questions}` body and `answers` shape in the published sample; alpha path. Optional `HTTP-Referer`/`X-Title` headers. Model page showed 33 % 3-day availability on 2026-09-17, so keep TypeSafe direct as primary ([model page](https://openrouter.ai/typesafe/jev-1.13)) |
| Cloudflare Workers AI | `POST https://api.cloudflare.com/client/v4/accounts/{id}/ai/run` with `{ "model": "typesafe/jev", "input": { state, questions } }` | `Bearer` CF token | `typesafe/jev` | **Different envelope** (`input` wrapper, model at top level); response `answers` identical to TypeSafe including `confidence`. Requires a small adapter, not a base_url swap ([Cloudflare model page](https://developers.cloudflare.com/ai/models/typesafe/jev/)) |
| LiteLLM proxy (≥ v1.103.0) | `POST {proxy}/typesafe/v1/systemone` (any `/typesafe/*` path forwarded) | `Bearer` LiteLLM virtual key | `jev-latest` etc. | Pure pass-through; response unchanged; spend logged under the versioned model ([LiteLLM docs](https://docs.litellm.ai/docs/pass_through/typesafe)) |

Recommendation: keep `base_url` configurable with the path prefix included (e.g. `https://ai-gateway.vercel.sh/typesafe`, `https://openrouter.ai/api`, `http://litellm:4000/typesafe`) and always append `/v1/systemone`; make `model` a free string. Do not promise Cloudflare or Vercel's native `/v1/evaluate` without an adapter. Correct the SPEC sentence accordingly.

### A4. Question designs for the reference strategies

Design rules applied: one atomic judgement per question, all numeric work in Go, bucket labels alongside numbers, explicit `none`/`other`, criteria as `{what, examples}` objects where the boundary matters, backticked field references, and speculative fan-out (ask everything in one call).

**funding_skew** (cadence 5m, per market; one request per market keeps state small). State built by `Snapshot`:

```json
{
  "market": "ETH",
  "funding": {"rate_8h_pct": 0.0125, "annualized_pct": 13.7, "z_30d": 2.6,
              "bucket": "extreme_positive", "percentile_30d": 98,
              "consecutive_periods_same_sign": 9},
  "premium": {"mark_vs_oracle_pct": 0.031, "bucket": "mark_above_oracle"},
  "open_interest": {"change_24h_pct": 18.4, "bucket": "rising_fast"},
  "price": {"return_24h_pct": 4.1, "bucket": "up_moderate", "return_1h_pct": -0.3},
  "context": {"regime_hint": "risk_on", "hours_to_next_funding": 2.5}
}
```

Buckets are computed in Go from configurable thresholds (`z_30d > 2 → extreme_positive`, etc.). Questions:

- `funding_extreme` (noul): instructions `"Is funding for `market` extreme relative to its recent history, judging from `funding.bucket`, `funding.z_30d` and `funding.percentile_30d`?"`, criteria `true: "bucket is extreme_positive or extreme_negative, or |z_30d| ≥ 2"`, `false: "bucket is normal or mild; |z_30d| < 2"`. (This is nearly deterministic; keep it because the governor threshold `min_p` sits on it and it lets the operator see Jev agreeing with the arithmetic.)
- `direction` (choice): instructions `"Which side is over-crowded and should be faded, given `funding`, `premium` and `open_interest`?"`; criteria `fade_long: {what: "Longs are paying: positive funding, mark above oracle, OI rising with price", examples: ["funding extreme_positive with rising_fast OI"]}`, `fade_short: {what: "Shorts are paying: negative funding, mark below oracle, OI rising while price falls"}`, `none: {what: "No crowded side: funding normal, or signals disagree (e.g. positive funding but OI falling)"}`.
- `crowding` (score, 3 levels): instructions `"How crowded is the paying side?"`; levels `["not crowded: funding normal and OI flat", "somewhat crowded: funding elevated or OI rising, not both", "extremely crowded: funding extreme and OI rising_fast and premium in the same direction"]`.
- Speculative extras in the same call (ignored unless needed): `momentum_against_fade` (noul: is `price.return_1h_pct` moving against the fade?), used by `Decide` to widen `price_limit`.

`Decide`: open opposite side only if `funding_extreme.noul ≥ params.min_p`, `direction.choice != none`, `direction.confidence ≥ config.min_confidence`, and `crowding.score ≥ params.min_crowding`. Intent `confidence` = `direction.confidence`.

**regime_rotation** (cadence 1h, one request). State:

```json
{
  "btc": {"ret_1d_pct": -3.2, "ret_7d_pct": -9.8, "ret_30d_pct": 2.1,
          "rv_7d_annualized_pct": 68, "rv_bucket": "high", "trend_7d": "down_strong"},
  "eth": {"ret_1d_pct": -4.5, "ret_7d_pct": -14.0, "ret_30d_pct": -1.2,
          "rv_7d_annualized_pct": 85, "rv_bucket": "very_high", "trend_7d": "down_strong"},
  "breadth": {"pct_top20_above_20d_ma": 15, "bucket": "narrow"},
  "funding_avg_bucket": "negative", "eth_btc_ratio_trend_7d": "down"
}
```

- `regime` (choice): instructions `"Which regime best describes the market right now?"`; criteria `risk_on: {what: "Broad uptrend with rising breadth", examples: ["trend_7d up_* on both, breadth wide"]}`, `risk_off: {what: "Broad downtrend or elevated volatility with narrow breadth", examples: ["trend_7d down_* on both, rv_bucket high or very_high"]}`, `chop: {what: "No clear direction: small returns, mixed trends, mid breadth"}`, `unclear: {what: "Signals contradict each other (e.g. BTC up strongly while breadth narrow)"}` — the explicit "other".
- `conviction` (score, 4 levels): `["weak: signals mixed", "moderate: most signals agree", "strong: all trend and breadth signals agree", "very strong: all signals agree and volatility bucket confirms"]`.
- Speculative: `vol_shock` (noul: is `rv_bucket` very_high on either asset?) used to halve target weights regardless of regime.

`Decide`: rebalance toward `params.weights[regime]` only when `regime.choice != unclear`, `regime.confidence ≥ min_confidence`, `conviction.score ≥ params.min_conviction`.

Both fixtures should be sampled 15× against the live API (once a key exists) to record flip rates before thresholds are chosen, following the consistency cookbook method.

---

## Part B — Chain-agnostic venue survey

Baseline: the existing `hlclient` already covers `metaAndAssetCtxs` (funding, premium, OI, mark/oracle), `allMids`, `clearinghouseState`, `openOrders`, `candleSnapshot`, and `signing/` implements the msgpack → keccak → phantom-agent → EIP-712 flow for L1 actions, so `hyperliquid` maps directly onto the Venue interface.

| Venue | Market data | Positions / account | Order placement and signing | Go SDK | Testnet | Venue mapping notes |
|---|---|---|---|---|---|---|
| **Hyperliquid** (L1) | `POST /info` `metaAndAssetCtxs` (funding, premium, OI, markPx, oraclePx), `allMids`, `candleSnapshot`; WS | `clearinghouseState`, `openOrders` | `POST /exchange` with msgpack-hashed action + EIP-712 "phantom agent" signature (chain `a` mainnet / `b` testnet); agent/API wallets supported | In-repo client + [community SDKs](https://github.com/tonymontanov/go-hyperliquid) | `api.hyperliquid-testnet.xyz`, full API parity | Markets ← `metaAndAssetCtxs`; Positions ← `clearinghouseState.assetPositions`; Place/Cancel ← `order`/`cancel` actions ([Exchange endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint), [Perps info](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/perpetuals)) |
| **Lighter** (zk-rollup) | REST `GET /api/v1/orderBookDetails`, funding/candles endpoints; WS | Account by L1 address → `account_index`; auth token (8 h) for private reads | Custom L2 transaction signing per API key (indices 2–254, per-key nonce) via `SignerClient`; signing implemented in a cgo shared library | **Official** [`elliottech/lighter-go`](https://github.com/elliottech/lighter-go) (signing + minimal HTTP; Python SDK has the fuller HTTP/WS surface) | Yes; base URL differs (`mainnet.zklighter.elliot.ai` vs testnet) | Integer price/size with per-market decimals; `client_order_index` for cancels. Signature scheme details **unverified** beyond "shared-library signer" ([Get started](https://apidocs.lighter.xyz/docs/get-started)) |
| **dYdX v4** (Cosmos app-chain) | Indexer REST `https://indexer.dydx.trade/v4/*` (markets, funding, candles) + WS | Indexer `subaccounts` for positions/fills | Cosmos SDK tx (`SIGN_MODE_DIRECT`, secp256k1) to a validator/OEGS gRPC node; short-term orders are gas-free | No official Go client (TS, Python, Rust official); community [`DaisukeYoda/godex`](https://pkg.go.dev/github.com/DaisukeYoda/godex/dydx) | Yes, with faucet; identical API | Requires gRPC + protobuf deps in Go; block-time semantics (goodTilBlock) differ from Hyperliquid ([Connecting](https://docs.dydx.xyz/interaction/endpoints)) |
| **GMX v2** (Arbitrum/Avalanche) | HTTP `https://{chain}.gmxapi.io/v1` (markets, tickers, positions) + Oracle API on `gmxinfra.io` | GMX API positions/orders endpoints | Two-phase (create order → keeper executes at oracle price); Express orders are EIP-712 typed data relayed via Gelato, otherwise on-chain `ExchangeRouter` tx | None; TS `@gmx-io/sdk` only | Arbitrum Sepolia contracts | Oracle-priced execution means no limit-price guarantee; needs an EVM RPC + ABI bindings in Go ([API overview](https://docs.gmx.io/docs/api/overview/)) |
| **Vertex** (Arbitrum et al.) | Gateway REST/WS queries (`market_prices`, `all_products`), archive for funding | `subaccount_info` | EIP-712 `place_order` with linked signer; Vertex "Edge" spans several chains | None; TS, Python, Rust SDKs | Arbitrum Sepolia testnet (per docs) | Straightforward REST + EIP-712 (reusable go-ethereum signer); multi-chain product IDs ([API](https://docs.vertexprotocol.com/developer-resources/api)) |
| **Aevo** (OP-stack L2) | REST `/markets`, `/funding`; WS | `/account`, `/positions` | EIP-712 `Order` struct signed with a delegated signing key; REST submission | None official | Sepolia domain (`chainId 11155111`) | Simple; options-first venue, perps volume thin ([Signing orders](https://api-docs.aevo.xyz/reference/signing-orders)) |
| **Drift → Velocity** (Solana) | Data API + on-chain accounts | On-chain user accounts | Solana transactions against the Velocity program; TS SDK only | None (TS official; Rust source-only) | Testnet "launching soon"; mainnet relaunch date not fixed after the April 2026 exploit | **Not viable now**: program IDs and SDK changed, no Go path, no live mainnet ([Velocity dev docs](https://docs.drift.trade/developers), [status](https://thedefiant.io/news/defi/drift-protocol-rebrands-to-velocity-dex-ahead-of-relaunch)) |
| **Bybit v5** (CEX) | REST `GET /v5/market/tickers`, `/funding/history`, `/kline`; WS | `GET /v5/position/list`, `/account/wallet-balance` | REST `POST /v5/order/create`, HMAC-SHA256 or RSA over timestamp+key+recv+body | [`tonymontanov/go-bybit`](https://pkg.go.dev/github.com/tonymontanov/go-bybit) and others | `api-testnet.bybit.com`, testnet flag in SDKs | Unified `linear` category = USDT perps; maps 1:1 to Venue ([Bybit v5 guide](https://bybit-exchange.github.io/docs/v5/guide)) |
| **Binance USDⓈ-M** (CEX) | `GET /fapi/v1/premiumIndex` (funding, mark), `/ticker`, `/klines`; WS | `GET /fapi/v2/positionRisk`, `/fapi/v2/account` | `POST /fapi/v1/order`, HMAC-SHA256 (or Ed25519) signed query | Several community SDKs | `demo-fapi.binance.com` | Geo-restrictions apply; heavier KYC ([Binance futures docs](https://developers.binance.com/docs/derivatives/usds-margined-futures/general-info)) |
| **paper** (in-memory) | Snapshot from any real venue's market data (or fixtures) | Simulated ledger keyed by market | Immediate fill at mark ± configurable slippage; no signing | n/a | n/a | Implements the full interface; `Capabilities` reports `perps`, `simulated` |

**Recommendation for the second real venue: Bybit v5 (CEX), with Lighter as the second on-chain venue.**
Bybit costs the least: HMAC signing is ~20 lines of stdlib Go, there is a full-parity testnet, mature Go SDKs exist, and its REST resource model (tickers, funding history, position list, order create/cancel) maps 1:1 onto `Markets/Positions/Place/Cancel`, which is the best proof that the Venue interface is chain-agnostic rather than Hyperliquid-shaped. If the requirement is specifically a second *on-chain* venue, Lighter is the pick: it is the only DEX here with an official Go SDK and a testnet, and its per-key nonce and integer price model are close to Hyperliquid's mental model; the cost is a cgo signer binary in the build. dYdX v4 is viable but drags in gRPC/protobuf and block-height order semantics; GMX's oracle execution breaks the `price_limit` contract; Vertex and Aevo have no Go SDKs (though go-ethereum's EIP-712 signer is reusable); Drift/Velocity is not live.

---

## Part C — Risks and open questions

1. **No calibration evidence on financial state.** All published calibration is on text tickets and moderation; confidence bands must be measured on labelled market fixtures before `threshold` mode is enabled.
2. **Numeric weakness is documented.** Jev is "not a calculator"; every threshold, z-score and comparison must live in Go, and questions must lean on bucket labels. Score interpolation between levels is unreliable.
3. **`jev-latest` moves silently.** Pin `jev-1.13.0` in production config, log `response.model` per DecisionRecord (already in PROTOCOL.md), and re-tune thresholds on each version bump.
4. **Cost is negligible, rate limits are not the constraint.** ~700 input tokens/tick → $0.00003 per call; 10 strategies at 1-minute cadence ≈ 14.4k calls/day ≈ 10M tokens ≈ $0.42/day; well under 1,200 rpm. Fan-out extra questions freely.
5. **Latency budget.** Plan for p95 ≈ 250 ms, timeout 5 s as specified; SPEC's "retry once on 5xx" should also cover 429/529 with `retry-after`, and 408.
6. **Error body shape and JSON tokenization are unverified** from a TypeSafe primary source; the Go client should keep raw error bodies and measure `usage.input_tokens` on fixtures.
7. **Rate limits and availability are "adjusting dynamically"** during early access; OpenRouter showed 33 % 3-day availability mid-September. Keep the `fake` decider as a degraded mode.
8. **Adversarial state.** Jev does not treat state as hostile; never pass free text from external sources (news, social) into strategy state without filtering.
9. **Structural invariants are not guaranteed** across questions; do not build `Decide` logic on identities between separate answers (e.g. Noul vs Choice on the same question).
10. **Gateway claims corrected.** Vercel `/typesafe`, OpenRouter `/api/v1/systemone` and LiteLLM are base-URL swaps; Cloudflare Workers AI and Vercel `/v1/evaluate` are not.
