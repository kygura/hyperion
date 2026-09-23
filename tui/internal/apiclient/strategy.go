package apiclient

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// This file mirrors docs/jev/PROTOCOL.md ("strategy wire contract") field
// for field. Unknown fields are ignored (encoding/json default); every
// timestamp is a Time so a placeholder or empty value never fails a decode.

// Time is an RFC3339 timestamp that decodes leniently: null, "" and any
// unparseable string (e.g. a not-yet-set last_run_at) become the zero time
// instead of failing the whole record. It marshals as RFC3339 UTC.
type Time struct{ time.Time }

// UnmarshalJSON implements json.Unmarshaler.
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Time = time.Time{}
		return nil
	}
	t.Time = parsed
	return nil
}

// MarshalJSON implements json.Marshaler.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.UTC().Format(time.RFC3339Nano))
}

// Question types and answer/verdict/governor enums as PROTOCOL.md spells them.
const (
	QuestionChoice = "choice"
	QuestionScore  = "score"
	QuestionNoul   = "noul"

	GovernorManual    = "manual"
	GovernorThreshold = "threshold"
	GovernorAuto      = "auto"

	VerdictProposed = "proposed"
	VerdictApproved = "approved"
	VerdictRejected = "rejected"
	VerdictExecuted = "executed"
	VerdictGated    = "gated"
	VerdictFailed   = "failed"

	ParamNumber = "number"
	ParamString = "string"
	ParamBool   = "bool"
	ParamEnum   = "enum"
)

// GovernorModes is the ordered option set for the mode selector.
var GovernorModes = []string{GovernorManual, GovernorThreshold, GovernorAuto}

// Criteria is a Question's option set: a map (choice), an ordered list
// (score) or absent (noul). Both shapes decode into one value so a question
// map can be a single Go type.
type Criteria struct {
	Options map[string]string // choice: option → description
	Levels  []string          // score: ordered level texts
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *Criteria) UnmarshalJSON(b []byte) error {
	*c = Criteria{}
	trimmed := strings.TrimSpace(string(b))
	switch {
	case trimmed == "null", trimmed == "":
		return nil
	case strings.HasPrefix(trimmed, "["):
		return json.Unmarshal(b, &c.Levels)
	case strings.HasPrefix(trimmed, "{"):
		return json.Unmarshal(b, &c.Options)
	}
	return fmt.Errorf("criteria: unexpected JSON %s", trimmed)
}

// MarshalJSON implements json.Marshaler.
func (c Criteria) MarshalJSON() ([]byte, error) {
	switch {
	case c.Levels != nil:
		return json.Marshal(c.Levels)
	case c.Options != nil:
		return json.Marshal(c.Options)
	}
	return []byte("null"), nil
}

// Question mirrors the Jev question primitive.
type Question struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     Criteria `json:"criteria,omitempty"`
}

// LegendEntry is one score-legend value: PROTOCOL.md allows a plain string
// or an object, and clients render "what" when it is an object.
type LegendEntry struct {
	What string
	Raw  json.RawMessage
}

// UnmarshalJSON implements json.Unmarshaler.
func (l *LegendEntry) UnmarshalJSON(b []byte) error {
	l.Raw = append(json.RawMessage(nil), b...)
	var s string
	if json.Unmarshal(b, &s) == nil {
		l.What = s
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if w, ok := obj["what"].(string); ok {
		l.What = w
		return nil
	}
	for _, k := range []string{"label", "text", "name", "description"} {
		if v, ok := obj[k].(string); ok {
			l.What = v
			return nil
		}
	}
	l.What = string(b)
	return nil
}

// MarshalJSON implements json.Marshaler — round-trips the original value.
func (l LegendEntry) MarshalJSON() ([]byte, error) {
	if len(l.Raw) > 0 {
		return l.Raw, nil
	}
	return json.Marshal(l.What)
}

// String is the display form.
func (l LegendEntry) String() string { return l.What }

// Answer mirrors the Jev answer primitive. Confidence is nil when absent
// (noul answers carry none).
type Answer struct {
	Type          string                 `json:"type"`
	Choice        string                 `json:"choice,omitempty"`
	Score         float64                `json:"score,omitempty"`
	Noul          float64                `json:"noul,omitempty"`
	Probabilities map[string]float64     `json:"probabilities,omitempty"`
	Legend        map[string]LegendEntry `json:"legend,omitempty"`
	Confidence    *float64               `json:"confidence,omitempty"`
}

// ConfidenceValue returns the confidence and whether one was present.
func (a Answer) ConfidenceValue() (float64, bool) {
	if a.Confidence == nil {
		return 0, false
	}
	return *a.Confidence, true
}

// LegendText returns the legend label for a score level key ("0", "1", …),
// or "" when no legend covers it.
func (a Answer) LegendText(key string) string {
	if a.Legend == nil {
		return ""
	}
	return a.Legend[key].What
}

// ParamSpec mirrors one manifest parameter. Default is whatever JSON value
// the manifest carries (float64, string, bool). Min/Max/Step are nil when
// the manifest omits them.
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

// Manifest mirrors the static plugin manifest.
type Manifest struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description,omitempty"`
	Venues      []string            `json:"venues"`
	Cadence     string              `json:"cadence"`
	Markets     []string            `json:"markets,omitempty"`
	Params      []ParamSpec         `json:"params"`
	Questions   map[string]Question `json:"questions"`
}

