// doctor: `hyperagent doctor` — preflight health report for each reasoning
// harness (pi/claude/codex): binary on PATH, auth status, model reachability.
// Plain text, one block per harness, consistent "key: value" lines so a
// script (or a human with grep) can parse it. Pure formatting over
// harness_probe.go's probeBinary/probeAuth/probeModels — no new probing
// logic lives here.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

// harnessNames is the fixed report order.
var harnessNames = []string{"pi", "claude", "codex", "kimi"}

// binLookup resolves a harness binary's path on PATH. Injectable (like
// cliRunner) so doctor's tests can force a "not found" case without
// depending on whatever's actually installed on the test machine.
type binLookup func(harness string) (string, error)

// harnessReport is one harness's folded-together probe result — the single
// piece of data both the CLI text renderer and (if ever wanted) an HTTP
// handler would need.
type harnessReport struct {
	Name       string
	BinaryPath string // "" if not found
	BinaryErr  error
	Auth       authState
	Model      string
	ModelErr   error
}

// probeHarness runs binary/auth/model probes for one harness. If the binary
// isn't found, auth/model are reported "unknown, binary missing" instead of
// spawning a subprocess that can only fail.
func probeHarness(ctx context.Context, run cliRunner, lookup binLookup, harness string) harnessReport {
	rep := harnessReport{Name: harness}
	rep.BinaryPath, rep.BinaryErr = lookup(harness)
	if rep.BinaryErr != nil {
		rep.Auth = authState{"unknown", "binary missing"}
		rep.ModelErr = fmt.Errorf("binary missing")
		return rep
	}
	rep.Auth = probeAuth(ctx, run, harness)
	rep.Model, rep.ModelErr = probeModels(ctx, run, harness)
	return rep
}

// runDoctor prints the plain-text health report to stdout. args is unused
// (no flags today) but kept to match main.go's subcommand signature.
func runDoctor(args []string) error {
	writeDoctor(os.Stdout, context.Background(), realCLI, probeBinary)
	return nil
}

// writeDoctor renders one block per harness:
//
//	<harness>:
//	  binary: found (<path>) | not found on PATH
//	  auth:   <state> (<detail>)
//	  model:  <signal> | unknown (<reason>)
//
// Split from runDoctor so tests can inject a fake cliRunner/binLookup and
// assert on a buffer instead of stdout — no real subprocess spawned.
func writeDoctor(w io.Writer, ctx context.Context, run cliRunner, lookup binLookup) {
	for _, h := range harnessNames {
		rep := probeHarness(ctx, run, lookup, h)
		fmt.Fprintf(w, "%s:\n", rep.Name)
		if rep.BinaryErr != nil {
			fmt.Fprintf(w, "  binary: not found on PATH\n")
		} else {
			fmt.Fprintf(w, "  binary: found (%s)\n", rep.BinaryPath)
		}
		fmt.Fprintf(w, "  auth:   %s (%s)\n", rep.Auth.State, rep.Auth.Detail)
		if rep.ModelErr != nil {
			fmt.Fprintf(w, "  model:  unknown (%s)\n", rep.ModelErr)
		} else {
			fmt.Fprintf(w, "  model:  %s\n", rep.Model)
		}
	}
}
