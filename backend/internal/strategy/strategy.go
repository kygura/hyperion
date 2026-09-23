// Package strategy is the plugin contract for the Jev-driven strategy runtime
// (docs/jev/SPEC.md) and the Go side of the wire types in
// docs/jev/PROTOCOL.md. JSON tags here are the protocol: the terminal and web
// clients mirror them byte for byte.
//
// A Strategy is three things: a static Manifest (params schema, the fixed
// question map, supported venues, cadence), Snapshot (venue → compact State
// the decider reads), and Decide (pure: answers → intents, thresholds from
// Params). Everything else — the decider call, the governor, execution,
// journaling — is the runtime's.
package strategy

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// Strategy is the plugin contract (SPEC.md "Plugin contract").
type Strategy interface {
	Manifest() Manifest
	Snapshot(ctx context.Context, v venue.Venue, p Params) (State, error)
	Decide(state State, a Answers, p Params) []Intent
}

// State is the compact, JSON-serializable snapshot sent to the decider:
// numbers and short labels, never raw order books (32k-token window).
type State = any

// Answers is the decider's output keyed like Manifest.Questions.
type Answers = map[string]decider.Answer

// Param types.
const (
	ParamNumber = "number"
	ParamString = "string"
	ParamBool   = "bool"
	ParamEnum   = "enum"
)

