package operator

import "github.com/hyperagent/tui/internal/apiclient"

// Tea messages the operator console reacts to. The bridge (PumpWS) produces
// the WS-derived ones; commands issued by Update produce the rest.
type (
	// connMsg is the TUI↔daemon push-link state, synthesized by PumpWS on
	// dial success and on every connection loss.
	connMsg struct{ Connected bool }

	// snapshotMsg is the result of one full REST cold-start (configs,
	// decisions, venues, governor). Err is the first fatal failure — when
	// set the console is offline and the other fields are ignored.
	snapshotMsg struct {
		Strategies []apiclient.StrategyStatus
		Decisions  []apiclient.DecisionRecord
		Venues     []apiclient.VenueStatus
		Governor   apiclient.GovernorSettings
		Partial    []string // non-fatal per-call failures, for the notice line
		Err        error
	}

	// WS strategy.* events (PROTOCOL.md).
	decisionMsg apiclient.DecisionRecord
	verdictMsg  apiclient.StrategyVerdictEvent
	configMsg   apiclient.StrategyStatus
	governorMsg apiclient.GovernorSettings
	venueMsg    apiclient.VenueStatus

	// opResultMsg is the outcome of one operator action (toggle, save,
	// dry-run, approve/reject, governor put, kill). Exactly one payload
	// pointer is set on success.
	opResultMsg struct {
		Op       string
		Err      error
		Status   *apiclient.StrategyStatus
		Decision *apiclient.DecisionRecord
		Governor *apiclient.GovernorSettings
		// FocusDecision switches to DECISIONS and selects Decision.
		FocusDecision bool
	}

	// refreshTickMsg fires the offline retry / stale-data poll.
	refreshTickMsg struct{}
)
