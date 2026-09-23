// Package venue is the chain-agnostic execution contract strategies see:
// markets by symbol, sizes in USD. Adapters (hyperliquid, paper) translate to
// their own units; a strategy never touches asset ids, size decimals or
// order books.
package venue

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented is returned by adapters for operations they do not
// support in this scaffold (e.g. hyperliquid Place without an executor).
var ErrNotImplemented = errors.New("venue: not implemented")

// Order sides.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

// Market is one tradable perp's compact context. All prices are USD; symbol
// is the bare coin ("ETH"). Funding is the hourly rate as a fraction; Premium
// is (mark - oracle) / oracle; DayChange is the 24h return as a fraction.
type Market struct {
	Symbol          string  `json:"symbol"`
	Mark            float64 `json:"mark"`
	Mid             float64 `json:"mid"`
	Funding         float64 `json:"funding"`
	Premium         float64 `json:"premium"`
	OpenInterestUSD float64 `json:"open_interest_usd"`
	DayVolumeUSD    float64 `json:"day_volume_usd"`
	DayChange       float64 `json:"day_change"`
}

// Position is one open position in USD terms; SizeUSD is signed (negative =
// short) and measured at the current mark. Byte-for-byte PROTOCOL.md's
// VenueStatus.positions element.
type Position struct {
	Market  string  `json:"market"`
	SizeUSD float64 `json:"size_usd"`
	Entry   float64 `json:"entry"`
	Mark    float64 `json:"mark"`
	UPnLUSD float64 `json:"upnl_usd"`
}

// VenueStatus is PROTOCOL.md's VenueStatus.
type VenueStatus struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Chain        string     `json:"chain"`
	Status       string     `json:"status"` // connected | degraded | disconnected
	Capabilities []string   `json:"capabilities"`
	Positions    []Position `json:"positions"`
	Error        string     `json:"error,omitempty"`
}

// Order is one execution request in USD notional. ReduceOnly closes or
// trims an existing position without ever flipping it. PriceLimit nil means
// take the mark (market/IOC).
type Order struct {
	ID         string   `json:"id"`
	Market     string   `json:"market"`
	Side       string   `json:"side"`
	SizeUSD    float64  `json:"size_usd"`
	ReduceOnly bool     `json:"reduce_only"`
	PriceLimit *float64 `json:"price_limit"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
}

// Fill is the venue's acknowledgement of an Order.
type Fill struct {
	OrderID string    `json:"order_id"`
	Market  string    `json:"market"`
	Side    string    `json:"side"`
	SizeUSD float64   `json:"size_usd"`
	Price   float64   `json:"price"`
	FeeUSD  float64   `json:"fee_usd"`
	TS      time.Time `json:"ts"`
}

// Venue is the adapter contract.
type Venue interface {
	ID() string
	Status(ctx context.Context) (VenueStatus, error)
	// Markets returns context for the given symbols (all known symbols when
	// symbols is empty). Unknown symbols are omitted, not an error.
	Markets(ctx context.Context, symbols []string) ([]Market, error)
	Positions(ctx context.Context) ([]Position, error)
	Place(ctx context.Context, o Order) (Fill, error)
	Cancel(ctx context.Context, id string) error
	Capabilities() []string
}

// PositionFor finds a symbol's position in a list; zero value when flat.
func PositionFor(positions []Position, market string) Position {
	for _, p := range positions {
		if p.Market == market {
			return p
		}
	}
	return Position{Market: market}
}