// ParamSpec is one operator-editable parameter (PROTOCOL.md "ParamSpec").
type ParamSpec struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Label       string   `json:"label,omitempty"`
	Default     any      `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Manifest is the static description of a strategy (PROTOCOL.md "Manifest").
type Manifest struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Version     string                      `json:"version"`
	Description string                      `json:"description"`
	Venues      []string                    `json:"venues"`
	Cadence     string                      `json:"cadence"`
	Markets     []string                    `json:"markets"`
	Params      []ParamSpec                 `json:"params"`
	Questions   map[string]decider.Question `json:"questions"`
}

// CadenceDuration parses Cadence ("5m", "1h", "30s"); 5 minutes when blank
// or malformed.
func (m Manifest) CadenceDuration() time.Duration {
	if d, err := time.ParseDuration(m.Cadence); err == nil && d > 0 {
		return d
	}
	return 5 * time.Minute
}

// SupportsVenue reports whether the manifest lists the venue id.
func (m Manifest) SupportsVenue(id string) bool {
	for _, v := range m.Venues {
		if v == id {
			return true
		}
	}
	return false
}

// Spec returns the ParamSpec for key.
func (m Manifest) Spec(key string) (ParamSpec, bool) {
	for _, p := range m.Params {
		if p.Key == key {
			return p, true
		}
	}
	return ParamSpec{}, false
}

// Params is the operator-set parameter map. Values arrive from TOML (int64 /
// float64 / bool / string / []any) or JSON (float64 / bool / string / []any),
// so the typed getters normalise both.
type Params map[string]any

// Float reads a numeric param; def when absent or not numeric.
func (p Params) Float(key string, def float64) float64 {
	if v, ok := toFloat(p[key]); ok {
		return v
	}
	return def
}

// Int reads a numeric param as int.
func (p Params) Int(key string, def int) int {
	if v, ok := toFloat(p[key]); ok {
		return int(v)
	}
	return def
}

// Bool reads a bool param.
func (p Params) Bool(key string, def bool) bool {
	if v, ok := p[key].(bool); ok {
		return v
	}
	return def
}

// String reads a string param.
func (p Params) String(key string, def string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return def
}

// Strings reads a list-of-strings param ([]string, []any of strings, or a
// comma-separated string).
func (p Params) Strings(key string, def []string) []string {
	switch v := p[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return def
		}
		var out []string
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return def
}

// Markets returns the effective universe: params["markets"] when set, else
// the manifest default.
func (p Params) Markets(m Manifest) []string {
	return p.Strings("markets", m.Markets)
}

// Clone returns a shallow copy.
func (p Params) Clone() Params {
	out := make(Params, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

// Defaults returns the manifest's default values as Params.
func Defaults(m Manifest) Params {
	out := make(Params, len(m.Params))
	for _, s := range m.Params {
		if s.Default != nil {
			out[s.Key] = s.Default
		}
	}
	return out
}

// Merge overlays p on the manifest defaults: every declared param has a value.
func Merge(m Manifest, p Params) Params {
	out := Defaults(m)
	for k, v := range p {
		out[k] = v
	}
	return out
}

// ValidationError names the offending field for the 400 envelope
// {error, field} (e.g. field = "params.size_usd").
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Msg }

// Validate checks p against the manifest: known keys only (plus the
// runtime's "markets" override), correct type, within min/max, enum member.
func (p Params) Validate(m Manifest) error {
	for key, val := range p {
		if key == "markets" {
			if list := p.Strings("markets", nil); len(list) == 0 {
				return &ValidationError{Field: "params.markets", Msg: "markets must be a non-empty list of symbols"}
			}
			continue
		}
		spec, ok := m.Spec(key)
		field := "params." + key
		if !ok {
			return &ValidationError{Field: field, Msg: fmt.Sprintf("unknown param %q", key)}
		}
		switch spec.Type {
		case ParamNumber:
			f, ok := toFloat(val)
			if !ok {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be a number", key)}
			}
			if spec.Min != nil && f < *spec.Min {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be >= %v", key, *spec.Min)}
			}
			if spec.Max != nil && f > *spec.Max {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be <= %v", key, *spec.Max)}
			}
		case ParamBool:
			if _, ok := val.(bool); !ok {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be a bool", key)}
			}
		case ParamString:
			if _, ok := val.(string); !ok {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be a string", key)}
			}
		case ParamEnum:
			s, ok := val.(string)
			if !ok {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be one of %v", key, spec.Options)}
			}
			found := false
			for _, o := range spec.Options {
				if o == s {
					found = true
					break
				}
			}
			if !found {
				return &ValidationError{Field: field, Msg: fmt.Sprintf("%s must be one of %v", key, spec.Options)}
			}
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n)
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// Intent actions.
const (
	ActionOpenLong  = "open_long"
	ActionOpenShort = "open_short"
	ActionClose     = "close"
	ActionScale     = "scale"
	ActionRebalance = "rebalance"
	ActionHold      = "hold"
)

// Intent is one proposed action (PROTOCOL.md "Intent"). Side is an additive
// hint for rebalance/scale intents ("buy" | "sell"), omitted otherwise;
// clients ignore it.
type Intent struct {
	ID           string   `json:"id"`
	StrategyID   string   `json:"strategy_id"`
	Venue        string   `json:"venue"`
	Market       string   `json:"market"`
	Action       string   `json:"action"`
	SizeUSD      float64  `json:"size_usd"`
	TargetWeight *float64 `json:"target_weight"`
	PriceLimit   *float64 `json:"price_limit"`
	Reason       string   `json:"reason"`
	Confidence   float64  `json:"confidence"`
	Side         string   `json:"side,omitempty"`
}

// Verdict statuses and authors.
const (
	StatusProposed = "proposed"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusExecuted = "executed"
	StatusGated    = "gated"
	StatusFailed   = "failed"

	ByGovernor = "governor"
	ByOperator = "operator"
	ByGate     = "gate"
	ByVenue    = "venue"
)

// Verdict is one status transition of an intent (PROTOCOL.md "Verdict").
type Verdict struct {
	IntentID string    `json:"intent_id"`
	Status   string    `json:"status"`
	By       string    `json:"by"`
	Reason   string    `json:"reason"`
	TS       time.Time `json:"ts"`
}

// Terminal reports whether the status ends the intent's life.
func (v Verdict) Terminal() bool {
	switch v.Status {
	case StatusRejected, StatusExecuted, StatusGated, StatusFailed:
		return true
	}
	return false
}

// DecisionRecord is one strategy tick (PROTOCOL.md "DecisionRecord").
type DecisionRecord struct {
	ID          string                      `json:"id"`
	TS          time.Time                   `json:"ts"`
	StrategyID  string                      `json:"strategy_id"`
	Venue       string                      `json:"venue"`
	DryRun      bool                        `json:"dry_run"`
	StateDigest string                      `json:"state_digest"`
	State       State                       `json:"state"`
	Questions   map[string]decider.Question `json:"questions"`
	Answers     Answers                     `json:"answers"`
	Intents     []Intent                    `json:"intents"`
	Verdicts    []Verdict                   `json:"verdicts"`
	Model       string                      `json:"model"`
	Usage       decider.Usage               `json:"usage"`
	LatencyMs   int64                       `json:"latency_ms"`
	Error       string                      `json:"error"`
}

// LatestVerdict returns the most recent verdict for an intent.
func (r DecisionRecord) LatestVerdict(intentID string) (Verdict, bool) {
	for i := len(r.Verdicts) - 1; i >= 0; i-- {
		if r.Verdicts[i].IntentID == intentID {
			return r.Verdicts[i], true
		}
	}
	return Verdict{}, false
}

// Intent returns the intent with the given id.
func (r DecisionRecord) Intent(id string) (Intent, bool) {
	for _, it := range r.Intents {
		if it.ID == id {
			return it, true
		}
	}
	return Intent{}, false
}

// Governor modes.
const (
	ModeManual    = "manual"
	ModeThreshold = "threshold"
	ModeAuto      = "auto"
)

// GovernorSettings is the global human-agency policy (PROTOCOL.md).
type GovernorSettings struct {
	Mode           string  `json:"mode" toml:"mode"`
	MinConfidence  float64 `json:"min_confidence" toml:"min_confidence"`
	MaxNotionalUSD float64 `json:"max_notional_usd" toml:"max_notional_usd"`
	MaxOpenIntents int     `json:"max_open_intents" toml:"max_open_intents"`
	Killed         bool    `json:"killed" toml:"-"`
}

// Validate checks the mode and limits.
func (g GovernorSettings) Validate() error {
	switch g.Mode {
	case ModeManual, ModeThreshold, ModeAuto:
	default:
		return &ValidationError{Field: "mode", Msg: fmt.Sprintf("mode must be manual|threshold|auto, got %q", g.Mode)}
	}
	if g.MinConfidence < 0 || g.MinConfidence > 1 {
		return &ValidationError{Field: "min_confidence", Msg: "min_confidence must be in [0,1]"}
	}
	if g.MaxNotionalUSD < 0 {
		return &ValidationError{Field: "max_notional_usd", Msg: "max_notional_usd must be >= 0"}
	}
	if g.MaxOpenIntents < 0 {
		return &ValidationError{Field: "max_open_intents", Msg: "max_open_intents must be >= 0"}
	}
	return nil
}

// GovernorOverride is the per-strategy override: same shape, every field
// optional (nil = inherit the global value).
type GovernorOverride struct {
	Mode           *string  `json:"mode,omitempty" toml:"mode"`
	MinConfidence  *float64 `json:"min_confidence,omitempty" toml:"min_confidence"`
	MaxNotionalUSD *float64 `json:"max_notional_usd,omitempty" toml:"max_notional_usd"`
	MaxOpenIntents *int     `json:"max_open_intents,omitempty" toml:"max_open_intents"`
}

// Apply returns the global settings with the override's set fields applied.
// Killed is never overridable.
func (o GovernorOverride) Apply(g GovernorSettings) GovernorSettings {
	if o.Mode != nil {
		g.Mode = *o.Mode
	}
	if o.MinConfidence != nil {
		g.MinConfidence = *o.MinConfidence
	}
	if o.MaxNotionalUSD != nil {
		g.MaxNotionalUSD = *o.MaxNotionalUSD
	}
	if o.MaxOpenIntents != nil {
		g.MaxOpenIntents = *o.MaxOpenIntents
	}
	return g
}

// Validate checks the override's set fields.
func (o GovernorOverride) Validate() error {
	if o.Mode != nil {
		switch *o.Mode {
		case ModeManual, ModeThreshold, ModeAuto:
		default:
			return &ValidationError{Field: "governor.mode", Msg: fmt.Sprintf("mode must be manual|threshold|auto, got %q", *o.Mode)}
		}
	}
	if o.MinConfidence != nil && (*o.MinConfidence < 0 || *o.MinConfidence > 1) {
		return &ValidationError{Field: "governor.min_confidence", Msg: "min_confidence must be in [0,1]"}
	}
	if o.MaxNotionalUSD != nil && *o.MaxNotionalUSD < 0 {
		return &ValidationError{Field: "governor.max_notional_usd", Msg: "max_notional_usd must be >= 0"}
	}
	if o.MaxOpenIntents != nil && *o.MaxOpenIntents < 0 {
		return &ValidationError{Field: "governor.max_open_intents", Msg: "max_open_intents must be >= 0"}
	}
	return nil
}

// StrategyConfig is the operator-editable per-strategy config (PROTOCOL.md).
// TOML tags let config.toml carry the same shape under [[strategy.configs]].
type StrategyConfig struct {
	ID       string           `json:"id" toml:"id"`
	Enabled  bool             `json:"enabled" toml:"enabled"`
	Venue    string           `json:"venue" toml:"venue"`
	Params   Params           `json:"params" toml:"params"`
	Governor GovernorOverride `json:"governor" toml:"governor"`
}

// Validate checks the config against its manifest and the known venues.
func (c StrategyConfig) Validate(m Manifest, venues []string) error {
	if c.ID != "" && c.ID != m.ID {
		return &ValidationError{Field: "id", Msg: fmt.Sprintf("id %q does not match strategy %q", c.ID, m.ID)}
	}
	if c.Venue == "" {
		return &ValidationError{Field: "venue", Msg: "venue is required"}
	}
	if !m.SupportsVenue(c.Venue) {
		return &ValidationError{Field: "venue", Msg: fmt.Sprintf("strategy %s does not support venue %q (supported: %s)", m.ID, c.Venue, strings.Join(m.Venues, ", "))}
	}
	known := false
	for _, v := range venues {
		if v == c.Venue {
			known = true
			break
		}
	}
	if !known {
		return &ValidationError{Field: "venue", Msg: fmt.Sprintf("venue %q is not configured", c.Venue)}
	}
	if err := c.Params.Validate(m); err != nil {
		return err
	}
	return c.Governor.Validate()
}

// StrategyStatus is Manifest + Config + runtime state (PROTOCOL.md).
// LastAction is a short summary of the newest decision's first intent
// ("open_short ETH"), "hold" when it had none, omitted when never run.
type StrategyStatus struct {
	Manifest       Manifest       `json:"manifest"`
	Config         StrategyConfig `json:"config"`
	LastRunAt      *time.Time     `json:"last_run_at"`
	LastDecisionID string         `json:"last_decision_id"`
	LastAction     string         `json:"last_action,omitempty"`
	LastError      string         `json:"last_error"`
	NextRunAt      *time.Time     `json:"next_run_at"`
}

// LastAction summarises a record for StrategyStatus.LastAction.
func (r DecisionRecord) LastAction() string {
	if len(r.Intents) == 0 {
		return "hold"
	}
	in := r.Intents[0]
	if in.Action == ActionHold {
		return "hold"
	}
	s := in.Action + " " + in.Market
	if len(r.Intents) > 1 {
		s += fmt.Sprintf(" +%d", len(r.Intents)-1)
	}
	return s
}

// F is a helper for pointer floats in specs and intents.
func F(v float64) *float64 { return &v }
