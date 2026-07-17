package reasoner

import (
	"context"
	"slices"
	"testing"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// recordProvider is a fake Provider that records the model id and role it was asked
// to serve, plus a call count, so we can assert the registry's bound model actually
// reaches the request and that each role hits its own provider.
type recordProvider struct {
	name      string
	lastModel string
	lastRole  Role
	calls     int
}

func (r *recordProvider) Name() string { return r.name }
func (r *recordProvider) Complete(_ context.Context, req Request) (Response, error) {
	r.lastModel = req.Model
	r.lastRole = req.Role
	r.calls++
	return Response{Reply: "ok", Model: req.Model}, nil
}

func TestRegistryModelSwitching(t *testing.T) {
	ant := &recordProvider{name: "anthropic"}
	ds := &recordProvider{name: "deepseek"}
	providers := map[string]Provider{"anthropic": ant, "deepseek": ds}
	models := map[string][]string{
		"anthropic": {"claude-opus-4-8", "claude-sonnet-4-6"},
		"deepseek":  {"deepseek-chat", "deepseek-reasoner"},
	}
	reg := NewRegistry(providers, models, "deepseek", "deepseek-chat", "anthropic", "claude-opus-4-8", "anthropic", "claude-opus-4-8", "deepseek", "deepseek-chat")

	// Initial binding from construction.
	if p, model := reg.Active(RoleChat); p != "anthropic" || model != "claude-opus-4-8" {
		t.Fatalf("chat binding = %s/%s, want anthropic/claude-opus-4-8", p, model)
	}

	// The core fix: switch the MODEL without touching the provider.
	if err := reg.SetModel(RoleChat, "claude-sonnet-4-6"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if p, model := reg.Active(RoleChat); p != "anthropic" || model != "claude-sonnet-4-6" {
		t.Fatalf("after SetModel = %s/%s, want anthropic/claude-sonnet-4-6", p, model)
	}

	// For returns the bound model, and Complete receives it.
	prov, model, ok := reg.For(RoleChat)
	if !ok || model != "claude-sonnet-4-6" {
		t.Fatalf("For(chat) model=%q ok=%v", model, ok)
	}
	if _, err := prov.Complete(context.Background(), Request{Role: RoleChat, Model: model}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if ant.lastModel != "claude-sonnet-4-6" {
		t.Fatalf("provider received model %q, want claude-sonnet-4-6", ant.lastModel)
	}

	// Switching provider resets the model to that provider's default.
	if err := reg.SetProvider(RoleChat, "deepseek"); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if p, model := reg.Active(RoleChat); p != "deepseek" || model != "deepseek-chat" {
		t.Fatalf("after SetProvider = %s/%s, want deepseek/deepseek-chat", p, model)
	}

	// A free-form model id (not in the configured list) is accepted and surfaced in
	// the picker — this is what makes a many-model endpoint addressable.
	if err := reg.SetModel(RoleChat, "deepseek-vision-7b"); err != nil {
		t.Fatalf("SetModel free-form: %v", err)
	}
	if pm := reg.ProviderModels(); !slices.Contains(pm["deepseek"], "deepseek-vision-7b") {
		t.Fatalf("ProviderModels missing free-form model: %v", pm["deepseek"])
	}

	// Unknown provider errors; empty model errors.
	if err := reg.SetProvider(RoleChat, "nope"); err == nil {
		t.Fatal("SetProvider(nope) should error")
	}
	if err := reg.SetModel(RoleChat, ""); err == nil {
		t.Fatal("SetModel(\"\") should error")
	}

	// The batch role is independent of chat changes.
	if p, model := reg.Active(RoleBatch); p != "deepseek" || model != "deepseek-chat" {
		t.Fatalf("batch binding drifted to %s/%s", p, model)
	}
}

// TestForBlankProviderFailsLoud proves a role bound to an empty provider name
// (e.g. an unset review_provider/trigger_provider) returns ok=false instead of
// guessing an arbitrary provider — a trading system must not silently reason on a
// random model for its decision role.
func TestForBlankProviderFailsLoud(t *testing.T) {
	providers := map[string]Provider{"deepseek": &recordProvider{name: "deepseek"}}
	models := map[string][]string{"deepseek": {"deepseek-chat"}}
	reg := NewRegistry(providers, models,
		"deepseek", "deepseek-chat", // batch
		"deepseek", "deepseek-chat", // chat
		"", "", // review: blank
		"", "") // trigger: blank

	if _, _, ok := reg.For(RoleReview); ok {
		t.Fatal("For(review) with blank provider returned ok=true; want fail-loud ok=false")
	}
	// A populated role still resolves normally.
	if _, _, ok := reg.For(RoleBatch); !ok {
		t.Fatal("For(batch) with populated provider returned ok=false")
	}
}

// TestReasonRoutesEachRoleToItsOwnProvider is the regression proof for the
// hardcoded-RoleBatch bug: reason() must resolve the provider binding for the
// role it is actually processing, so review and trigger digests hit two
// different providers. Against the old code — which looked up For(RoleBatch)
// for every role — a review digest would have hit the batch-bound (trigger)
// provider and left the review provider with zero calls, failing this test.
func TestReasonRoutesEachRoleToItsOwnProvider(t *testing.T) {
	reviewProv := &recordProvider{name: "reviewer"}
	triggerProv := &recordProvider{name: "trigger"}
	providers := map[string]Provider{"reviewer": reviewProv, "trigger": triggerProv}
	models := map[string][]string{"reviewer": {"rm"}, "trigger": {"tm"}}
	// Batch is deliberately bound to the trigger provider; the old reason() sent
	// every role through this batch binding, so a review digest would misroute here.
	reg := NewRegistry(providers, models,
		"trigger", "tm", // batch
		"reviewer", "rm", // chat (unused here)
		"reviewer", "rm", // review
		"trigger", "tm") // trigger
	eng := NewEngine(bus.New(), reg, nil, nil)

	eng.reason(context.Background(), metrics.DigestReview,
		[]metrics.Digest{{Coin: "BTC", Kind: metrics.DigestReview}})
	eng.reason(context.Background(), metrics.DigestTrigger,
		[]metrics.Digest{{Coin: "ETH", Kind: metrics.DigestTrigger}})

	if reviewProv.calls != 1 || reviewProv.lastRole != RoleReview {
		t.Fatalf("review provider: calls=%d lastRole=%q, want exactly 1 call as review", reviewProv.calls, reviewProv.lastRole)
	}
	if triggerProv.calls != 1 || triggerProv.lastRole != RoleTrigger {
		t.Fatalf("trigger provider: calls=%d lastRole=%q, want exactly 1 call as trigger", triggerProv.calls, triggerProv.lastRole)
	}
}
