package runtime

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
)

// ringSize is how many decision records stay in memory (newest kept).
const ringSize = 500

// DecisionStore keeps the last ringSize DecisionRecords in memory and
// appends every record (and every later verdict, as a full re-encode of
// the record) to one NDJSON file per UTC day under dir. An empty dir keeps
// the store memory-only (tests, CLI dry runs). The last line for an id in a
// day file is its final state, so a restart reloads today's file and keeps
// the latest copy per id.
type DecisionStore struct {
	dir string
	now func() time.Time

	mu      sync.RWMutex
	records []strategy.DecisionRecord // oldest first
	index   map[string]int            // id -> position in records
}

// NewDecisionStore opens (creating) dir and reloads today's file. dir ""
// disables persistence.
func NewDecisionStore(dir string) (*DecisionStore, error) {
	s := &DecisionStore{dir: dir, now: time.Now, index: make(map[string]int)}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("decisions: mkdir %s: %w", dir, err)
	}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// reload reads today's and yesterday's files so a restart keeps the recent
// history the operator was looking at.
func (s *DecisionStore) reload() error {
	latest := map[string]strategy.DecisionRecord{}
	var order []string
	for _, day := range []time.Time{s.now().UTC().AddDate(0, 0, -1), s.now().UTC()} {
		path := filepath.Join(s.dir, day.Format("2006-01-02")+".ndjson")
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("decisions: open %s: %w", path, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var r strategy.DecisionRecord
			if json.Unmarshal(sc.Bytes(), &r) != nil || r.ID == "" {
				continue
			}
			if _, seen := latest[r.ID]; !seen {
				order = append(order, r.ID)
			}
			latest[r.ID] = r
		}
		f.Close()
	}
	sort.SliceStable(order, func(i, j int) bool { return latest[order[i]].TS.Before(latest[order[j]].TS) })
	if len(order) > ringSize {
		order = order[len(order)-ringSize:]
	}
	for _, id := range order {
		s.index[id] = len(s.records)
		s.records = append(s.records, latest[id])
	}
	return nil
}

// NewID returns a time-sortable id: 12 hex digits of unix milliseconds plus
// 8 random hex digits. Not a ULID, but lexically ordered by time like one.
func NewID(now time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%012x%s", now.UnixMilli(), hex.EncodeToString(b))
}

// Append stores a new record (assigning an id when blank) and persists it.
func (s *DecisionStore) Append(r strategy.DecisionRecord) (strategy.DecisionRecord, error) {
	if r.ID == "" {
		r.ID = NewID(s.now())
	}
	if r.TS.IsZero() {
		r.TS = s.now()
	}
	normalize(&r)
	s.mu.Lock()
	if i, ok := s.index[r.ID]; ok {
		s.records[i] = r
	} else {
		s.records = append(s.records, r)
		if len(s.records) > ringSize {
			drop := len(s.records) - ringSize
			s.records = s.records[drop:]
			s.index = make(map[string]int, len(s.records))
			for i, rec := range s.records {
				s.index[rec.ID] = i
			}
		} else {
			s.index[r.ID] = len(s.records) - 1
		}
	}
	s.mu.Unlock()
	return r, s.persist(r)
}

// AppendVerdict adds a verdict to a stored record and re-persists it.
func (s *DecisionStore) AppendVerdict(decisionID string, v strategy.Verdict) (strategy.DecisionRecord, error) {
	if v.TS.IsZero() {
		v.TS = s.now()
	}
	s.mu.Lock()
	i, ok := s.index[decisionID]
	if !ok {
		s.mu.Unlock()
		return strategy.DecisionRecord{}, fmt.Errorf("decision %s not found", decisionID)
	}
	if _, ok := s.records[i].Intent(v.IntentID); !ok {
		s.mu.Unlock()
		return strategy.DecisionRecord{}, fmt.Errorf("intent %s not in decision %s", v.IntentID, decisionID)
	}
	s.records[i].Verdicts = append(s.records[i].Verdicts, v)
	r := s.records[i]
	s.mu.Unlock()
	return r, s.persist(r)
}

// Get returns one record by id.
func (s *DecisionStore) Get(id string) (strategy.DecisionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.index[id]
	if !ok {
		return strategy.DecisionRecord{}, false
	}
	return s.records[i], true
}

// List returns up to limit records newest first, optionally filtered by
// strategy id. limit <= 0 means 50.
func (s *DecisionStore) List(limit int, strategyID string) []strategy.DecisionRecord {
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]strategy.DecisionRecord, 0, limit)
	for i := len(s.records) - 1; i >= 0 && len(out) < limit; i-- {
		if strategyID != "" && s.records[i].StrategyID != strategyID {
			continue
		}
		out = append(out, s.records[i])
	}
	return out
}

// Open returns every (decision id, intent) whose latest verdict is
// proposed or approved — the intents the governor counts against
// max_open_intents and the ones Kill rejects.
func (s *DecisionStore) Open() []OpenIntent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []OpenIntent
	for _, r := range s.records {
		if r.DryRun {
			continue
		}
		for _, in := range r.Intents {
			v, ok := r.LatestVerdict(in.ID)
			if !ok || v.Terminal() {
				continue
			}
			out = append(out, OpenIntent{DecisionID: r.ID, Intent: in, Status: v.Status})
		}
	}
	return out
}

// OpenCount is Open()'s length, for the governor.
func (s *DecisionStore) OpenCount() int { return len(s.Open()) }

// OpenIntent is one non-terminal intent with its decision id.
type OpenIntent struct {
	DecisionID string
	Intent     strategy.Intent
	Status     string
}

// persist appends the record to its day file.
func (s *DecisionStore) persist(r strategy.DecisionRecord) error {
	if s.dir == "" {
		return nil
	}
	path := filepath.Join(s.dir, r.TS.UTC().Format("2006-01-02")+".ndjson")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("decisions: open %s: %w", path, err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(r)
}

// normalize makes the JSON shape stable: empty slices/maps, never null.
func normalize(r *strategy.DecisionRecord) {
	if r.Intents == nil {
		r.Intents = []strategy.Intent{}
	}
	if r.Verdicts == nil {
		r.Verdicts = []strategy.Verdict{}
	}
	if r.Answers == nil {
		r.Answers = strategy.Answers{}
	}
	if r.Questions == nil {
		r.Questions = map[string]decider.Question{}
	}
}
