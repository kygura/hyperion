package main

import (
	"context"
	"testing"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/store"
)

// testAgentKey is a syntactically valid secp256k1 key with no funds behind
// it — enough to exercise signer construction without touching a real wallet.
const testAgentKey = "0x0123456789012345678901234567890123456789012345678901234567890123"

// TestBuildExecutor_SignerBuiltInProposeMode guards the bug where an agent
// key present in propose mode (the default, safe mode) was silently ignored:
// buildExecutor only constructed a signer when execution.mode was already
// "autonomous" at startup, so an approved proposal could never actually be
// signed and submitted until the operator flipped modes first.
func TestBuildExecutor_SignerBuiltInProposeMode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Dir = dir
	cfg.Execution.Mode = "propose"

	b := bus.New()
	st, err := store.New(dir, cfg.Storage.RingSize)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	jr, err := journal.New(b, dir)
	if err != nil {
		t.Fatalf("journal.New: %v", err)
	}
	rest := hlclient.New(hlclient.TestnetAPI)

	exec := buildExecutor(context.Background(), cfg, b, st, jr, rest, testAgentKey, true)

	// The executor has no public signer getter; SetMode("autonomous") is
	// rejected specifically and only when no signer is configured (see
	// executor.SetMode), so it doubles as the externally-observable proof
	// that buildExecutor actually wired the key into a signer.
	if err := exec.SetMode("autonomous"); err != nil {
		t.Fatalf("expected a signer to be configured in propose mode when an agent key is present, got: %v", err)
	}
}

// TestBuildExecutor_NoSignerWithoutKey confirms the no-key case still stays
// unsigned, i.e. the fix didn't accidentally make a signer appear from
// nothing.
func TestBuildExecutor_NoSignerWithoutKey(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Dir = dir
	cfg.Execution.Mode = "propose"

	b := bus.New()
	st, err := store.New(dir, cfg.Storage.RingSize)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	jr, err := journal.New(b, dir)
	if err != nil {
		t.Fatalf("journal.New: %v", err)
	}
	rest := hlclient.New(hlclient.TestnetAPI)

	exec := buildExecutor(context.Background(), cfg, b, st, jr, rest, "", true)

	if err := exec.SetMode("autonomous"); err == nil {
		t.Fatal("expected SetMode(autonomous) to fail with no agent key configured")
	}
}
