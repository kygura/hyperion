package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/runtime"
)

// Strategy routes (docs/jev/PROTOCOL.md "HTTP"). Every handler degrades to
// 503 when Deps.Strategy is nil (strategy.enabled=false), matching how the
// legacy handlers treat a missing Engine/Exec.
func (s *Server) strategyRoutes() {
	s.mux.HandleFunc("GET /api/strategy/manifests", s.handleStrategyManifests)
	s.mux.HandleFunc("GET /api/strategy/configs", s.handleStrategyConfigs)
	s.mux.HandleFunc("GET /api/strategy/configs/{id}", s.handleStrategyConfig)
	s.mux.HandleFunc("PUT /api/strategy/configs/{id}", s.handleStrategyPutConfig)
	s.mux.HandleFunc("POST /api/strategy/configs/{id}/run", s.handleStrategyRun)
	s.mux.HandleFunc("GET /api/strategy/decisions", s.handleStrategyDecisions)
	s.mux.HandleFunc("GET /api/strategy/decisions/{id}", s.handleStrategyDecision)
	s.mux.HandleFunc("POST /api/strategy/decisions/{id}/intents/{iid}/approve", s.handleStrategyApprove)
	s.mux.HandleFunc("POST /api/strategy/decisions/{id}/intents/{iid}/reject", s.handleStrategyReject)
	s.mux.HandleFunc("GET /api/strategy/venues", s.handleStrategyVenues)
	s.mux.HandleFunc("GET /api/strategy/governor", s.handleStrategyGovernor)
	s.mux.HandleFunc("PUT /api/strategy/governor", s.handleStrategyPutGovernor)
	s.mux.HandleFunc("POST /api/strategy/kill", s.handleStrategyKill)
}

// runner returns the strategy runtime or writes the 503 and returns nil.
func (s *Server) runner(w http.ResponseWriter) *runtime.Runner {
	if s.deps.Strategy == nil {
		writeErr(w, http.StatusServiceUnavailable, "strategy runtime disabled")
		return nil
	}
	return s.deps.Strategy
}

// writeStrategyErr maps runtime errors onto the {error, field} envelope:
// validation → 400 (with field), not found → 404, conflict → 409, decider
// unavailable → 503, anything else → 500.
func writeStrategyErr(w http.ResponseWriter, err error) {
	var ve *strategy.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg, "field": ve.Field})
	case errors.Is(err, runtime.ErrNotFound):
		writeErr(w, http.StatusNotFound, "%s", err.Error())
	case errors.Is(err, runtime.ErrConflict):
		writeErr(w, http.StatusConflict, "%s", err.Error())
	case errors.Is(err, decider.ErrUnavailable):
		writeErr(w, http.StatusServiceUnavailable, "decider unavailable")
	default:
		writeErr(w, http.StatusInternalServerError, "%s", err.Error())
	}
}

func (s *Server) handleStrategyManifests(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifests": rt.Manifests()})
}

func (s *Server) handleStrategyConfigs(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"strategies": rt.Statuses()})
}

func (s *Server) handleStrategyConfig(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	st, err := rt.Status(r.PathValue("id"))
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleStrategyPutConfig validates the body against the manifest (400 with
// {error, field}) and applies it; the change is persisted to config.toml
// best-effort through Deps.SaveConfig.
func (s *Server) handleStrategyPutConfig(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	var cfg strategy.StrategyConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: %s", err.Error())
		return
	}
	id := r.PathValue("id")
	if cfg.ID != "" && cfg.ID != id {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body id does not match path", "field": "id"})
		return
	}
	cfg.ID = id
	st, err := rt.SetConfig(cfg)
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	s.persistStrategyConfig(st.Config)
	writeJSON(w, http.StatusOK, st)
}

// persistStrategyConfig upserts one entry in cfg.Strategy.Configs.
func (s *Server) persistStrategyConfig(cfg strategy.StrategyConfig) {
	if s.deps.SaveConfig == nil {
		return
	}
	_ = s.deps.SaveConfig(func(c *config.Config) {
		for i := range c.Strategy.Configs {
			if c.Strategy.Configs[i].ID == cfg.ID {
				c.Strategy.Configs[i] = cfg
				return
			}
		}
		c.Strategy.Configs = append(c.Strategy.Configs, cfg)
	})
}

// handleStrategyRun executes one tick synchronously. dry_run never touches
// the governor or a venue.
func (s *Server) handleStrategyRun(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	var req struct {
		DryRun bool `json:"dry_run"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body: %s", err.Error())
			return
		}
	}
	rec, err := rt.RunOnce(r.Context(), r.PathValue("id"), req.DryRun)
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleStrategyDecisions(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer", "field": "limit"})
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": rt.Store().List(limit, r.URL.Query().Get("strategy"))})
}

func (s *Server) handleStrategyDecision(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	rec, ok := rt.Store().Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "decision %q not found", r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleStrategyApprove(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	rec, err := rt.Approve(r.Context(), r.PathValue("id"), r.PathValue("iid"))
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleStrategyReject(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	rec, err := rt.Reject(r.PathValue("id"), r.PathValue("iid"))
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleStrategyVenues(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"venues": rt.Venues(r.Context())})
}

func (s *Server) handleStrategyGovernor(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	writeJSON(w, http.StatusOK, rt.Governor().Settings())
}

// handleStrategyPutGovernor replaces the governor settings. killed=false on
// a killed governor un-kills it (strategies stay disabled); killed=true
// kills without touching configs — POST /kill is the full stop.
func (s *Server) handleStrategyPutGovernor(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	body := rt.Governor().Settings()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: %s", err.Error())
		return
	}
	out, err := rt.UpdateGovernor(body)
	if err != nil {
		writeStrategyErr(w, err)
		return
	}
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) { c.Strategy.Governor = out })
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleStrategyKill(w http.ResponseWriter, r *http.Request) {
	rt := s.runner(w)
	if rt == nil {
		return
	}
	out := rt.Kill()
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) {
			for i := range c.Strategy.Configs {
				c.Strategy.Configs[i].Enabled = false
			}
		})
	}
	writeJSON(w, http.StatusOK, out)
}
