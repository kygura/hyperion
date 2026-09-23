package monad

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// rpcStub is a minimal Monad JSON-RPC node over httptest: enough of eth_*
// for ethclient, with ERC-20 balances, a price-model QuoterV2 and a
// SwapRouter02 that settles swaps into receipts with Transfer logs.
type rpcStub struct {
	t        *testing.T
	def      NetworkDefaults
	chainID  int64
	usd      map[common.Address]float64 // token → USD price
	dec      map[common.Address]int
	balances map[common.Address]*big.Int // token → owner balance (single owner)

	mu          sync.Mutex
	allowance   *big.Int
	fail        map[string]string // method → error message
	noReceipts  bool
	sent        []*types.Transaction
	receipts    map[common.Hash]*types.Receipt
	receiptPoll map[common.Hash]int
}

func newStub(t *testing.T) *rpcStub {
	d := Defaults(Mainnet)
	s := &rpcStub{
		t: t, def: d, chainID: 143,
		usd:         map[common.Address]float64{},
		dec:         map[common.Address]int{},
		balances:    map[common.Address]*big.Int{},
		allowance:   new(big.Int),
		fail:        map[string]string{},
		receipts:    map[common.Hash]*types.Receipt{},
		receiptPoll: map[common.Hash]int{},
	}
	usdc := common.HexToAddress(d.Quote.Address)
	s.usd[usdc], s.dec[usdc] = 1, 6
	for _, p := range d.Pairs {
		a := common.HexToAddress(p.Token.Address)
		s.dec[a] = p.Token.Decimals
		switch p.Symbol {
		case "MON":
			s.usd[a] = 0.025
		case "ETH":
			s.usd[a] = 2500
		}
	}
	return s
}

func (s *rpcStub) token(sym string) common.Address {
	if sym == "USDC" {
		return common.HexToAddress(s.def.Quote.Address)
	}
	for _, p := range s.def.Pairs {
		if p.Symbol == sym {
			return common.HexToAddress(p.Token.Address)
		}
	}
	s.t.Fatalf("unknown token %s", sym)
	return common.Address{}
}

func (s *rpcStub) setBalance(sym string, amount float64) {
	a := s.token(sym)
	s.balances[a] = toUnits(amount, s.dec[a])
}

func (s *rpcStub) quote(in, out common.Address, amt *big.Int) *big.Int {
	x := fromUnits(amt, s.dec[in]) * s.usd[in] / s.usd[out]
	return toUnits(x, s.dec[out])
}

