package apiclient

import (
	"encoding/json"
	"fmt"
)

// Envelope is one /api/ws frame. The legacy daemon writes {"topic":…,"data":…}
// while PROTOCOL.md's strategy events use {"type":…,"data":…}; Kind() hides
// the difference so one read loop can switch on both.
type Envelope struct {
	Type  string          `json:"type,omitempty"`
	Topic string          `json:"topic,omitempty"`
	Data  json.RawMessage `json:"data"`
}

// Kind returns the event name regardless of which key carried it.
func (e Envelope) Kind() string {
	if e.Type != "" {
		return e.Type
	}
	return e.Topic
}

// Strategy WS event names (PROTOCOL.md, "WebSocket").
const (
	EventStrategyDecision = "strategy.decision"
	EventStrategyVerdict  = "strategy.verdict"
	EventStrategyConfig   = "strategy.config"
	EventStrategyGovernor = "strategy.governor"
	EventStrategyVenue    = "strategy.venue"
)

// StrategyVerdictEvent is the payload of a strategy.verdict frame.
type StrategyVerdictEvent struct {
	DecisionID string        `json:"decision_id"`
	Verdict    IntentVerdict `json:"verdict"`
}

// IsStrategyEvent reports whether kind is one of the strategy.* events.
func IsStrategyEvent(kind string) bool {
	switch kind {
	case EventStrategyDecision, EventStrategyVerdict, EventStrategyConfig, EventStrategyGovernor, EventStrategyVenue:
		return true
	}
	return false
}

// DecodeStrategyEvent decodes a strategy.* envelope into its typed payload:
// DecisionRecord, StrategyVerdictEvent, StrategyStatus, GovernorSettings or
// VenueStatus. ok is false for any other kind; err is set when the data does
// not decode.
func DecodeStrategyEvent(e Envelope) (payload any, ok bool, err error) {
	switch e.Kind() {
	case EventStrategyDecision:
		var d DecisionRecord
		err = json.Unmarshal(e.Data, &d)
		return d, true, err
	case EventStrategyVerdict:
		var v StrategyVerdictEvent
		err = json.Unmarshal(e.Data, &v)
		return v, true, err
	case EventStrategyConfig:
		var s StrategyStatus
		err = json.Unmarshal(e.Data, &s)
		return s, true, err
	case EventStrategyGovernor:
		var g GovernorSettings
		err = json.Unmarshal(e.Data, &g)
		return g, true, err
	case EventStrategyVenue:
		var v VenueStatus
		err = json.Unmarshal(e.Data, &v)
		return v, true, err
	}
	return nil, false, nil
}

// DecodeStrategyFrame is DecodeStrategyEvent over a raw frame.
func DecodeStrategyFrame(raw []byte) (payload any, ok bool, err error) {
	var e Envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, false, fmt.Errorf("ws frame: %w", err)
	}
	return DecodeStrategyEvent(e)
}
