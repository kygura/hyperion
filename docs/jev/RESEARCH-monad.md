# RESEARCH — Monad venue for the strategy runtime

Status: research note, 2026-09-23. Companion to [SPEC.md](./SPEC.md) (Phase 2),
[PROTOCOL.md](./PROTOCOL.md) and [RESEARCH.md](./RESEARCH.md) Part B.

Method: official docs read through Exa and web search mirrors. Every Monad RPC
endpoint (`rpc.monad.xyz`, `rpc1..3.monad.xyz`, `rpc-mainnet.monadinfra.com`,
`testnet-rpc.monad.xyz`, Ankr, dRPC) and `docs.kuru.io` returned 403 from this
session's egress proxy, so **no fact below was checked against the live chain**.
Items that could not be confirmed from a primary source are listed at the end.

---

## 1. Chain facts

| Item | Mainnet | Testnet | Source |
|---|---|---|---|
| Chain id | `143` (`0x8f`) | `10143` | [Network info (mainnet)](https://docs.monad.xyz/developer-essentials/network-information), [Network info (testnet)](https://docs.monad.xyz/developer-essentials/testnets) |
| Native token | MON (18 decimals) | MON (faucet: faucet.monad.xyz) | same |
| Public RPC | `https://rpc.monad.xyz` (QuickNode, 25 rps, batch 100); `rpc1` Alchemy 15 rps; `rpc2` Goldsky; `rpc3` Ankr; `rpc-mainnet.monadinfra.com` (20 rps, batch 1) | `https://testnet-rpc.monad.xyz` (50 rps; 25 rps for `eth_call`/`eth_estimateGas`); `rpc-testnet.monadinfra.com` | same |
| Explorers | monadvision.com, monadscan.com | testnet.monadvision.com, testnet.monadscan.com | same |
| Wrapped native | WMON `0x3bd359C1119dA7Da1D913D1C4D2B7c461115433A` | WMON `0xFb8bf4c1CC7a94c73D209a149eA2AbEa852BC541` | same |
| Multicall3 / Permit2 | canonical addresses (`0xcA11…CA11`, `0x0000…8ba3`) | same | same |
| Client version | `v0.15.2` / revision `MONAD_NINE` (both networks at time of reading) | | same |
| Testnet reset | — | reset from genesis on **2025-12-16**; anything deployed before that is gone | [Testnet](https://docs.monad.xyz/developer-essentials/testnets) |

### Gas model

- EIP-1559 compatible: `price_per_gas = min(base + priority, max)`; type-2
  transactions work unchanged ([Gas pricing](https://docs.monad.xyz/developer-essentials/gas-pricing)).
- **Charged on gas limit, not gas used** (`gas_paid = gas_limit × price`), a
  DoS guard for asynchronous execution. Consequence for the adapter: estimate,
  add a small buffer (we use +15 %), never pad generously.
- Minimum base fee 100 MON-gwei; block gas limit 200M; per-tx gas limit 30M.
  Base-fee controller rises slower and falls faster than Ethereum's.
- `eth_maxPriorityFeePerGas` returns a hardcoded 2 gwei today ("temporary").
- `eth_feeHistory` with newest=`latest` repeats the latest base fee instead of
  projecting the next one.
- Blob (type-3) transactions are rejected ([Differences](https://docs.monad.xyz/developer-essentials/differences)).

### RPC and finality quirks relevant to go-ethereum

From [JSON-RPC overview](https://docs.monad.xyz/reference/rpc-differences):

- Monad "aims to match the RPC behavior of Geth as closely as possible", so
  `ethclient` (go-ethereum v1.15.11, already a backend dependency) works over
  plain HTTP JSON-RPC: `eth_chainId`, `eth_blockNumber`, `eth_getBalance`,
  `eth_call`, `eth_estimateGas`, `eth_getTransactionCount`, `eth_gasPrice`,
  `eth_maxPriorityFeePerGas`, `eth_getBlockByNumber`, `eth_sendRawTransaction`,
  `eth_getTransactionReceipt` are all supported.
- **Deferred validation**: `eth_sendRawTransaction` may accept a tx with a nonce
  gap or insufficient balance because the RPC node may not have the latest
  state (asynchronous execution). A successful send is not an acceptance;
  only a receipt is.
- **No pending lookups**: `eth_getTransactionByHash` returns null until the tx
  is in a block. The adapter therefore polls `eth_getTransactionReceipt` (which
  go-ethereum maps to `ethereum.NotFound` until mined) with a timeout.
- Block tags map to commitment states: `latest` = Proposed (speculative),
  `safe` = Voted, `finalized` = Finalized. Receipts can come from a
  non-finalized block and may change. For a prototype venue that places small
  swaps, a `latest` receipt is accepted as execution; settlement-grade
  crediting would compare `blockNumber` to the `finalized` head.
- Full nodes do not serve arbitrary historic state; `eth_call` against old
  blocks may fail. The adapter only calls at `latest`.
- `eth_call`/`eth_estimateGas` gas caps: 200M on the public providers.
- Reserve Balance: transactions that would drop the account below a reserve
  can be included and then revert; they still pay gas. Keep a MON buffer.

---

## 2. On-chain venues surveyed

| Venue | Type | Stable public ABI | Testnet | Integration cost from Go | Notes |
|---|---|---|---|---|---|
| **Uniswap v3** (official deployment) | Concentrated-liquidity AMM | Yes: `@uniswap/v3-periphery@1.0.0`, `@uniswap/swap-router-contracts@1.1.0`; immutable, identical ABI on every chain | Addresses published for Monad testnet in `sdk-core` (see unverified list) | Low: `QuoterV2.quoteExactInputSingle` via `eth_call`, `SwapRouter02.exactInputSingle` tx, ERC-20 `approve/allowance/balanceOf`. All packable with `accounts/abi` | Monad mainnet: Factory `0x204faca1764b154221e35c0d20abb3c525710498`, **QuoterV2 `0x661e93cca42afacb172121ef892830ca3b70f08d`**, **SwapRouter02 `0xfe31f71c1b106eac32f1a19239c9a9a72ddfb900`**, UniversalRouter `0x0d97dc33264bfc1c226207428a79b26757fb9dc3` ([Uniswap Monad deployments](https://developers.uniswap.org/docs/protocols/v3/deployments/v3-monad-deployments), [sdk-core addresses.ts](https://github.com/Uniswap/sdks/blob/main/sdks/sdk-core/src/addresses.ts)) |
| **Kuru** | On-chain CLOB + backstop AMM, plus Kuru Flow aggregator | Documented Solidity interfaces (Router `anyToAnySwap`, OrderBook `addBuyOrder/addSellOrder`, `placeAndExecuteMarketBuy/Sell`, `batchCancelOrders`, `bestBidAsk`); official SDK is TypeScript | Yes (Router `0x7EFb…4630`, MON-USDC market `0xa241…D2D9`) | Medium: uint32 prices / uint96 sizes scaled by per-market `pricePrecision`/`sizePrecision`, margin-account deposits for resting orders, native-vs-ERC20 market types | Mainnet: Router `0xd651346d7c789536ebf06dc72aE3C8502cd695CC`, MarginAccount `0x2A68…90c5`, KuruFlowEntrypoint `0xb3e6…13cb`; official markets MON-AUSD `0x131a…0da9`, MON-USDC `0x065C…C394` ([Kuru contract addresses](https://docs.kuru.io/contracts/Contract-addresses), [OrderBook](https://docs.kuru.io/contracts/OrderBook), [Router](https://docs.kuru.io/contracts/Router)) |
| **Perpl** | Perpetuals on an on-chain CLOB | Exchange contract is on-chain (`0x34B6…2a6F` mainnet, `0x1964…80cc` testnet), but **trading is via WebSocket/REST with Ed25519 API keys** enrolled by a wallet signature; Rust SDK | Yes (`testnet.perpl.xyz`, chain 10143) | High for an EVM adapter: separate auth scheme, WS order protocol, per-network market ids (BTC=1, MON=10, ETH=20… mainnet; 16/64/32… testnet), AUSD collateral | Best fit for perps on Monad, but it is a REST/WS venue, not an ethclient venue ([Perpl api-docs](https://github.com/PerplFoundation/api-docs), [docs.perpl.xyz](https://docs.perpl.xyz/)) |
| Uniswap v4 | Singleton AMM with hooks | Yes, but swaps go through UniversalRouter command encoding + Permit2 | Mainnet only listed | Higher (command bytes, Permit2 signatures) | PoolManager `0x188d…a8e`, V4Quoter `0xa222…891` on mainnet |
| Monday Trade, LFJ, others | AMMs / aggregators | Varying | Some | Not assessed in depth | Seen in pool listings; no reason to prefer over Uniswap v3 for a first adapter |

### Liquidity snapshot (third-party, point in time)

GeckoTerminal listings for Uniswap v3 on Monad at time of reading: WMON/USDC
0.3 % pool `0x659b…a9da` ≈ $1.3M liquidity, ≈ $0.86M 24h volume; WETH/USDC
0.3 % pool `0x25ef…5bf1` ≈ $35k liquidity; WMON/USDC 1 % ≈ $9k; WETH/WMON 0.05 %
effectively empty ([WMON/USDC](https://www.geckoterminal.com/monad/pools/0x659bd0bc4167ba25c62e05656f78043e7ed4a9da),
[WETH/USDC](https://www.geckoterminal.com/monad/pools/0x25ef1a210ff55bcee9f8fee979aaff6bd1be5bf1)).
Only the MON pair is deep enough for anything but tiny test sizes; ETH is
configured for price visibility and small swaps.

### Tokens (official Monad token list)

Mainnet ([tokenlist-mainnet.json](https://github.com/monad-crypto/token-list)):
USDC `0x754704Bc059F8C67012fEd69BC8A327a5aafb603` (6), AUSD
`0x00000000eFE302BEAA2b3e6e1b18d08D69a9012a` (6), USDT0
`0xe7cd86e13AC4309349F30B3435a9d337750fC82D` (6), WETH
`0xEE8c0E9f1BFFb4Eb878d8f15f368A02a35481242` (18), WBTC
`0x0555E30da8f98308EdB960aa94C0Db47230d2B9c` (8), WMON (18).
Testnet: USDC `0x534b2f3A21130d7a60830c2Df862319e593943A3` (6), WETH
`0x45477f4709771331db81944A5E20eF95Bc7BA2D7` (18), WMON (18). Kuru's docs list
a different testnet USDC (`0x3bA3…1570`), so testnet stablecoins are not
canonical across venues.

---

## 3. Chosen protocol and why

**Uniswap v3 on Monad: QuoterV2 for prices, SwapRouter02 `exactInputSingle`
for bounded-slippage spot swaps.** Capability reported: `spot` (plus
`execute` when a signer is configured).

1. **Stable ABI.** The v3 periphery contracts are immutable and the same
   bytecode/ABI everywhere; we embed four small ABI fragments (ERC-20,
   QuoterV2, SwapRouter02) and nothing moves under us. Kuru's interfaces are
   documented but upgradeable (Router exposes implementation updates) and its
   integer price/size scaling is per market.
2. **Pure JSON-RPC.** Quoting is one `eth_call`; placing is `approve` (only
   when allowance is short) plus one type-2 transaction. No API keys, no
   off-chain auth, no WS session: exactly what `ethclient` gives us, and it is
   testable with an `httptest` JSON-RPC stub.
3. **On-chain price.** `quoteExactInputSingle` returns the executable output
   for a probe size, so the mark is what the venue would actually pay,
   including the pool fee. No external price feed is needed.
4. **Bounded slippage is native.** `amountOutMinimum` is computed from a fresh
   quote minus `slippage_bps`, tightened further by the intent's
   `price_limit` when present; the swap reverts on chain rather than filling
   worse.
5. **Deepest liquidity for the default universe** (MON/USDC) among venues with
   a stable ABI.

Not chosen now:

- **Perpl** is the right target for *perps* on Monad (funding_skew would need
  it), but it is a WebSocket/REST venue with Ed25519 request signing, so it
  belongs in its own adapter (`venue/perpl`), not in the EVM venue. Next step
  if the operator wants perps on Monad.
- **Kuru** is the natural second EVM protocol inside `venue/monad` (a
  `protocol = "kuru"` switch using `placeAndExecuteMarketBuy/Sell` for taker
  swaps and `addBuyOrder` + `batchCancelOrders` for a real Cancel). Deferred
  because only two official markets exist and precision handling needs the
  live `getMarketParams()` values, which we could not read from here.

### Adapter shape (implemented in `backend/internal/strategy/venue/monad`)

- `Status`: `eth_chainId` (mismatch with config → degraded), `eth_blockNumber`,
  native balance of the signer; unreachable RPC → `disconnected`, partial
  failure → `degraded`. Extra fields go in the optional `meta` object
  (`chain_id`, `network`, `head_block`, `native_balance`, `address`, `protocol`).
- `Markets`: one QuoterV2 quote per configured pair (`probe` units of the base
  token → quote token, assumed USD-pegged). Funding, premium, OI are zero
  (spot); `day_change` is derived from the venue's own in-memory samples once
  it has 24h of them, else zero.
- `Positions`: ERC-20 balances of each pair's base token × quoted price (spot,
  long-only; entry unknown → 0; uPnL 0).
- `Place`: enforces `max_notional_usd` itself (min of the venue's own cap and
  the governor's global cap), refuses sells larger than the held balance
  (spot cannot short; reduce-only sells clip to the balance), quotes,
  computes `amountOutMinimum`, approves exactly `amountIn` if the router
  allowance is short, signs an EIP-1559 tx (`LatestSignerForChainID`), sends,
  waits for the receipt with a timeout, reads the actual output from the
  ERC-20 `Transfer` log, and returns the fill with `tx_hash`.
- `Cancel`: not applicable (swaps settle atomically) → `ErrNotImplemented`.
- Signer from `MONAD_PRIVATE_KEY` (env name configurable, but the Hyperliquid
  key variables `HL_AGENT_KEY` / `HL_MASTER_KEY` are refused by config
  validation). RPC from `MONAD_RPC_URL`, else config, else the network's public
  endpoint.

---

## 4. Unverified

1. **Nothing was checked against a live Monad node**: all RPC hosts are
   egress-blocked here. Chain ids, contract code presence, pool existence and
   quotes are from documentation only.
2. **QuoterV2 address discrepancy.** Uniswap's Monad deployments page lists
   QuoterV2 `0x661e93cca42afacb172121ef892830ca3b70f08d`; `sdk-core`'s
   `MONAD_ADDRESSES.quoterAddress` is `0x2d01411773c8c24805306e89a41f7855c3c4fe65`
   (possibly the v1 Quoter, whose ABI differs). The adapter defaults to the
   docs' QuoterV2 and makes `quoter` configurable. Verify with
   `eth_getCode` + one `quoteExactInputSingle` before enabling.
3. **Testnet Uniswap v3 addresses** (`sdk-core` `MONAD_TESTNET_ADDRESSES`:
   QuoterV2 `0x1ba215c17565de7b0cb7ecab971bcf540c24a862`, SwapRouter02
   `0x4c4eabd5fb1d1a7234a48692551eaecff8194ca7`) may predate the 2025-12-16
   testnet reset. The testnet defaults are best-effort; override `quoter` and
   `router` in config after checking the explorer.
4. **Pool depth and fee tiers** are a third-party snapshot with an unknown
   exact timestamp. The default pairs use the 0.3 % tier.
5. **Receipt semantics.** We treat a receipt at `latest` (Proposed) as
   executed; Monad documents that such data can change until Finalized.
6. **Gas estimation on Monad** charges the full limit; the +15 % buffer is a
   judgment call, not a documented recommendation.
7. **Kuru and Perpl** addresses and API shapes are from their docs/READMEs
   only; neither was exercised.
