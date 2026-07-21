package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeLookup returns a binLookup from a set of harnesses considered
// "found" — everything else reports not-found, mirroring exec.LookPath's
// error shape without touching the real PATH.
func fakeLookup(found map[string]string) binLookup {
	return func(harness string) (string, error) {
		if p, ok := found[harness]; ok {
			return p, nil
		}
		return "", fmt.Errorf("exec: %q: not found on PATH", harness)
	}
}

func TestWriteDoctorAllHealthy(t *testing.T) {
	run := fakeCLI(map[string]string{
		"claude auth status --json": `{"loggedIn":true}`,
		"codex login status":        "Logged in using ChatGPT",
		"pi --list-models":          "provider  model  context\nanthropic  claude-opus-4-5  200K\nopenai  gpt-5.6  400K\n",
	})
	lookup := fakeLookup(map[string]string{
		"pi":     "/home/user/.bun/bin/pi",
		"claude": "/home/user/.local/bin/claude",
		"codex":  "/home/user/.local/bin/codex",
		"kimi":   "/home/user/.local/bin/kimi",
	})

	var buf bytes.Buffer
	writeDoctor(&buf, context.Background(), run, lookup)
	out := buf.String()

	for _, want := range []string{
		"pi:\n",
		"  binary: found (/home/user/.bun/bin/pi)\n",
		"2 models listed",
		"claude:\n",
		"  binary: found (/home/user/.local/bin/claude)\n",
		"  auth:   ok (logged in)\n",
		"codex:\n",
		"  binary: found (/home/user/.local/bin/codex)\n",
		"kimi:\n",
		"  binary: found (/home/user/.local/bin/kimi)\n",
		"  auth:   unknown (kimi has no auth-status command",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestWriteDoctorBinaryMissing(t *testing.T) {
	run := fakeCLI(nil)
	lookup := fakeLookup(map[string]string{
		"pi":     "/home/user/.bun/bin/pi",
		"claude": "/home/user/.local/bin/claude",
		// codex deliberately absent -> not found
	})

	var buf bytes.Buffer
	writeDoctor(&buf, context.Background(), run, lookup)
	out := buf.String()

	idx := strings.Index(out, "codex:\n")
	if idx < 0 {
		t.Fatalf("missing codex block:\n%s", out)
	}
	block := out[idx:]
	if !strings.Contains(block, "  binary: not found on PATH\n") {
		t.Errorf("codex block: want binary not found, got:\n%s", block)
	}
	if !strings.Contains(block, "  auth:   unknown (binary missing)\n") {
		t.Errorf("codex block: want auth unknown/binary missing, got:\n%s", block)
	}
	if !strings.Contains(block, "  model:  unknown (binary missing)\n") {
		t.Errorf("codex block: want model unknown/binary missing, got:\n%s", block)
	}
}

func TestWriteDoctorPiAuthUnknown(t *testing.T) {
	run := fakeCLI(map[string]string{
		"pi --list-models": "provider  model\nanthropic  claude-opus-4-5\n",
	})
	lookup := fakeLookup(map[string]string{"pi": "/home/user/.bun/bin/pi"})

	var buf bytes.Buffer
	writeDoctor(&buf, context.Background(), run, lookup)
	out := buf.String()

	idx := strings.Index(out, "pi:\n")
	if idx < 0 {
		t.Fatalf("missing pi block:\n%s", out)
	}
	block := out[idx:]
	if !strings.Contains(block, "  auth:   unknown (pi has no auth-status") {
		t.Errorf("pi block: want honest unknown auth (no fabricated status), got:\n%s", block)
	}
	if strings.Contains(block, "auth:   ok") {
		t.Errorf("pi has no auth primitive: must never report ok, got:\n%s", block)
	}
}
