// Command hyperagent-tui is the standalone terminal client for the
// hyperagent daemon: it holds no backend state of its own, talking
// exclusively over HTTP+WS to a running daemon's unified core API.
//
// The default program is the operator console (internal/operator):
// strategies, decisions, venues, governor. --cockpit runs the legacy chat
// cockpit (internal/cockpit) unchanged.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
	"github.com/hyperagent/tui/internal/cockpit"
	"github.com/hyperagent/tui/internal/operator"
)

func main() {
	coreURL := flag.String("core-url", "http://127.0.0.1:8787", "hyperagent daemon base URL")
	token := flag.String("token", os.Getenv("HYPERAGENT_TOKEN"), "bearer token, if the daemon requires one")
	legacy := flag.Bool("cockpit", false, "run the legacy chat cockpit instead of the operator console")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	client := apiclient.New(*coreURL, *token)

	if *legacy {
		runCockpit(ctx, cancel, *coreURL, client)
		return
	}
	runOperator(ctx, cancel, *coreURL, client)
}

// runOperator starts the operator console. It does not require the daemon
// to be up: the console renders its offline state and retries.
func runOperator(ctx context.Context, cancel context.CancelFunc, coreURL string, client *apiclient.Client) {
	model := operator.New(operator.Config{API: client, CoreURL: coreURL})
	p := tea.NewProgram(model, tea.WithContext(ctx))
	go operator.PumpWS(ctx, coreURL, p)
	if _, err := p.Run(); err != nil {
		cancel()
		log.Fatalf("hyperagent-tui: %v", err)
	}
	cancel()
}

// runCockpit is the legacy chat cockpit, byte for byte the old default.
func runCockpit(ctx context.Context, cancel context.CancelFunc, coreURL string, client *apiclient.Client) {
	cache := apiclient.NewCache()

	settings, err := client.Settings(ctx)
	if err != nil {
		log.Fatalf("hyperagent-tui: could not reach daemon at %s: %v", coreURL, err)
	}

	chatFn := func(ctx context.Context, msg string, history []apiclient.ChatTurn) (string, error) {
		reply, _, _, err := client.Chat(ctx, msg, history)
		return reply, err
	}

	model := cockpit.New(cockpit.Config{
		Cache:    cache,
		Controls: client,
		Settings: settings,
		ChatFn:   chatFn,
	})

	p := tea.NewProgram(model, tea.WithContext(ctx))
	go cockpit.PumpWS(ctx, coreURL, client, cache, p)
	go cockpit.PollMarkets(ctx, client, cache, p)

	if _, err := p.Run(); err != nil {
		cancel()
		log.Fatalf("hyperagent-tui: %v", err)
	}
	cancel()
}