type rpcReq struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func (s *rpcStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req rpcReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reply := func(result any, errMsg string) {
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if errMsg != "" {
			resp["error"] = map[string]any{"code": -32000, "message": errMsg}
		} else {
			resp["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
	if msg, ok := s.fail[req.Method]; ok {
		reply(nil, msg)
		return
	}
	switch req.Method {
	case "eth_chainId":
		reply(hexutil.EncodeBig(big.NewInt(s.chainID)), "")
	case "eth_blockNumber":
		reply("0x2a", "")
	case "eth_getBalance":
		reply(hexutil.EncodeBig(toUnits(1.5, 18)), "")
	case "eth_getTransactionCount":
		reply(hexutil.EncodeUint64(uint64(7+len(s.sent))), "")
	case "eth_maxPriorityFeePerGas":
		reply(hexutil.EncodeBig(big.NewInt(2e9)), "")
	case "eth_gasPrice":
		reply(hexutil.EncodeBig(big.NewInt(102e9)), "")
	case "eth_estimateGas":
		reply("0x30d40", "")
	case "eth_getBlockByNumber":
		h := &types.Header{Number: big.NewInt(42), Difficulty: new(big.Int), BaseFee: big.NewInt(100e9), GasLimit: 200_000_000, Time: 1}
		reply(h, "")
	case "eth_call":
		out, err := s.call(req.Params[0])
		if err != nil {
			reply(nil, err.Error())
			return
		}
		reply(hexutil.Encode(out), "")
	case "eth_sendRawTransaction":
		var raw hexutil.Bytes
		_ = json.Unmarshal(req.Params[0], &raw)
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(raw); err != nil {
			reply(nil, "bad tx: "+err.Error())
			return
		}
		s.sent = append(s.sent, tx)
		s.receipts[tx.Hash()] = s.settle(tx)
		reply(tx.Hash().Hex(), "")
	case "eth_getTransactionReceipt":
		var h common.Hash
		_ = json.Unmarshal(req.Params[0], &h)
		s.receiptPoll[h]++
		rc := s.receipts[h]
		// First poll returns null (Monad has no pending lookups), then the receipt.
		if s.noReceipts || rc == nil || s.receiptPoll[h] < 2 {
			reply(nil, "")
			return
		}
		reply(rc, "")
	default:
		reply(nil, "method not stubbed: "+req.Method)
	}
}

func (s *rpcStub) call(raw json.RawMessage) ([]byte, error) {
	var arg struct {
		To    common.Address `json:"to"`
		Input hexutil.Bytes  `json:"input"`
		Data  hexutil.Bytes  `json:"data"`
	}
	if err := json.Unmarshal(raw, &arg); err != nil {
		return nil, err
	}
	in := arg.Input
	if len(in) == 0 {
		in = arg.Data
	}
	if len(in) < 4 {
		return nil, errors.New("short calldata")
	}
	sel, body := in[:4], in[4:]
	word := func(i int) []byte { return body[i*32 : (i+1)*32] }
	switch {
	case arg.To == common.HexToAddress(s.def.Quoter):
		if !bytesEq(sel, quoterABI.Methods["quoteExactInputSingle"].ID) {
			return nil, errors.New("unknown quoter selector")
		}
		tin, tout := common.BytesToAddress(word(0)), common.BytesToAddress(word(1))
		amt := new(big.Int).SetBytes(word(2))
		out := s.quote(tin, tout, amt)
		return quoterABI.Methods["quoteExactInputSingle"].Outputs.Pack(out, new(big.Int), uint32(1), big.NewInt(90000))
	case bytesEq(sel, erc20ABI.Methods["balanceOf"].ID):
		b := s.balances[arg.To]
		if b == nil {
			b = new(big.Int)
		}
		return erc20ABI.Methods["balanceOf"].Outputs.Pack(b)
	case bytesEq(sel, erc20ABI.Methods["allowance"].ID):
		return erc20ABI.Methods["allowance"].Outputs.Pack(s.allowance)
	}
	return nil, errors.New("execution reverted")
}

// settle applies a sent tx: approve sets the allowance; exactInputSingle
// swaps at the model price, reverting below amountOutMinimum.
func (s *rpcStub) settle(tx *types.Transaction) *types.Receipt {
	rc := &types.Receipt{Type: types.DynamicFeeTxType, Status: types.ReceiptStatusSuccessful, TxHash: tx.Hash(), GasUsed: 150000, CumulativeGasUsed: 150000, BlockNumber: big.NewInt(43), Logs: []*types.Log{}}
	data := tx.Data()
	sel, body := data[:4], data[4:]
	word := func(i int) []byte { return body[i*32 : (i+1)*32] }
	switch {
	case bytesEq(sel, erc20ABI.Methods["approve"].ID):
		s.allowance = new(big.Int).SetBytes(word(1))
	case *tx.To() == common.HexToAddress(s.def.Router) && bytesEq(sel, routerABI.Methods["exactInputSingle"].ID):
		tin, tout := common.BytesToAddress(word(0)), common.BytesToAddress(word(1))
		recipient := common.BytesToAddress(word(3))
		amtIn, minOut := new(big.Int).SetBytes(word(4)), new(big.Int).SetBytes(word(5))
		out := s.quote(tin, tout, amtIn)
		if out.Cmp(minOut) < 0 || s.allowance.Cmp(amtIn) < 0 {
			rc.Status = types.ReceiptStatusFailed
			return rc
		}
		rc.Logs = append(rc.Logs, &types.Log{
			Address: tout,
			Topics:  []common.Hash{transferTopic, common.BytesToHash(common.HexToAddress(s.def.Router).Bytes()), common.BytesToHash(recipient.Bytes())},
			Data:    common.LeftPadBytes(out.Bytes(), 32),
			TxHash:  tx.Hash(), BlockNumber: 43,
		})
	}
	rc.Bloom = types.CreateBloom(rc)
	return rc
}

func bytesEq(a, b []byte) bool { return hex.EncodeToString(a) == hex.EncodeToString(b) }

func testKey(t *testing.T) (string, common.Address) {
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(crypto.FromECDSA(k)), crypto.PubkeyToAddress(k.PublicKey)
}

func newVenue(t *testing.T, s *rpcStub, key string, opts ...Option) *Venue {
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithPollInterval(5 * time.Millisecond)}, opts...)
	v, err := New(Config{RPCURL: srv.URL, PrivateKeyHex: key, ReceiptTimeout: 2 * time.Second}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStatusConnected(t *testing.T) {
	s := newStub(t)
	s.setBalance("MON", 100)
	key, addr := testKey(t)
	v := newVenue(t, s, key)
	st, err := v.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != "monad" || st.Kind != "evm" || st.Chain != "monad" || st.Status != "connected" || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	if strings.Join(st.Capabilities, ",") != "spot,execute" {
		t.Errorf("capabilities = %v", st.Capabilities)
	}
	if st.Meta["head_block"] != uint64(42) || st.Meta["chain_id"] != int64(143) || st.Meta["native_balance"] != 1.5 || st.Meta["address"] != addr.Hex() {
		t.Errorf("meta = %+v", st.Meta)
	}
	if len(st.Positions) != 1 || st.Positions[0].Market != "MON" || st.Positions[0].SizeUSD != 2.5 || st.Positions[0].Mark != 0.025 {
		t.Errorf("positions = %+v", st.Positions)
	}
}

func TestStatusReadOnlyHasNoExecute(t *testing.T) {
	v := newVenue(t, newStub(t), "")
	st, _ := v.Status(context.Background())
	if st.Status != "connected" || strings.Join(st.Capabilities, ",") != "spot" || len(st.Positions) != 0 {
		t.Fatalf("status = %+v", st)
	}
	if _, ok := st.Meta["address"]; ok {
		t.Error("read-only venue should not report an address")
	}
}

func TestStatusChainMismatchDegraded(t *testing.T) {
	s := newStub(t)
	s.chainID = 10143
	v := newVenue(t, s, "")
	st, _ := v.Status(context.Background())
	if st.Status != "degraded" || !strings.Contains(st.Error, "does not match") {
		t.Fatalf("status = %+v", st)
	}
}

func TestStatusRPCFailureDegraded(t *testing.T) {
	s := newStub(t)
	s.setBalance("MON", 1)
	s.fail["eth_call"] = "upstream timeout"
	key, _ := testKey(t)
	v := newVenue(t, s, key)
	st, _ := v.Status(context.Background())
	if st.Status != "degraded" || !strings.Contains(st.Error, "upstream timeout") {
		t.Fatalf("status = %+v", st)
	}
	if st.Meta["head_block"] != uint64(42) {
		t.Errorf("head block should still be reported: %+v", st.Meta)
	}
}

func TestStatusRPCDownDisconnected(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	v, err := New(Config{RPCURL: url})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := v.Status(context.Background())
	if st.Status != "disconnected" || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestMarkets(t *testing.T) {
	v := newVenue(t, newStub(t), "")
	ms, err := v.Markets(context.Background(), []string{"ETH", "MON", "DOGE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Symbol != "ETH" || ms[1].Symbol != "MON" {
		t.Fatalf("markets = %+v", ms)
	}
	if ms[0].Mark != 2500 || ms[1].Mark != 0.025 || ms[1].Mid != ms[1].Mark || ms[0].Funding != 0 {
		t.Errorf("prices = %+v", ms)
	}
	all, _ := v.Markets(context.Background(), nil)
	if len(all) != 2 {
		t.Errorf("all markets = %+v", all)
	}
}

func TestMarketsDayChangeFromSamples(t *testing.T) {
	s := newStub(t)
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	v := newVenue(t, s, "", WithClock(func() time.Time { return now }))
	if ms, _ := v.Markets(context.Background(), []string{"MON"}); ms[0].DayChange != 0 {
		t.Fatalf("first sample day change = %v", ms[0].DayChange)
	}
	now = now.Add(24 * time.Hour)
	s.usd[s.token("MON")] = 0.0275
	ms, _ := v.Markets(context.Background(), []string{"MON"})
	if d := ms[0].DayChange; d < 0.0999 || d > 0.1001 {
		t.Fatalf("day change = %v, want 0.10", d)
	}
}

func TestMarketsQuoteFailure(t *testing.T) {
	s := newStub(t)
	s.fail["eth_call"] = "boom"
	v := newVenue(t, s, "")
	if _, err := v.Markets(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlaceBuySignsApprovesAndSwaps(t *testing.T) {
	s := newStub(t)
	s.setBalance("USDC", 1000)
	key, addr := testKey(t)
	v := newVenue(t, s, key)
	fill, err := v.Place(context.Background(), venue.Order{ID: "i-1", Market: "MON", Side: venue.SideBuy, SizeUSD: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 2 {
		t.Fatalf("sent %d txs, want approve + swap", len(s.sent))
	}
	signer := types.LatestSignerForChainID(big.NewInt(143))
	for i, tx := range s.sent {
		from, err := types.Sender(signer, tx)
		if err != nil || from != addr {
			t.Fatalf("tx %d sender = %s, %v", i, from, err)
		}
		if tx.Type() != types.DynamicFeeTxType || tx.ChainId().Int64() != 143 {
			t.Errorf("tx %d type=%d chain=%s", i, tx.Type(), tx.ChainId())
		}
		// fee cap = 2 × base (100 gwei) + tip (2 gwei); gas = 200000 × 1.15
		if tx.GasFeeCap().Cmp(big.NewInt(202e9)) != 0 || tx.GasTipCap().Cmp(big.NewInt(2e9)) != 0 || tx.Gas() != 230000 {
			t.Errorf("tx %d fees cap=%s tip=%s gas=%d", i, tx.GasFeeCap(), tx.GasTipCap(), tx.Gas())
		}
	}
	approve, swap := s.sent[0], s.sent[1]
	if *approve.To() != s.token("USDC") || new(big.Int).SetBytes(approve.Data()[36:68]).Cmp(toUnits(100, 6)) != 0 {
		t.Errorf("approve should be exact 100 USDC to the router")
	}
	if *swap.To() != common.HexToAddress(s.def.Router) {
		t.Fatalf("swap to %s", swap.To())
	}
	body := swap.Data()[4:]
	minOut := new(big.Int).SetBytes(body[5*32 : 6*32])
	// 100 USDC / 0.025 = 4000 MON; 50 bps slippage → 3980 MON minimum.
	if got := fromUnits(minOut, 18); got < 3979.99 || got > 3980.01 {
		t.Errorf("amountOutMinimum = %v MON, want 3980", got)
	}
	if fill.TxHash != swap.Hash().Hex() || fill.Market != "MON" || fill.Side != "buy" || fill.SizeUSD != 100 {
		t.Errorf("fill = %+v", fill)
	}
	if fill.Price < 0.02499 || fill.Price > 0.02501 {
		t.Errorf("fill price = %v", fill.Price)
	}
}

func TestPlaceSkipsApproveWhenAllowanceCovers(t *testing.T) {
	s := newStub(t)
	s.setBalance("MON", 10000)
	s.allowance = toUnits(1e9, 18)
	key, _ := testKey(t)
	v := newVenue(t, s, key)
	fill, err := v.Place(context.Background(), venue.Order{Market: "MON", Side: venue.SideSell, SizeUSD: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 1 {
		t.Fatalf("sent %d txs, want swap only", len(s.sent))
	}
	if fill.SizeUSD != 50 || fill.Side != "sell" {
		t.Errorf("fill = %+v", fill)
	}
}

func TestPlaceNotionalCapRejects(t *testing.T) {
	s := newStub(t)
	s.setBalance("USDC", 10000)
	key, _ := testKey(t)
	v := newVenue(t, s, key, WithNotionalCap(func() float64 { return 100 }))
	_, err := v.Place(context.Background(), venue.Order{Market: "MON", Side: venue.SideBuy, SizeUSD: 150})
	if err == nil || !strings.Contains(err.Error(), "max_notional_usd") {
		t.Fatalf("err = %v", err)
	}
	if len(s.sent) != 0 {
		t.Fatalf("a capped order must not send a tx (sent %d)", len(s.sent))
	}
	// The venue's own cap applies when it is tighter than the governor's.
	v.cfg.MaxNotionalUSD = 40
	if c := v.NotionalCap(); c != 40 {
		t.Errorf("NotionalCap = %v, want 40", c)
	}
}

func TestPlaceSpotCannotShort(t *testing.T) {
	s := newStub(t)
	key, _ := testKey(t)
	v := newVenue(t, s, key)
	if _, err := v.Place(context.Background(), venue.Order{Market: "ETH", Side: venue.SideSell, SizeUSD: 100}); err == nil || !strings.Contains(err.Error(), "cannot short") {
		t.Fatalf("err = %v", err)
	}
	s.setBalance("ETH", 0.01) // $25
	if _, err := v.Place(context.Background(), venue.Order{Market: "ETH", Side: venue.SideSell, SizeUSD: 100}); err == nil || !strings.Contains(err.Error(), "exceeds balance") {
		t.Fatalf("err = %v", err)
	}
	// Reduce-only clips to the balance instead.
	s.allowance = toUnits(1, 18)
	fill, err := v.Place(context.Background(), venue.Order{Market: "ETH", Side: venue.SideSell, SizeUSD: 100, ReduceOnly: true})
	if err != nil || fill.SizeUSD != 25 {
		t.Fatalf("reduce-only fill = %+v, %v", fill, err)
	}
}

func TestPlacePriceLimitOutside(t *testing.T) {
	s := newStub(t)
	s.setBalance("USDC", 1000)
	key, _ := testKey(t)
	v := newVenue(t, s, key)
	limit := 0.02
	if _, err := v.Place(context.Background(), venue.Order{Market: "MON", Side: venue.SideBuy, SizeUSD: 10, PriceLimit: &limit}); err == nil || !strings.Contains(err.Error(), "price limit") {
		t.Fatalf("err = %v", err)
	}
	if len(s.sent) != 0 {
		t.Fatalf("sent %d txs", len(s.sent))
	}
}

func TestPlaceReceiptTimeout(t *testing.T) {
	s := newStub(t)
	s.setBalance("USDC", 1000)
	s.allowance = toUnits(1e6, 6)
	s.noReceipts = true
	key, _ := testKey(t)
	srv := httptest.NewServer(s)
	defer srv.Close()
	v, _ := New(Config{RPCURL: srv.URL, PrivateKeyHex: key, ReceiptTimeout: 50 * time.Millisecond}, WithPollInterval(5*time.Millisecond))
	if _, err := v.Place(context.Background(), venue.Order{Market: "MON", Side: venue.SideBuy, SizeUSD: 10}); err == nil || !strings.Contains(err.Error(), "not seen within") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlaceWithoutSignerNotImplemented(t *testing.T) {
	v := newVenue(t, newStub(t), "")
	if _, err := v.Place(context.Background(), venue.Order{Market: "MON", Side: venue.SideBuy, SizeUSD: 10}); !errors.Is(err, venue.ErrNotImplemented) {
		t.Fatalf("err = %v", err)
	}
	if err := v.Cancel(context.Background(), "x"); !errors.Is(err, venue.ErrNotImplemented) {
		t.Fatalf("cancel err = %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{Network: "devnet"}); err == nil {
		t.Error("unknown network accepted")
	}
	if _, err := New(Config{Router: "nope"}); err == nil {
		t.Error("bad router address accepted")
	}
	if _, err := New(Config{PrivateKeyHex: "zz"}); err == nil || strings.Contains(err.Error(), "zz") {
		t.Errorf("bad key: err = %v (must not echo the key)", err)
	}
	v, err := New(Config{Network: Testnet})
	if err != nil || v.cfg.ChainID != 10143 || v.cfg.RPCURL != "https://testnet-rpc.monad.xyz" {
		t.Errorf("testnet defaults: %+v, %v", v.cfg, err)
	}
}