// GovernorSettings mirrors the global governor settings.
type GovernorSettings struct {
	Mode           string  `json:"mode"`
	MinConfidence  float64 `json:"min_confidence"`
	MaxNotionalUSD float64 `json:"max_notional_usd"`
	MaxOpenIntents int     `json:"max_open_intents"`
	Killed         bool    `json:"killed"`
}

// GovernorOverride is the per-strategy governor override: same shape as
// GovernorSettings with every field optional.
type GovernorOverride struct {
	Mode           *string  `json:"mode,omitempty"`
	MinConfidence  *float64 `json:"min_confidence,omitempty"`
	MaxNotionalUSD *float64 `json:"max_notional_usd,omitempty"`
	MaxOpenIntents *int     `json:"max_open_intents,omitempty"`
	Killed         *bool    `json:"killed,omitempty"`
}

// StrategyConfig mirrors the operator-editable strategy config.
type StrategyConfig struct {
	ID       string            `json:"id"`
	Enabled  bool              `json:"enabled"`
	Venue    string            `json:"venue"`
	Params   map[string]any    `json:"params"`
	Governor *GovernorOverride `json:"governor,omitempty"`
}

// StrategyStatus mirrors Manifest + Config + runtime.
type StrategyStatus struct {
	Manifest       Manifest       `json:"manifest"`
	Config         StrategyConfig `json:"config"`
	LastRunAt      Time           `json:"last_run_at"`
	LastDecisionID string         `json:"last_decision_id"`
	LastError      string         `json:"last_error"`
	NextRunAt      Time           `json:"next_run_at"`
}

// Intent mirrors one strategy intent.
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
}

// IntentVerdict mirrors PROTOCOL.md's Verdict (one per intent, appended as
// status changes). Named IntentVerdict because Verdict is already taken by
// the legacy reasoner verdict type in this package.
type IntentVerdict struct {
	IntentID string `json:"intent_id"`
	Status   string `json:"status"`
	By       string `json:"by"`
	Reason   string `json:"reason"`
	TS       Time   `json:"ts"`
}

// Usage mirrors the decider token usage.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// DecisionRecord mirrors one strategy tick.
type DecisionRecord struct {
	ID          string              `json:"id"`
	TS          Time                `json:"ts"`
	StrategyID  string              `json:"strategy_id"`
	Venue       string              `json:"venue"`
	DryRun      bool                `json:"dry_run"`
	StateDigest string              `json:"state_digest"`
	State       json.RawMessage     `json:"state,omitempty"`
	Questions   map[string]Question `json:"questions"`
	Answers     map[string]Answer   `json:"answers"`
	Intents     []Intent            `json:"intents"`
	Verdicts    []IntentVerdict     `json:"verdicts"`
	Model       string              `json:"model"`
	Usage       Usage               `json:"usage"`
	LatencyMS   int                 `json:"latency_ms"`
	Error       string              `json:"error"`
}

// LatestVerdict returns the most recent verdict for intentID (verdicts are
// appended in order, so the last match wins) and whether one exists.
func (d DecisionRecord) LatestVerdict(intentID string) (IntentVerdict, bool) {
	var out IntentVerdict
	found := false
	for _, v := range d.Verdicts {
		if v.IntentID == intentID {
			out, found = v, true
		}
	}
	return out, found
}

// VenuePosition mirrors one venue position row.
type VenuePosition struct {
	Market  string  `json:"market"`
	SizeUSD float64 `json:"size_usd"`
	Entry   float64 `json:"entry"`
	Mark    float64 `json:"mark"`
	UPnLUSD float64 `json:"upnl_usd"`
}

// VenueStatus mirrors one venue's status.
type VenueStatus struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Chain        string          `json:"chain"`
	Status       string          `json:"status"`
	Capabilities []string        `json:"capabilities"`
	Positions    []VenuePosition `json:"positions"`
}

// APIError is the daemon's {error, field} body with its HTTP status. Its
// Error() is the bare message so callers matching on text keep working.
type APIError struct {
	Status  int
	Message string
	Field   string
}

func (e *APIError) Error() string { return e.Message }
