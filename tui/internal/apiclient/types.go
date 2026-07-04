// Package apiclient is the tui module's only dependency on backend/: a thin
// HTTP+WS client. No import of backend/internal/* — every wire type here is
// hand-derived from the JSON the daemon's API actually produces.
package apiclient

import "time"

// Bar mirrors backend/internal/metrics.Bar (untagged Go struct — wire fields
// are the exported Go names verbatim; Open/CloseTime marshal as RFC3339).
type Bar struct {
	Coin        string
	Timeframe   string
	OpenTime    time.Time
	CloseTime   time.Time
	Open, High, Low, Close float64
	Volume      float64
	TradeCount  int
	Final       bool
	Return      float64
	Funding     float64
	OIDelta     float64
	RelStrength float64
}

// AssetCtx mirrors backend/internal/metrics.AssetCtx.
type AssetCtx struct {
	Coin         string
	MarkPrice    float64
	OraclePrice  float64
	Funding      float64
	OpenInterest float64
	Premium      float64
	DayVolume    float64
	Time         time.Time
}

// Position mirrors backend/internal/metrics.Position.
type Position struct {
	Coin      string
	Size      float64
	MarkPrice float64
}

func (p Position) IsFlat() bool { return p.Size == 0 }

// MarketEntry mirrors the marketEntry JSON shape of GET /api/markets
// (backend/internal/api/read.go).
type MarketEntry struct {
	Coin     string   `json:"coin"`
	Bar      Bar      `json:"bar"`
	Mid      float64  `json:"mid"`
	AssetCtx AssetCtx `json:"asset_ctx"`
	Position Position `json:"position"`
}

// Verdict mirrors backend/internal/metrics.Verdict.
type Verdict struct {
	Asset                string
	Action               string
	Thesis               string
	Confidence           float64
	SizeUSD              float64
	Stop, TakeProfit     float64
	Entry                Entry
	RequiresConfirmation bool
}

// Entry mirrors backend/internal/metrics.Entry.
type Entry struct {
	Type  string
	Price float64
}

// ChatTurn mirrors backend/internal/reasoner.ChatTurn (json tags role/text).
type ChatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// SettingsResponse mirrors GET /api/settings's body.
type SettingsResponse struct {
	Mode           string              `json:"mode"`
	Batch          RoleSettings        `json:"batch"`
	Chat           RoleSettings        `json:"chat"`
	ProviderNames  []string            `json:"provider_names"`
	ProviderModels map[string][]string `json:"provider_models"`
	KeyHints       map[string]string   `json:"key_hints"`
	Visualized     []string            `json:"visualized"`
	Tracked        []string            `json:"tracked"`
	Timeframes     map[string]string   `json:"timeframes"`
	Risk           RiskSettings        `json:"risk"`
}

type RoleSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// RiskSettings mirrors backend/internal/api/settings.go's riskSettings.
type RiskSettings struct {
	MaxPositionUSD      float64 `json:"max_position_usd"`
	MaxTotalExposureUSD float64 `json:"max_total_exposure_usd"`
	MaxConcurrent       int     `json:"max_concurrent"`
	DailyLossKillUSD    float64 `json:"daily_loss_kill_usd"`
}
