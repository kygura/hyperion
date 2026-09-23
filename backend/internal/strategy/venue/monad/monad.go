// Package monad is the EVM venue for Monad (docs/jev/RESEARCH-monad.md):
// spot swaps on Uniswap v3 over plain JSON-RPC through go-ethereum's
// ethclient. Prices come from QuoterV2 (eth_call), positions are ERC-20
// balances valued at those quotes, and Place is a bounded-slippage
// SwapRouter02.exactInputSingle signed as an EIP-1559 transaction.
//
// The venue enforces max_notional_usd itself: the executor's risk gates are
// Hyperliquid-specific and never see these orders. The signer comes from its
// own key (MONAD_PRIVATE_KEY by default), never from the Hyperliquid keys.
package monad

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// ID is the venue id.
const ID = "monad"

// Protocol names the on-chain protocol this adapter speaks.
const Protocol = "uniswap_v3"

// Networks.
const (
	Mainnet = "mainnet"
	Testnet = "testnet"
)

// Backend is the slice of *ethclient.Client the venue needs; tests point a
// real ethclient at an httptest JSON-RPC stub, but any implementation works.
type Backend interface {
	ChainID(ctx context.Context) (*big.Int, error)
	BlockNumber(ctx context.Context) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
}

// Token is an ERC-20 the venue trades or quotes in.
type Token struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

// Pair is one tradable market: Symbol is what strategies see ("MON",
// "ETH"); Token is the base ERC-20; Fee is the Uniswap v3 pool fee tier in
// hundredths of a bip (500, 3000, 10000); Probe is the base amount quoted
// for the mark (default 1 token; lower it for expensive or thin tokens).
type Pair struct {
	Symbol string
	Token  Token
	Fee    uint32
	Probe  float64
}

// NetworkDefaults are the per-network constants from RESEARCH-monad.md.
type NetworkDefaults struct {
	ChainID int64
	RPCURL  string
	Quoter  string
	Router  string
	Quote   Token
	Pairs   []Pair
}

// Defaults returns the network's defaults (mainnet when name is unknown).
// Testnet contract addresses are best effort (see RESEARCH-monad.md
// "Unverified"): override quoter/router in config after checking them.
func Defaults(network string) NetworkDefaults {
	if network == Testnet {
		return NetworkDefaults{
			ChainID: 10143,
			RPCURL:  "https://testnet-rpc.monad.xyz",
			Quoter:  "0x1ba215c17565de7b0cb7ecab971bcf540c24a862",
			Router:  "0x4c4eabd5fb1d1a7234a48692551eaecff8194ca7",
			Quote:   Token{Symbol: "USDC", Address: "0x534b2f3A21130d7a60830c2Df862319e593943A3", Decimals: 6},
			Pairs: []Pair{
				{Symbol: "MON", Token: Token{Symbol: "WMON", Address: "0xFb8bf4c1CC7a94c73D209a149eA2AbEa852BC541", Decimals: 18}, Fee: 3000, Probe: 1},
				{Symbol: "ETH", Token: Token{Symbol: "WETH", Address: "0x45477f4709771331db81944A5E20eF95Bc7BA2D7", Decimals: 18}, Fee: 3000, Probe: 0.01},
			},
		}
	}
	return NetworkDefaults{
		ChainID: 143,
		RPCURL:  "https://rpc.monad.xyz",
		Quoter:  "0x661e93cca42afacb172121ef892830ca3b70f08d",
		Router:  "0xfe31f71c1b106eac32f1a19239c9a9a72ddfb900",
		Quote:   Token{Symbol: "USDC", Address: "0x754704Bc059F8C67012fEd69BC8A327a5aafb603", Decimals: 6},
		Pairs: []Pair{
			{Symbol: "MON", Token: Token{Symbol: "WMON", Address: "0x3bd359C1119dA7Da1D913D1C4D2B7c461115433A", Decimals: 18}, Fee: 3000, Probe: 1},
			{Symbol: "ETH", Token: Token{Symbol: "WETH", Address: "0xEE8c0E9f1BFFb4Eb878d8f15f368A02a35481242", Decimals: 18}, Fee: 3000, Probe: 0.01},
		},
	}
}

