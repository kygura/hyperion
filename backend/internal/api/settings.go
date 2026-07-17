package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

type settingsResponse struct {
	Mode           string              `json:"mode"`
	Batch          roleSettings        `json:"batch"`
	Chat           roleSettings        `json:"chat"`
	Review         roleSettings        `json:"review"`
	Trigger        roleSettings        `json:"trigger"`
	ProviderNames  []string            `json:"provider_names"`
	ProviderModels map[string][]string `json:"provider_models"`
	KeyHints       map[string]string   `json:"key_hints"`
	// Visualized/Tracked/Timeframes let a client with no local config.toml
	// (the standalone TUI) bootstrap its initial watchlist and per-coin
	// timeframe at startup — the same three things tui.Config used to read
	// straight from cfg.Markets/cfg.Timeframe before the TUI moved out of
	// process.
	Visualized []string          `json:"visualized"`
	Tracked    []string          `json:"tracked"`
	Timeframes map[string]string `json:"timeframes"` // coin -> configured display tf
	Risk       riskSettings      `json:"risk"`
}

// riskSettings mirrors the TUI's read-only risk display (tui.RiskView),
// sourced from cfg.Execution — static per daemon run, no live-mutation
// endpoint exists for these today (nor did one exist for the embedded TUI).
type riskSettings struct {
	MaxPositionUSD      float64 `json:"max_position_usd"`
	MaxTotalExposureUSD float64 `json:"max_total_exposure_usd"`
	MaxConcurrent       int     `json:"max_concurrent"`
	DailyLossKillUSD    float64 `json:"daily_loss_kill_usd"`
}

type roleSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	reg := s.deps.Engine.Registry()
	batchP, batchM := reg.Active(reasoner.RoleBatch)
	chatP, chatM := reg.Active(reasoner.RoleChat)
	reviewP, reviewM := reg.Active(reasoner.RoleReview)
	triggerP, triggerM := reg.Active(reasoner.RoleTrigger)
	mode := s.deps.Cfg.Execution.Mode
	if s.deps.Exec != nil {
		mode = s.deps.Exec.Mode()
	}
	hints := map[string]string{}
	for _, name := range reg.Names() {
		if pc, ok := providerCfgFor(s.deps.CfgSnapshot(), name); ok {
			hints[name] = maskKey(pc.Key(strings.ToUpper(name) + "_API_KEY"))
		}
	}
	tfs := make(map[string]string, len(s.deps.Cfg.Markets.Visualized))
	for _, coin := range s.deps.Cfg.Markets.Visualized {
		tfs[coin] = s.deps.Cfg.Timeframe.For(coin)
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		Mode:           mode,
		Batch:          roleSettings{batchP, batchM},
		Chat:           roleSettings{chatP, chatM},
		Review:         roleSettings{reviewP, reviewM},
		Trigger:        roleSettings{triggerP, triggerM},
		ProviderNames:  reg.Names(),
		ProviderModels: reg.ProviderModels(),
		KeyHints:       hints,
		Visualized:     s.deps.Cfg.Markets.Visualized,
		Tracked:        s.deps.Cfg.Markets.Tracked,
		Timeframes:     tfs,
		Risk: riskSettings{
			MaxPositionUSD:      s.deps.Cfg.Execution.MaxPositionUSD,
			MaxTotalExposureUSD: s.deps.Cfg.Execution.MaxTotalExposureUSD,
			MaxConcurrent:       s.deps.Cfg.Execution.MaxConcurrent,
			DailyLossKillUSD:    s.deps.Cfg.Execution.DailyLossKillUSD,
		},
	})
}

