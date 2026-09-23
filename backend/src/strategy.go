// strategy: `hyperagent strategy list` and `hyperagent strategy run <id>
// --dry-run [--decider fake]` — the CLI face of the strategy runtime
// (docs/jev/SPEC.md). `run` prints the resulting DecisionRecord as JSON so
// an operator (or a script) can see exactly what a strategy would do right
// now, without the daemon. Markets come from Hyperliquid's public info API
// when reachable, else from the builtin fixture tape (flagged on stderr).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/builtin"
	"github.com/hyperagent/hyperagent/internal/strategy/decider"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/fake"
	"github.com/hyperagent/hyperagent/internal/strategy/decider/jev"
	"github.com/hyperagent/hyperagent/internal/strategy/registry"
	"github.com/hyperagent/hyperagent/internal/strategy/runtime"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
	hlvenue "github.com/hyperagent/hyperagent/internal/strategy/venue/hyperliquid"
	"github.com/hyperagent/hyperagent/internal/strategy/venue/paper"
)

// runStrategy dispatches the `strategy` subcommand.
func runStrategy(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hyperagent strategy list | run <id> --dry-run [--decider fake] [--config config.toml]")
	}
	switch args[0] {
	case "list":
		return strategyList(os.Stdout)
	case "run":
		return strategyRun(args[1:], os.Stdout, os.Stderr)
	}
	return fmt.Errorf("strategy: unknown subcommand %q (want list|run)", args[0])
}

// strategyList prints one line per registered strategy.
func strategyList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tVERSION\tCADENCE\tVENUES\tMARKETS\tQUESTIONS")
	for _, name := range registry.Names() {
		s, err := registry.Get(name)
		if err != nil {
			return err
		}
		m := s.Manifest()
		qs := make([]string, 0, len(m.Questions))
		for k := range m.Questions {
			qs = append(qs, k)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n", m.ID, m.Name, m.Version, m.Cadence,
			strings.Join(m.Venues, ","), strings.Join(m.Markets, ","), len(qs))
	}
	return tw.Flush()
}

// strategyRun runs one tick of a strategy. Only --dry-run is supported from
// the CLI: a live run belongs to the daemon, where the governor, the
// executor's risk gates and the journal are wired.
func strategyRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("strategy run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "snapshot + decide only; never touches the governor or a venue (required)")
	deciderKind := fs.String("decider", "", "override strategy.decider.kind: jev | fake")
	configPath := fs.String("config", "config.toml", "path to config.toml")
	testnet := fs.Bool("testnet", false, "use Hyperliquid testnet for market data")
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return fmt.Errorf("usage: hyperagent strategy run <id> --dry-run [--decider fake]")
	}
	if !*dryRun {
		return fmt.Errorf("strategy run: only --dry-run is supported from the CLI; live runs go through the daemon")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *deciderKind != "" {
		cfg.Strategy.Decider.Kind = *deciderKind
	}
	d, err := buildDecider(cfg.Strategy.Decider)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Paper venue fed by live Hyperliquid marks when reachable, else the
	// builtin fixture tape so the dry run works offline.
	apiURL := hlclient.MainnetAPI
	if *testnet {
		apiURL = hlclient.TestnetAPI
	}
	hl := hlvenue.New(hlclient.New(apiURL), os.Getenv("HL_MASTER_ADDRESS"))
	pv := paper.New()
	probe, probeCancel := context.WithTimeout(ctx, 4*time.Second)
	live, err := hl.Markets(probe, nil)
	probeCancel()
	if err != nil || len(live) == 0 {
		fmt.Fprintf(stderr, "hyperliquid market data unavailable (%v); using builtin fixture marks\n", err)
		pv.SetMarks(builtin.FixtureMarkets()...)
	} else {
		pv.SetMarks(live...)
	}
	venues := map[string]venue.Venue{"paper": pv, "hyperliquid": hl}

	// Force the paper venue for the dry run: hyperliquid needs a master
	// address for positions and a signer for anything else. Configs on a
	// venue the CLI does not wire (e.g. monad) also fall back to paper so
	// runtime validation does not refuse the whole set.
	configs := cfg.Strategy.Configs
	found := false
	for i := range configs {
		if configs[i].ID == id {
			configs[i].Venue = "paper"
			found = true
		} else if _, ok := venues[configs[i].Venue]; !ok {
			configs[i].Venue = "paper"
		}
	}
	if !found {
		configs = append(configs, strategy.StrategyConfig{ID: id, Venue: "paper"})
	}

	store, err := runtime.NewDecisionStore("") // memory-only: the daemon owns decisions_dir
	if err != nil {
		return err
	}
	strategies := registry.All()
	if _, ok := strategies[id]; !ok {
		return fmt.Errorf("strategy run: unknown strategy %q (known: %s)", id, strings.Join(registry.Names(), ", "))
	}
	rt, err := runtime.New(runtime.Config{
		Strategies: strategies,
		Venues:     venues,
		Decider:    d,
		Governor:   runtime.NewGovernor(cfg.Strategy.Governor, store.OpenCount),
		Store:      store,
		Configs:    configs,
	})
	if err != nil {
		return err
	}
	rec, err := rt.RunOnce(ctx, id, true)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rec)
}

// buildDecider constructs the decider the [strategy.decider] section names.
// Shared with the daemon's startup wiring.
func buildDecider(c config.StrategyDecider) (decider.Decider, error) {
	switch c.Kind {
	case "fake":
		return fake.New(nil), nil
	case "jev", "":
		return jev.New(c.BaseURL, c.Model, c.APIKeyEnv), nil
	}
	return nil, fmt.Errorf("strategy.decider.kind must be jev|fake, got %q", c.Kind)
}
