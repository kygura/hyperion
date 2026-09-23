// Package operator is the terminal operator console: four keyboard-driven
// tabs (STRATEGIES, DECISIONS, VENUES, GOVERNOR) over the daemon's
// /api/strategy/* surface (docs/jev/SPEC.md "Terminal (tui/)",
// docs/jev/PROTOCOL.md). It reads over HTTP+WS and writes only through
// apiclient; there is no chat anywhere in it.
package operator

import (
	"context"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// API is the slice of apiclient.Client the console needs. It is an
// interface so tests can drive Update with a fake daemon.
type API interface {
	Manifests(ctx context.Context) ([]apiclient.Manifest, error)
	StrategyConfigs(ctx context.Context) ([]apiclient.StrategyStatus, error)
	PutStrategyConfig(ctx context.Context, cfg apiclient.StrategyConfig) (apiclient.StrategyStatus, error)
	RunStrategy(ctx context.Context, id string, dryRun bool) (apiclient.DecisionRecord, error)
	Decisions(ctx context.Context, limit int, strategy string) ([]apiclient.DecisionRecord, error)
	ApproveIntent(ctx context.Context, decisionID, intentID string) (apiclient.DecisionRecord, error)
	RejectIntent(ctx context.Context, decisionID, intentID string) (apiclient.DecisionRecord, error)
	Venues(ctx context.Context) ([]apiclient.VenueStatus, error)
	Governor(ctx context.Context) (apiclient.GovernorSettings, error)
	PutGovernor(ctx context.Context, g apiclient.GovernorSettings) (apiclient.GovernorSettings, error)
	Kill(ctx context.Context) (apiclient.GovernorSettings, error)
}

var _ API = (*apiclient.Client)(nil)

// Config carries everything the console needs at construction.
type Config struct {
	API     API
	CoreURL string // shown in the offline panel
}

type tab int

const (
	tabStrategies tab = iota
	tabDecisions
	tabVenues
	tabGovernor
	tabCount
)

var tabNames = [tabCount]string{"STRATEGIES", "DECISIONS", "VENUES", "GOVERNOR"}

const (
	minW = 60
	minH = 16

	// decisionsKeep caps the in-memory decision list (newest first).
	decisionsKeep = 200
	// snapshotLimit is the decisions page fetched on cold start.
	snapshotLimit = 50
	// refreshEvery is the offline retry / stale poll cadence.
	refreshEvery = 5 * time.Second
	// requestTimeout bounds every daemon call issued from a key press.
	requestTimeout = 15 * time.Second
	// noticeTTL is how long a footer notice stays visible.
	noticeTTL = 8 * time.Second
)

// Model is the operator console root model.
type Model struct {
	width, height int
	api           API
	coreURL       string

	tab tab

	// Daemon link state. connected is the WS push link; loaded is whether a
	// REST snapshot has ever succeeded; offlineErr is the last snapshot
	// failure (empty when the last snapshot succeeded).
	connected  bool
	loaded     bool
	offlineErr string
	fetching   bool

	governor   apiclient.GovernorSettings
	strategies []apiclient.StrategyStatus // sorted by manifest id
	decisions  []apiclient.DecisionRecord // newest first
	venues     []apiclient.VenueStatus

	stratCursor  int
	decCursor    int
	intentCursor int
	detailScroll int
	venueCursor  int

	form *paramForm   // STRATEGIES: open param editor, nil when closed
	gov  governorForm // GOVERNOR: editable draft
	kill killPrompt   // GOVERNOR: typed confirmation

	busy     string // in-flight operation label; "" when idle
	notice   string
	noticeAt time.Time
	noticeOK bool
}

// New builds the console model. Nothing is fetched until Init.
func New(cfg Config) *Model {
	return &Model{
		api:     cfg.API,
		coreURL: cfg.CoreURL,
		gov:     newGovernorForm(apiclient.GovernorSettings{Mode: apiclient.GovernorManual}),
		kill:    newKillPrompt(),
	}
}

// Init implements tea.Model: cold-start snapshot plus the retry ticker.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.fetchSnapshot(), refreshTick())
}

// refreshTick schedules the next refreshTickMsg. A var so tests can stub
// the 5s timer out.
var refreshTick = func() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// fetchSnapshot cold-starts every tab from REST. Configs failing is fatal
// (offline); the other calls degrade to a notice so a daemon with the
// strategy subsystem half-mounted still shows what it can.
func (m *Model) fetchSnapshot() tea.Cmd {
	if m.api == nil || m.fetching {
		return nil
	}
	m.fetching = true
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		var out snapshotMsg
		statuses, err := api.StrategyConfigs(ctx)
		if err != nil {
			out.Err = err
			return out
		}
		manifests, err := api.Manifests(ctx)
		if err != nil {
			out.Partial = append(out.Partial, "manifests: "+err.Error())
		}
		out.Strategies = mergeStatuses(statuses, manifests)
		if out.Decisions, err = api.Decisions(ctx, snapshotLimit, ""); err != nil {
			out.Partial = append(out.Partial, "decisions: "+err.Error())
		}
		if out.Venues, err = api.Venues(ctx); err != nil {
			out.Partial = append(out.Partial, "venues: "+err.Error())
		}
		if out.Governor, err = api.Governor(ctx); err != nil {
			out.Partial = append(out.Partial, "governor: "+err.Error())
		}
		return out
	}
}