// Config configures the venue. Zero values take the network defaults.
type Config struct {
	Network        string        // mainnet | testnet (default mainnet)
	RPCURL         string        // resolved by the caller (env MONAD_RPC_URL → config → default)
	ChainID        int64         // expected chain id; 0 → network default
	PrivateKeyHex  string        // signer; "" → read-only (no Place)
	Quoter         string        // QuoterV2 address
	Router         string        // SwapRouter02 address
	Quote          Token         // USD-pegged quote token
	Pairs          []Pair        // tradable markets
	SlippageBps    float64       // amountOutMinimum = quote × (1 − bps/1e4); default 50
	MaxNotionalUSD float64       // venue's own cap; 0 → only the governor cap applies
	ReceiptTimeout time.Duration // default 30s
	GasBufferPct   float64       // added to eth_estimateGas; default 15 (Monad charges the full limit)
}

// Option configures the venue.
type Option func(*Venue)

// WithBackend injects the JSON-RPC backend (tests); otherwise New dials RPCURL.
func WithBackend(b Backend) Option { return func(v *Venue) { v.backend = b } }

// WithNotionalCap adds a dynamic cap (the governor's global max_notional_usd).
func WithNotionalCap(f func() float64) Option { return func(v *Venue) { v.capFn = f } }

// WithClock injects the clock (tests).
func WithClock(now func() time.Time) Option { return func(v *Venue) { v.now = now } }

// WithPollInterval sets the receipt poll interval (tests).
func WithPollInterval(d time.Duration) Option { return func(v *Venue) { v.poll = d } }

type pair struct {
	symbol  string
	token   common.Address
	tokSym  string
	dec     int
	fee     uint32
	probe   float64
	samples []sample // in-memory price history for day_change
}

type sample struct {
	ts    time.Time
	price float64
}

// Venue is the adapter. Safe for concurrent use.
type Venue struct {
	cfg      Config
	backend  Backend
	chainID  *big.Int
	key      *ecdsa.PrivateKey
	address  common.Address
	quoter   common.Address
	router   common.Address
	quote    common.Address
	quoteDec int
	capFn    func() float64
	now      func() time.Time
	poll     time.Duration

	mu     sync.Mutex
	pairs  map[string]*pair
	order  []string
	sendMu sync.Mutex // serialises nonce use across Place calls
}

// New builds the venue. It never touches the network: ethclient dials HTTP
// lazily, so a daemon with an unreachable RPC still starts and reports the
// venue as disconnected.
func New(cfg Config, opts ...Option) (*Venue, error) {
	if cfg.Network == "" {
		cfg.Network = Mainnet
	}
	if cfg.Network != Mainnet && cfg.Network != Testnet {
		return nil, fmt.Errorf("monad: network must be mainnet|testnet, got %q", cfg.Network)
	}
	def := Defaults(cfg.Network)
	if cfg.RPCURL == "" {
		cfg.RPCURL = def.RPCURL
	}
	if cfg.ChainID == 0 {
		cfg.ChainID = def.ChainID
	}
	if cfg.Quoter == "" {
		cfg.Quoter = def.Quoter
	}
	if cfg.Router == "" {
		cfg.Router = def.Router
	}
	if cfg.Quote.Address == "" {
		cfg.Quote = def.Quote
	}
	if len(cfg.Pairs) == 0 {
		cfg.Pairs = def.Pairs
	}
	if cfg.SlippageBps <= 0 {
		cfg.SlippageBps = 50
	}
	if cfg.ReceiptTimeout <= 0 {
		cfg.ReceiptTimeout = 30 * time.Second
	}
	if cfg.GasBufferPct <= 0 {
		cfg.GasBufferPct = 15
	}
	v := &Venue{
		cfg:     cfg,
		chainID: big.NewInt(cfg.ChainID),
		now:     time.Now,
		poll:    500 * time.Millisecond,
		pairs:   map[string]*pair{},
	}
	for _, a := range []struct {
		name string
		hex  string
		dst  *common.Address
	}{{"quoter", cfg.Quoter, &v.quoter}, {"router", cfg.Router, &v.router}, {"quote token", cfg.Quote.Address, &v.quote}} {
		if !common.IsHexAddress(a.hex) {
			return nil, fmt.Errorf("monad: %s address %q is not a hex address", a.name, a.hex)
		}
		*a.dst = common.HexToAddress(a.hex)
	}
	v.quoteDec = cfg.Quote.Decimals
	if v.quoteDec <= 0 {
		v.quoteDec = 6
	}
	for _, p := range cfg.Pairs {
		sym := strings.ToUpper(strings.TrimSpace(p.Symbol))
		if sym == "" {
			return nil, errors.New("monad: pair with empty symbol")
		}
		if !common.IsHexAddress(p.Token.Address) {
			return nil, fmt.Errorf("monad: pair %s token %q is not a hex address", sym, p.Token.Address)
		}
		if _, dup := v.pairs[sym]; dup {
			return nil, fmt.Errorf("monad: duplicate pair %s", sym)
		}
		dec := p.Token.Decimals
		if dec <= 0 {
			dec = 18
		}
		fee := p.Fee
		if fee == 0 {
			fee = 3000
		}
		probe := p.Probe
		if probe <= 0 {
			probe = 1
		}
		v.pairs[sym] = &pair{symbol: sym, token: common.HexToAddress(p.Token.Address), tokSym: p.Token.Symbol, dec: dec, fee: fee, probe: probe}
		v.order = append(v.order, sym)
	}
	sort.Strings(v.order)
	if k := strings.TrimSpace(cfg.PrivateKeyHex); k != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(k, "0x"))
		if err != nil {
			return nil, fmt.Errorf("monad: private key: invalid hex key")
		}
		v.key = key
		v.address = crypto.PubkeyToAddress(key.PublicKey)
	}
	for _, o := range opts {
		o(v)
	}
	if v.backend == nil {
		c, err := ethclient.Dial(cfg.RPCURL)
		if err != nil {
			return nil, fmt.Errorf("monad: dial rpc: %w", err)
		}
		v.backend = c
	}
	return v, nil
}

