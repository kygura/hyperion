package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// The /api/strategy/* surface (PROTOCOL.md, "HTTP"). Every method is a thin
// wrapper over do; errors from the daemon come back as *APIError.

// Manifests → GET /api/strategy/manifests.
func (c *Client) Manifests(ctx context.Context) ([]Manifest, error) {
	var out struct {
		Manifests []Manifest `json:"manifests"`
	}
	err := c.do(ctx, http.MethodGet, "/api/strategy/manifests", nil, &out)
	return out.Manifests, err
}

// StrategyConfigs → GET /api/strategy/configs.
func (c *Client) StrategyConfigs(ctx context.Context) ([]StrategyStatus, error) {
	var out struct {
		Strategies []StrategyStatus `json:"strategies"`
	}
	err := c.do(ctx, http.MethodGet, "/api/strategy/configs", nil, &out)
	return out.Strategies, err
}

// StrategyConfig → GET /api/strategy/configs/{id}.
func (c *Client) StrategyConfig(ctx context.Context, id string) (StrategyStatus, error) {
	var out StrategyStatus
	err := c.do(ctx, http.MethodGet, "/api/strategy/configs/"+url.PathEscape(id), nil, &out)
	return out, err
}

// PutStrategyConfig → PUT /api/strategy/configs/{id}. A 400 validation error
// comes back as *APIError with Field set (e.g. "params.size_usd").
func (c *Client) PutStrategyConfig(ctx context.Context, cfg StrategyConfig) (StrategyStatus, error) {
	var out StrategyStatus
	err := c.do(ctx, http.MethodPut, "/api/strategy/configs/"+url.PathEscape(cfg.ID), cfg, &out)
	return out, err
}

// RunStrategy → POST /api/strategy/configs/{id}/run. Synchronous: the
// returned DecisionRecord is the tick's result.
func (c *Client) RunStrategy(ctx context.Context, id string, dryRun bool) (DecisionRecord, error) {
	var out DecisionRecord
	err := c.do(ctx, http.MethodPost, "/api/strategy/configs/"+url.PathEscape(id)+"/run", map[string]any{"dry_run": dryRun}, &out)
	return out, err
}

// Decisions → GET /api/strategy/decisions?limit=&strategy= (newest first).
// limit <= 0 omits the parameter; strategy "" omits the filter.
func (c *Client) Decisions(ctx context.Context, limit int, strategy string) ([]DecisionRecord, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if strategy != "" {
		q.Set("strategy", strategy)
	}
	path := "/api/strategy/decisions"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Decisions []DecisionRecord `json:"decisions"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Decisions, err
}

// Decision → GET /api/strategy/decisions/{id}.
func (c *Client) Decision(ctx context.Context, id string) (DecisionRecord, error) {
	var out DecisionRecord
	err := c.do(ctx, http.MethodGet, "/api/strategy/decisions/"+url.PathEscape(id), nil, &out)
	return out, err
}

// ApproveIntent → POST /api/strategy/decisions/{id}/intents/{iid}/approve.
func (c *Client) ApproveIntent(ctx context.Context, decisionID, intentID string) (DecisionRecord, error) {
	return c.intentAction(ctx, decisionID, intentID, "approve")
}

// RejectIntent → POST /api/strategy/decisions/{id}/intents/{iid}/reject.
func (c *Client) RejectIntent(ctx context.Context, decisionID, intentID string) (DecisionRecord, error) {
	return c.intentAction(ctx, decisionID, intentID, "reject")
}

func (c *Client) intentAction(ctx context.Context, decisionID, intentID, verb string) (DecisionRecord, error) {
	var out DecisionRecord
	path := fmt.Sprintf("/api/strategy/decisions/%s/intents/%s/%s", url.PathEscape(decisionID), url.PathEscape(intentID), verb)
	err := c.do(ctx, http.MethodPost, path, nil, &out)
	return out, err
}

// Venues → GET /api/strategy/venues.
func (c *Client) Venues(ctx context.Context) ([]VenueStatus, error) {
	var out struct {
		Venues []VenueStatus `json:"venues"`
	}
	err := c.do(ctx, http.MethodGet, "/api/strategy/venues", nil, &out)
	return out.Venues, err
}

// Governor → GET /api/strategy/governor.
func (c *Client) Governor(ctx context.Context) (GovernorSettings, error) {
	var out GovernorSettings
	err := c.do(ctx, http.MethodGet, "/api/strategy/governor", nil, &out)
	return out, err
}

// PutGovernor → PUT /api/strategy/governor.
func (c *Client) PutGovernor(ctx context.Context, g GovernorSettings) (GovernorSettings, error) {
	var out GovernorSettings
	err := c.do(ctx, http.MethodPut, "/api/strategy/governor", g, &out)
	return out, err
}

// Kill → POST /api/strategy/kill: killed=true, every config disabled, open
// proposals rejected.
func (c *Client) Kill(ctx context.Context) (GovernorSettings, error) {
	var out GovernorSettings
	err := c.do(ctx, http.MethodPost, "/api/strategy/kill", nil, &out)
	return out, err
}
