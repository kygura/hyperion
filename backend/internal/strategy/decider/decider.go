// Package decider is the typed-question contract between a strategy and the
// model that answers it. A Decider takes a compact state plus a map of typed
// questions (choice / score / noul, per PROTOCOL.md "Jev primitives") and
// returns calibrated answers. The jev subpackage is the TypeSafe HTTP client;
// fake is the scripted stand-in for tests and keyless paper runs.
package decider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Question types. Jev evaluates every question independently in one request
// and can only answer from the option set the question carries.
const (
	TypeChoice = "choice"
	TypeScore  = "score"
	TypeNoul   = "noul"
)

// ErrUnavailable means the decider cannot serve at all (no API key, no
// endpoint configured). The API maps it to 503 {"error":"decider unavailable"}.
var ErrUnavailable = errors.New("decider unavailable")

// Question is one typed question, byte-for-byte the wire shape:
//
//	{ "type": "choice", "instructions": "...", "criteria": { "opt": "desc" } }
//	{ "type": "score",  "instructions": "...", "criteria": [ "level0", ... ] }
//	{ "type": "noul",   "instructions": "..." }
//
// Criteria is untyped because its shape depends on Type; use ChoiceOptions /
// ScoreLevels to read it regardless of whether it was built in Go or decoded
// from JSON.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Choice builds a choice question from option → description.
func Choice(instructions string, options map[string]string) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: options}
}

// Score builds a score question from an ordered list of level descriptions.
func Score(instructions string, levels []string) Question {
	return Question{Type: TypeScore, Instructions: instructions, Criteria: levels}
}

// Noul builds a noul (unit-interval likelihood) question.
func Noul(instructions string) Question {
	return Question{Type: TypeNoul, Instructions: instructions}
}

// ChoiceOptions returns the option → description map of a choice question,
// tolerating both the Go-built map[string]string and the JSON-decoded
// map[string]any form. Nil for other types.
func (q Question) ChoiceOptions() map[string]string {
	switch c := q.Criteria.(type) {
	case map[string]string:
		return c
	case map[string]any:
		out := make(map[string]string, len(c))
		for k, v := range c {
			out[k], _ = v.(string)
		}
		return out
	}
	return nil
}

// ChoiceKeys returns the choice options sorted, for deterministic iteration.
func (q Question) ChoiceKeys() []string {
	opts := q.ChoiceOptions()
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ScoreLevels returns the ordered level texts of a score question, tolerating
// both []string and the JSON-decoded []any form. Nil for other types.
func (q Question) ScoreLevels() []string {
	switch c := q.Criteria.(type) {
	case []string:
		return c
	case []any:
		out := make([]string, 0, len(c))
		for _, v := range c {
			s, _ := v.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}

// Validate checks the question is well-formed for its type.
func (q Question) Validate() error {
	if q.Instructions == "" {
		return errors.New("question: empty instructions")
	}
	switch q.Type {
	case TypeChoice:
		if len(q.ChoiceOptions()) < 2 {
			return errors.New("question: choice needs at least 2 options")
		}
	case TypeScore:
		if n := len(q.ScoreLevels()); n < 2 || n > 10 {
			return fmt.Errorf("question: score needs 2-10 levels, got %d", n)
		}
	case TypeNoul:
	default:
		return fmt.Errorf("question: unknown type %q", q.Type)
	}
	return nil
}

// LegendEntry is one score-legend value. The wire allows a plain string or an
// object; when it is an object, What carries its "what" field (what clients
// render) and Object keeps the full value so it round-trips untouched.
type LegendEntry struct {
	What   string
	Object map[string]any // nil when the wire value was a string
}

// UnmarshalJSON accepts either a JSON string or an object.
func (l *LegendEntry) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &l.What)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("legend: string or object expected: %w", err)
	}
	l.Object = obj
	l.What, _ = obj["what"].(string)
	return nil
}

// MarshalJSON emits the object form when present, else the string.
func (l LegendEntry) MarshalJSON() ([]byte, error) {
	if l.Object != nil {
		return json.Marshal(l.Object)
	}
	return json.Marshal(l.What)
}

// Answer is one typed answer, byte-for-byte the wire shape. Fields that do
// not apply to the type are omitted (a noul answer carries no confidence).
type Answer struct {
	Type          string                 `json:"type"`
	Choice        string                 `json:"choice,omitempty"`
	Score         *float64               `json:"score,omitempty"`
	Noul          *float64               `json:"noul,omitempty"`
	Probabilities map[string]float64     `json:"probabilities,omitempty"`
	Legend        map[string]LegendEntry `json:"legend,omitempty"`
	Confidence    *float64               `json:"confidence,omitempty"`
}

// Conf returns the confidence, or 0 when absent (noul answers).
func (a Answer) Conf() float64 {
	if a.Confidence == nil {
		return 0
	}
	return *a.Confidence
}

// ScoreValue returns the score, or 0 when absent.
func (a Answer) ScoreValue() float64 {
	if a.Score == nil {
		return 0
	}
	return *a.Score
}

// NoulValue returns the noul likelihood, or 0 when absent.
func (a Answer) NoulValue() float64 {
	if a.Noul == nil {
		return 0
	}
	return *a.Noul
}

// Usage is the token accounting the decider reports per request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Result is one evaluation: the answers keyed like the questions, plus the
// model that produced them and the request cost.
type Result struct {
	Answers   map[string]Answer
	Model     string
	Usage     Usage
	LatencyMs int64
}

// Decider answers a map of questions about a state. Implementations must be
// safe for concurrent use.
type Decider interface {
	Evaluate(ctx context.Context, state any, questions map[string]Question) (Result, error)
}

// Availability is optionally implemented by deciders that can be configured
// but unusable (jev without a key). The runtime checks it before a tick so
// the caller gets ErrUnavailable instead of a failed decision record.
type Availability interface {
	Available() error
}

// F is a small helper for building *float64 answer fields.
func F(v float64) *float64 { return &v }