// ID implements venue.Venue.
func (v *Venue) ID() string { return ID }

// Address is the signer address (zero when read-only).
func (v *Venue) Address() common.Address { return v.address }

// HasSigner reports whether a key is configured (Place possible).
func (v *Venue) HasSigner() bool { return v.key != nil }

// Capabilities implements venue.Venue: spot swaps, plus execute with a signer.
func (v *Venue) Capabilities() []string {
	caps := []string{"spot"}
	if v.key != nil {
		caps = append(caps, "execute")
	}
	return caps
}

// NotionalCap is the effective per-order cap: the smaller positive of the
// venue's own max_notional_usd and the dynamic (governor) cap; 0 = none.
func (v *Venue) NotionalCap() float64 {
	c := v.cfg.MaxNotionalUSD
	if v.capFn != nil {
		if g := v.capFn(); g > 0 && (c <= 0 || g < c) {
			c = g
		}
	}
	return c
}

// Status implements venue.Venue.
//
//   - chain id unreachable            → disconnected
//   - chain id mismatch               → degraded (never trade on the wrong chain)
//   - head block / balance / positions failing → degraded
//   - otherwise                       → connected
func (v *Venue) Status(ctx context.Context) (venue.VenueStatus, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	meta := map[string]any{
		"network":  v.cfg.Network,
		"chain_id": v.cfg.ChainID,
		"protocol": Protocol,
	}
	if v.key != nil {
		meta["address"] = v.address.Hex()
	}
	st := venue.VenueStatus{
		ID:           ID,
		Kind:         "evm",
		Chain:        "monad",
		Status:       "connected",
		Capabilities: v.Capabilities(),
		Positions:    []venue.Position{},
		Meta:         meta,
	}
	id, err := v.backend.ChainID(cctx)
	if err != nil {
		st.Status = "disconnected"
		st.Error = "rpc: " + err.Error()
		return st, nil
	}
	if id.Cmp(v.chainID) != 0 {
		st.Status = "degraded"
		st.Error = fmt.Sprintf("chain id %s does not match configured %d", id, v.cfg.ChainID)
		meta["chain_id"] = id.Int64()
		return st, nil
	}
	var problems []string
	if head, err := v.backend.BlockNumber(cctx); err != nil {
		problems = append(problems, "head block: "+err.Error())
	} else {
		meta["head_block"] = head
	}
	if v.key != nil {
		if bal, err := v.backend.BalanceAt(cctx, v.address, nil); err != nil {
			problems = append(problems, "native balance: "+err.Error())
		} else {
			meta["native_balance"] = fromUnits(bal, 18)
			meta["native_symbol"] = "MON"
		}
	}
	pos, err := v.Positions(cctx)
	if err != nil {
		problems = append(problems, err.Error())
	} else {
		st.Positions = pos
	}
	if len(problems) > 0 {
		st.Status = "degraded"
		st.Error = strings.Join(problems, "; ")
	}
	return st, nil
}