type putSettingsRequest struct {
	ChatProvider    string `json:"chat_provider"`
	ChatModel       string `json:"chat_model"`
	BatchProvider   string `json:"batch_provider"`
	BatchModel      string `json:"batch_model"`
	ReviewProvider  string `json:"review_provider"`
	ReviewModel     string `json:"review_model"`
	TriggerProvider string `json:"trigger_provider"`
	TriggerModel    string `json:"trigger_model"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	var req putSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	reg := s.deps.Engine.Registry()
	// apply switches a role's provider (which resets the model to that provider's
	// default) and then its model, mirroring the picker's provider-then-model order.
	apply := func(role reasoner.Role, provider, model string) error {
		if provider != "" {
			if err := reg.SetProvider(role, provider); err != nil {
				return err
			}
		}
		if model != "" {
			if err := reg.SetModel(role, model); err != nil {
				return err
			}
		}
		return nil
	}
	for _, u := range []struct {
		role     reasoner.Role
		provider string
		model    string
	}{
		{reasoner.RoleChat, req.ChatProvider, req.ChatModel},
		{reasoner.RoleBatch, req.BatchProvider, req.BatchModel},
		{reasoner.RoleReview, req.ReviewProvider, req.ReviewModel},
		{reasoner.RoleTrigger, req.TriggerProvider, req.TriggerModel},
	} {
		if err := apply(u.role, u.provider, u.model); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
	}
	if s.deps.SaveConfig != nil {
		chatP, chatM := reg.Active(reasoner.RoleChat)
		batchP, batchM := reg.Active(reasoner.RoleBatch)
		reviewP, reviewM := reg.Active(reasoner.RoleReview)
		triggerP, triggerM := reg.Active(reasoner.RoleTrigger)
		_ = s.deps.SaveConfig(func(c *config.Config) {
			c.Reasoner.ChatProvider, c.Reasoner.ChatModel = chatP, chatM
			c.Reasoner.BatchProvider, c.Reasoner.BatchModel = batchP, batchM
			c.Reasoner.ReviewProvider, c.Reasoner.ReviewModel = reviewP, reviewM
			c.Reasoner.TriggerProvider, c.Reasoner.TriggerModel = triggerP, triggerM
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

type putModeRequest struct {
	Mode string `json:"mode"`
}

func (s *Server) handlePutMode(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "executor not configured")
		return
	}
	var req putModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if err := s.deps.Exec.SetMode(req.Mode); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
		return
	}
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) { c.Execution.Mode = req.Mode })
	}
	w.WriteHeader(http.StatusNoContent)
}

type putKeyRequest struct {
	Key string `json:"key"`
}

func (s *Server) handlePutProviderKey(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "reasoner not configured")
		return
	}
	name := r.PathValue("name")
	pc, ok := providerCfgFor(s.deps.CfgSnapshot(), name)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown provider %q", name)
		return
	}
	if strings.HasPrefix(pc.Kind, "harness-") {
		writeErr(w, http.StatusUnprocessableEntity, "provider %q authenticates via its own CLI login, not an API key — nothing to set", name)
		return
	}
	var req putKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if err := s.deps.Engine.Registry().Replace(name, buildProvider(name, pc, req.Key)); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
		return
	}
	if s.deps.SaveConfig != nil {
		_ = s.deps.SaveConfig(func(c *config.Config) { setProviderKey(c, name, req.Key) })
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers moved from backend/src/main.go: these were the TUI settings
// modal's plumbing; now the API server's, since it owns settings persistence.

func providerCfgFor(cfg config.Config, name string) (config.ProviderCfg, bool) {
	switch name {
	case "anthropic":
		return cfg.Providers.Anthropic, true
	case "openai":
		return cfg.Providers.OpenAI, true
	case "deepseek":
		return cfg.Providers.Deepseek, true
	}
	pc, ok := cfg.Providers.Custom[name]
	return pc, ok
}

func setProviderKey(c *config.Config, name, key string) {
	switch name {
	case "anthropic":
		c.Providers.Anthropic.APIKey = key
	case "openai":
		c.Providers.OpenAI.APIKey = key
	case "deepseek":
		c.Providers.Deepseek.APIKey = key
	default:
		if c.Providers.Custom == nil {
			c.Providers.Custom = map[string]config.ProviderCfg{}
		}
		pc := c.Providers.Custom[name]
		pc.APIKey = key
		c.Providers.Custom[name] = pc
	}
}

func buildProvider(name string, pc config.ProviderCfg, key string) reasoner.Provider {
	if name == "anthropic" || pc.Kind == "anthropic" {
		return reasoner.NewAnthropic(key, pc.Model, pc.BaseURL)
	}
	return reasoner.NewOpenAICompatible(name, key, pc.Model, pc.BaseURL)
}

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "•••"
	}
	return k[:6] + "…" + k[len(k)-4:]
}
