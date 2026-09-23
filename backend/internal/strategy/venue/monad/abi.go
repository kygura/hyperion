package monad

import (
	"bytes"
	_ "embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ABI fragments for the chosen protocol (docs/jev/RESEARCH-monad.md §3):
// ERC-20, Uniswap v3 QuoterV2 and SwapRouter02. Only the functions the
// adapter calls are included; the full contracts are immutable upstream.
var (
	//go:embed abi/erc20.json
	erc20JSON []byte
	//go:embed abi/quoterv2.json
	quoterJSON []byte
	//go:embed abi/swaprouter02.json
	routerJSON []byte

	erc20ABI  = mustABI(erc20JSON)
	quoterABI = mustABI(quoterJSON)
	routerABI = mustABI(routerJSON)

	// transferTopic is keccak256("Transfer(address,address,uint256)").
	transferTopic = erc20ABI.Events["Transfer"].ID
)

func mustABI(b []byte) abi.ABI {
	a, err := abi.JSON(bytes.NewReader(b))
	if err != nil {
		panic(fmt.Sprintf("monad: embedded ABI: %v", err))
	}
	return a
}

// quoteParams is QuoterV2's QuoteExactInputSingleParams tuple.
type quoteParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	AmountIn          *big.Int
	Fee               *big.Int
	SqrtPriceLimitX96 *big.Int
}

// swapParams is SwapRouter02's (IV3SwapRouter) ExactInputSingleParams tuple.
// SwapRouter02 has no deadline field; the receipt timeout bounds the wait.
type swapParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Fee               *big.Int
	Recipient         common.Address
	AmountIn          *big.Int
	AmountOutMinimum  *big.Int
	SqrtPriceLimitX96 *big.Int
}

func packQuote(p quoteParams) ([]byte, error) {
	return quoterABI.Pack("quoteExactInputSingle", p)
}

func unpackQuote(out []byte) (*big.Int, error) {
	vals, err := quoterABI.Unpack("quoteExactInputSingle", out)
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("empty quote")
	}
	amt, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("quote: unexpected type %T", vals[0])
	}
	return amt, nil
}

func packSwap(p swapParams) ([]byte, error) {
	return routerABI.Pack("exactInputSingle", p)
}

func unpackUint(a abi.ABI, method string, out []byte) (*big.Int, error) {
	vals, err := a.Unpack(method, out)
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("%s: empty result", method)
	}
	v, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected type %T", method, vals[0])
	}
	return v, nil
}