// Markets implements venue.Venue: one QuoterV2 quote per requested pair.
// Unknown symbols are omitted; a pair whose quote fails is omitted and, if
// none succeed, the first error is returned.
func (v *Venue) Markets(ctx context.Context, symbols []string) ([]venue.Market, error) {
	want := symbols
	if len(want) == 0 {
		want = v.order
	}
	var out []venue.Market
	var firstErr error
	for _, s := range want {
		p := v.pair(s)
		if p == nil {
			continue
		}
		price, err := v.price(ctx, p)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("monad: quote %s: %w", p.symbol, err)
			}
			continue
		}
		out = append(out, venue.Market{Symbol: p.symbol, Mark: price, Mid: price, DayChange: v.record(p, price)})
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

// Positions implements venue.Venue: base-token balances × quoted price.
// Spot is long-only; entry is unknown (0) and uPnL is not tracked.
func (v *Venue) Positions(ctx context.Context) ([]venue.Position, error) {
	if v.key == nil {
		return []venue.Position{}, nil
	}
	out := []venue.Position{}
	for _, sym := range v.order {
		p := v.pair(sym)
		bal, err := v.balanceOf(ctx, p.token, v.address)
		if err != nil {
			return nil, fmt.Errorf("monad: balance %s: %w", p.tokSym, err)
		}
		if bal.Sign() == 0 {
			continue
		}
		price, err := v.price(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("monad: quote %s: %w", p.symbol, err)
		}
		units := fromUnits(bal, p.dec)
		out = append(out, venue.Position{Market: p.symbol, SizeUSD: round2(units * price), Mark: price})
	}
	return out, nil
}