// mergeStatuses returns one StrategyStatus per manifest: the daemon's status
// where it has one, otherwise a disabled default config, so a manifest the
// daemon knows but never configured still appears in the table. Sorted by
// manifest id.
func mergeStatuses(statuses []apiclient.StrategyStatus, manifests []apiclient.Manifest) []apiclient.StrategyStatus {
	byID := make(map[string]bool, len(statuses))
	out := make([]apiclient.StrategyStatus, 0, len(statuses)+len(manifests))
	for _, s := range statuses {
		if s.Config.ID == "" {
			s.Config.ID = s.Manifest.ID
		}
		if s.Config.Params == nil {
			s.Config.Params = map[string]any{}
		}
		byID[s.Manifest.ID] = true
		out = append(out, s)
	}
	for _, mf := range manifests {
		if byID[mf.ID] {
			continue
		}
		out = append(out, apiclient.StrategyStatus{Manifest: mf, Config: defaultConfig(mf)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

// defaultConfig is the config an unconfigured manifest edits from: disabled,
// first venue, manifest defaults.
func defaultConfig(mf apiclient.Manifest) apiclient.StrategyConfig {
	cfg := apiclient.StrategyConfig{ID: mf.ID, Params: map[string]any{}}
	if len(mf.Venues) > 0 {
		cfg.Venue = mf.Venues[0]
	}
	for _, p := range mf.Params {
		if p.Default != nil {
			cfg.Params[p.Key] = p.Default
		}
	}
	return cfg
}

// selectedStrategy returns the cursor row of the STRATEGIES table.
func (m *Model) selectedStrategy() (apiclient.StrategyStatus, bool) {
	if m.stratCursor < 0 || m.stratCursor >= len(m.strategies) {
		return apiclient.StrategyStatus{}, false
	}
	return m.strategies[m.stratCursor], true
}

// selectedDecision returns the cursor row of the DECISIONS list.
func (m *Model) selectedDecision() (apiclient.DecisionRecord, bool) {
	if m.decCursor < 0 || m.decCursor >= len(m.decisions) {
		return apiclient.DecisionRecord{}, false
	}
	return m.decisions[m.decCursor], true
}

// selectedIntent returns the selected intent of the selected decision.
func (m *Model) selectedIntent() (apiclient.DecisionRecord, apiclient.Intent, bool) {
	d, ok := m.selectedDecision()
	if !ok || m.intentCursor < 0 || m.intentCursor >= len(d.Intents) {
		return d, apiclient.Intent{}, false
	}
	return d, d.Intents[m.intentCursor], true
}

// upsertStatus replaces or inserts a strategy status, keeping id order.
func (m *Model) upsertStatus(s apiclient.StrategyStatus) {
	if s.Config.Params == nil {
		s.Config.Params = map[string]any{}
	}
	for i := range m.strategies {
		if m.strategies[i].Manifest.ID == s.Manifest.ID {
			m.strategies[i] = s
			return
		}
	}
	m.strategies = append(m.strategies, s)
	sort.SliceStable(m.strategies, func(i, j int) bool { return m.strategies[i].Manifest.ID < m.strategies[j].Manifest.ID })
}

// upsertDecision prepends a new decision or replaces an existing one by id.
// The cursor follows the decision it was on, so a live prepend does not
// silently swap the detail pane under the operator. Returns the index.
func (m *Model) upsertDecision(d apiclient.DecisionRecord) int {
	for i := range m.decisions {
		if m.decisions[i].ID == d.ID {
			m.decisions[i] = d
			return i
		}
	}
	m.decisions = append([]apiclient.DecisionRecord{d}, m.decisions...)
	if len(m.decisions) > decisionsKeep {
		m.decisions = m.decisions[:decisionsKeep]
	}
	if len(m.decisions) > 1 {
		m.decCursor++
		if m.decCursor >= len(m.decisions) {
			m.decCursor = len(m.decisions) - 1
		}
	}
	return 0
}

// focusDecision selects decision index i on the DECISIONS tab.
func (m *Model) focusDecision(i int) {
	m.tab = tabDecisions
	m.decCursor = i
	m.intentCursor = 0
	m.detailScroll = 0
}

// upsertVenue replaces or appends a venue status by id.
func (m *Model) upsertVenue(v apiclient.VenueStatus) {
	for i := range m.venues {
		if m.venues[i].ID == v.ID {
			m.venues[i] = v
			return
		}
	}
	m.venues = append(m.venues, v)
	sort.SliceStable(m.venues, func(i, j int) bool { return m.venues[i].ID < m.venues[j].ID })
}

// setNotice shows a transient footer message.
func (m *Model) setNotice(text string, ok bool) {
	m.notice, m.noticeAt, m.noticeOK = text, timeNow(), ok
}

// pendingProposals counts intents whose latest verdict is still proposed.
func (m *Model) pendingProposals() int {
	n := 0
	for _, d := range m.decisions {
		if d.DryRun {
			continue
		}
		for _, in := range d.Intents {
			if v, ok := d.LatestVerdict(in.ID); ok && v.Status == apiclient.VerdictProposed {
				n++
			}
		}
	}
	return n
}

// timeNow is a seam for tests.
var timeNow = time.Now