// Place implements venue.Venue as a bounded-slippage Uniswap v3 swap:
// buy = quote token → base, sell = base → quote token.
func (v *Venue) Place(ctx context.Context, o venue.Order) (venue.Fill, error) {
	if v.key == nil {
		return venue.Fill{}, fmt.Errorf("%w: monad Place needs a signer (MONAD_PRIVATE_KEY)", venue.ErrNotImplemented)
	}
	if o.SizeUSD <= 0 || math.IsNaN(o.SizeUSD) || math.IsInf(o.SizeUSD, 0) {
		return venue.Fill{}, errors.New("monad: size_usd must be positive")
	}
	if c := v.NotionalCap(); c > 0 && o.SizeUSD > c {
		return venue.Fill{}, fmt.Errorf("monad: size_usd %.2f exceeds max_notional_usd %.2f", o.SizeUSD, c)
	}
	if o.Side != venue.SideBuy && o.Side != venue.SideSell {
		return venue.Fill{}, fmt.Errorf("monad: side must be buy|sell, got %q", o.Side)
	}
	p := v.pair(o.Market)
	if p == nil {
		return venue.Fill{}, fmt.Errorf("monad: unknown market %q", o.Market)
	}
	price, err := v.price(ctx, p)
	if err != nil {
		return venue.Fill{}, fmt.Errorf("monad: quote %s: %w", p.symbol, err)
	}

	var tokenIn, tokenOut common.Address
	var amountIn *big.Int
	var outDec int
	sizeUSD := o.SizeUSD
	if o.Side == venue.SideBuy {
		if o.ReduceOnly {
			return venue.Fill{}, fmt.Errorf("monad: reduce-only buy on %s: spot holds no shorts", p.symbol)
		}
		tokenIn, tokenOut, outDec = v.quote, p.token, p.dec
		amountIn = toUnits(sizeUSD, v.quoteDec)
		bal, err := v.balanceOf(ctx, v.quote, v.address)
		if err != nil {
			return venue.Fill{}, fmt.Errorf("monad: balance %s: %w", v.cfg.Quote.Symbol, err)
		}
		if bal.Cmp(amountIn) < 0 {
			return venue.Fill{}, fmt.Errorf("monad: insufficient %s: have %.2f, need %.2f", v.cfg.Quote.Symbol, fromUnits(bal, v.quoteDec), sizeUSD)
		}
	} else {
		tokenIn, tokenOut, outDec = p.token, v.quote, v.quoteDec
		amountIn = toUnits(sizeUSD/price, p.dec)
		bal, err := v.balanceOf(ctx, p.token, v.address)
		if err != nil {
			return venue.Fill{}, fmt.Errorf("monad: balance %s: %w", p.tokSym, err)
		}
		if bal.Sign() == 0 {
			return venue.Fill{}, fmt.Errorf("monad: no %s balance to sell (spot venue cannot short)", p.tokSym)
		}
		if bal.Cmp(amountIn) < 0 {
			if !o.ReduceOnly {
				return venue.Fill{}, fmt.Errorf("monad: sell $%.2f of %s exceeds balance $%.2f (spot venue cannot short)", sizeUSD, p.symbol, fromUnits(bal, p.dec)*price)
			}
			amountIn = bal
			sizeUSD = fromUnits(bal, p.dec) * price
		}
	}
	if amountIn.Sign() <= 0 {
		return venue.Fill{}, fmt.Errorf("monad: order rounds to zero units")
	}

	expected, err := v.quoteExact(ctx, tokenIn, tokenOut, p.fee, amountIn)
	if err != nil {
		return venue.Fill{}, fmt.Errorf("monad: quote order: %w", err)
	}
	minOut := applySlippage(expected, v.cfg.SlippageBps)
	if o.PriceLimit != nil && *o.PriceLimit > 0 {
		var floor *big.Int
		if o.Side == venue.SideBuy {
			// Pay at most limit per base unit → receive at least size/limit.
			floor = toUnits(fromUnits(amountIn, v.quoteDec) / *o.PriceLimit, p.dec)
		} else {
			// Sell at least at limit → receive at least units × limit.
			floor = toUnits(fromUnits(amountIn, p.dec)**o.PriceLimit, v.quoteDec)
		}
		if floor.Cmp(expected) > 0 {
			return venue.Fill{}, fmt.Errorf("monad: quote %.6f outside price limit %.6f", impliedPrice(o.Side, amountIn, expected, p.dec, v.quoteDec), *o.PriceLimit)
		}
		if floor.Cmp(minOut) > 0 {
			minOut = floor
		}
	}

	v.sendMu.Lock()
	defer v.sendMu.Unlock()
	if err := v.ensureAllowance(ctx, tokenIn, amountIn); err != nil {
		return venue.Fill{}, err
	}
	data, err := packSwap(swapParams{
		TokenIn: tokenIn, TokenOut: tokenOut, Fee: big.NewInt(int64(p.fee)), Recipient: v.address,
		AmountIn: amountIn, AmountOutMinimum: minOut, SqrtPriceLimitX96: new(big.Int),
	})
	if err != nil {
		return venue.Fill{}, fmt.Errorf("monad: pack swap: %w", err)
	}
	rcpt, hash, err := v.transact(ctx, v.router, data)
	if err != nil {
		return venue.Fill{}, fmt.Errorf("monad: swap: %w", err)
	}
	got := transferredTo(rcpt, tokenOut, v.address)
	if got == nil {
		got = expected // no Transfer log parsed; report the quote
	}
	fill := venue.Fill{
		OrderID: o.ID,
		Market:  p.symbol,
		Side:    o.Side,
		TS:      v.now(),
		TxHash:  hash.Hex(),
	}
	if o.Side == venue.SideBuy {
		units := fromUnits(got, outDec)
		fill.SizeUSD = round2(fromUnits(amountIn, v.quoteDec))
		if units > 0 {
			fill.Price = fill.SizeUSD / units
		}
	} else {
		units := fromUnits(amountIn, p.dec)
		fill.SizeUSD = round2(fromUnits(got, outDec))
		if units > 0 {
			fill.Price = fill.SizeUSD / units
		}
	}
	return fill, nil
}

// Cancel implements venue.Venue: swaps settle atomically in one
// transaction, so nothing rests on a book to cancel.
func (v *Venue) Cancel(context.Context, string) error {
	return fmt.Errorf("%w: monad swaps settle atomically; nothing rests to cancel", venue.ErrNotImplemented)
}

// --- internals ---

func (v *Venue) pair(sym string) *pair {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.pairs[strings.ToUpper(sym)]
}

// price quotes p.probe base units into the quote token.
func (v *Venue) price(ctx context.Context, p *pair) (float64, error) {
	in := toUnits(p.probe, p.dec)
	out, err := v.quoteExact(ctx, p.token, v.quote, p.fee, in)
	if err != nil {
		return 0, err
	}
	px := fromUnits(out, v.quoteDec) / p.probe
	if px <= 0 {
		return 0, errors.New("zero quote (no liquidity?)")
	}
	return px, nil
}

func (v *Venue) quoteExact(ctx context.Context, in, out common.Address, fee uint32, amount *big.Int) (*big.Int, error) {
	data, err := packQuote(quoteParams{TokenIn: in, TokenOut: out, AmountIn: amount, Fee: big.NewInt(int64(fee)), SqrtPriceLimitX96: new(big.Int)})
	if err != nil {
		return nil, err
	}
	res, err := v.backend.CallContract(ctx, ethereum.CallMsg{To: &v.quoter, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return unpackQuote(res)
}

func (v *Venue) balanceOf(ctx context.Context, token, owner common.Address) (*big.Int, error) {
	data, err := erc20ABI.Pack("balanceOf", owner)
	if err != nil {
		return nil, err
	}
	res, err := v.backend.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return unpackUint(erc20ABI, "balanceOf", res)
}

// ensureAllowance approves exactly amount for the router when the current
// allowance is short (never an unlimited approval).
func (v *Venue) ensureAllowance(ctx context.Context, token common.Address, amount *big.Int) error {
	data, err := erc20ABI.Pack("allowance", v.address, v.router)
	if err != nil {
		return err
	}
	res, err := v.backend.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return fmt.Errorf("monad: allowance: %w", err)
	}
	cur, err := unpackUint(erc20ABI, "allowance", res)
	if err != nil {
		return fmt.Errorf("monad: allowance: %w", err)
	}
	if cur.Cmp(amount) >= 0 {
		return nil
	}
	approve, err := erc20ABI.Pack("approve", v.router, amount)
	if err != nil {
		return err
	}
	if _, _, err := v.transact(ctx, token, approve); err != nil {
		return fmt.Errorf("monad: approve: %w", err)
	}
	return nil
}

// transact builds, signs (EIP-1559) and sends a call to `to`, then waits for
// its receipt. A reverted receipt is an error. Callers hold sendMu.
func (v *Venue) transact(ctx context.Context, to common.Address, data []byte) (*types.Receipt, common.Hash, error) {
	nonce, err := v.backend.PendingNonceAt(ctx, v.address)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("nonce: %w", err)
	}
	tip, err := v.backend.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("tip: %w", err)
	}
	var feeCap *big.Int
	if h, err := v.backend.HeaderByNumber(ctx, nil); err == nil && h != nil && h.BaseFee != nil {
		feeCap = new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
	} else {
		gp, err := v.backend.SuggestGasPrice(ctx)
		if err != nil {
			return nil, common.Hash{}, fmt.Errorf("gas price: %w", err)
		}
		feeCap = new(big.Int).Add(new(big.Int).Mul(gp, big.NewInt(2)), tip)
	}
	gas, err := v.backend.EstimateGas(ctx, ethereum.CallMsg{From: v.address, To: &to, Data: data, GasTipCap: tip, GasFeeCap: feeCap})
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("estimate gas (would revert?): %w", err)
	}
	// Monad charges the gas limit, not gas used: keep the buffer small.
	bufBP := uint64(math.Round(v.cfg.GasBufferPct * 100))
	gas += (gas*bufBP + 9999) / 10000
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   v.chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: feeCap,
		Gas:       gas,
		To:        &to,
		Value:     new(big.Int),
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(v.chainID), v.key)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("sign: %w", err)
	}
	if err := v.backend.SendTransaction(ctx, signed); err != nil {
		return nil, signed.Hash(), fmt.Errorf("send: %w", err)
	}
	rcpt, err := v.waitReceipt(ctx, signed.Hash())
	if err != nil {
		return nil, signed.Hash(), err
	}
	if rcpt.Status != types.ReceiptStatusSuccessful {
		return rcpt, signed.Hash(), fmt.Errorf("tx %s reverted", signed.Hash().Hex())
	}
	return rcpt, signed.Hash(), nil
}

// waitReceipt polls for the receipt until ReceiptTimeout. Monad's RPC
// returns nothing for pending transactions, so NotFound means "keep waiting".
func (v *Venue) waitReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	wctx, cancel := context.WithTimeout(ctx, v.cfg.ReceiptTimeout)
	defer cancel()
	t := time.NewTicker(v.poll)
	defer t.Stop()
	for {
		// NotFound and transient RPC errors both mean "poll again" until
		// the deadline; the timeout error is what the caller records.
		if r, err := v.backend.TransactionReceipt(wctx, hash); err == nil && r != nil {
			return r, nil
		}
		select {
		case <-wctx.Done():
			return nil, fmt.Errorf("receipt for %s not seen within %s", hash.Hex(), v.cfg.ReceiptTimeout)
		case <-t.C:
		}
	}
}

// record stores a price sample (at most one per minute) and returns the
// 24h change once the venue has a sample at least 24h old, else 0.
func (v *Venue) record(p *pair, price float64) float64 {
	now := v.now()
	v.mu.Lock()
	defer v.mu.Unlock()
	if n := len(p.samples); n == 0 || now.Sub(p.samples[n-1].ts) >= time.Minute {
		p.samples = append(p.samples, sample{ts: now, price: price})
	}
	cut := now.Add(-25 * time.Hour)
	i := 0
	for i < len(p.samples)-1 && p.samples[i].ts.Before(cut) {
		i++
	}
	p.samples = p.samples[i:]
	var ref float64
	for _, s := range p.samples {
		if now.Sub(s.ts) >= 24*time.Hour {
			ref = s.price
		}
	}
	if ref <= 0 {
		return 0
	}
	return price/ref - 1
}

// transferredTo sums ERC-20 Transfer amounts of token to recipient in a receipt.
func transferredTo(r *types.Receipt, token, recipient common.Address) *big.Int {
	if r == nil {
		return nil
	}
	var sum *big.Int
	for _, l := range r.Logs {
		if l.Address != token || len(l.Topics) != 3 || l.Topics[0] != transferTopic {
			continue
		}
		if common.BytesToAddress(l.Topics[2].Bytes()) != recipient {
			continue
		}
		if sum == nil {
			sum = new(big.Int)
		}
		sum.Add(sum, new(big.Int).SetBytes(l.Data))
	}
	return sum
}

func applySlippage(x *big.Int, bps float64) *big.Int {
	keep := int64(math.Round(10000 - bps))
	if keep < 0 {
		keep = 0
	}
	out := new(big.Int).Mul(x, big.NewInt(keep))
	return out.Div(out, big.NewInt(10000))
}

func impliedPrice(side string, amountIn, amountOut *big.Int, baseDec, quoteDec int) float64 {
	if side == venue.SideBuy {
		base := fromUnits(amountOut, baseDec)
		if base == 0 {
			return 0
		}
		return fromUnits(amountIn, quoteDec) / base
	}
	base := fromUnits(amountIn, baseDec)
	if base == 0 {
		return 0
	}
	return fromUnits(amountOut, quoteDec) / base
}

// toUnits converts a decimal amount to integer token units (truncating).
func toUnits(x float64, dec int) *big.Int {
	if x <= 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return new(big.Int)
	}
	f := new(big.Float).SetPrec(256).SetFloat64(x)
	f.Mul(f, new(big.Float).SetPrec(256).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil)))
	out, _ := f.Int(nil)
	return out
}

// fromUnits converts integer token units to a float amount.
func fromUnits(x *big.Int, dec int) float64 {
	if x == nil {
		return 0
	}
	f := new(big.Float).SetPrec(256).SetInt(x)
	f.Quo(f, new(big.Float).SetPrec(256).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil)))
	out, _ := f.Float64()
	return out
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// Compile-time checks.
var (
	_ Backend     = (*ethclient.Client)(nil)
	_ venue.Venue = (*Venue)(nil)
)
