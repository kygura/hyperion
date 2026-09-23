package registry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy"
	"github.com/hyperagent/hyperagent/internal/strategy/venue"
)

// EXPERIMENTAL: the stdio plugin adapter lets a non-Go strategy plug into
// the runtime over a child process's stdin/stdout. One JSON request line
// in, one JSON response line out:
//
//	→ {"method":"manifest","params":{}}
//	← {"result": Manifest}
//	→ {"method":"snapshot","params":{"markets":[Market],"positions":[Position],"params":{...}}}
//	← {"result": State}
//	→ {"method":"decide","params":{"state":State,"answers":{...},"params":{...}}}
//	← {"result": [Intent]}
//	← {"error":"message"}                      (any method)
//
// The child sees markets/positions already fetched from the venue, so it
// needs no venue access of its own. This scaffold runs one process per
// call (ProcessRunner); a long-lived pipe is a later optimisation behind the
// same Runner signature.

// Runner sends one request line to the plugin and returns its response
// line. Injected so tests can drive the protocol without a real process.
type Runner func(ctx context.Context, request []byte) ([]byte, error)

type rpcRequest struct {
	Method string `json:"method"`
	Params any    `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// StdioPlugin is a Strategy backed by an external process.
type StdioPlugin struct {
	run      Runner
	manifest strategy.Manifest
	timeout  time.Duration
}

// NewStdio performs the manifest handshake through run and returns the
// plugin. The manifest is fetched once; it is static by contract.
func NewStdio(ctx context.Context, run Runner) (*StdioPlugin, error) {
	p := &StdioPlugin{run: run, timeout: 10 * time.Second}
	raw, err := p.call(ctx, "manifest", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("stdio plugin: manifest handshake: %w", err)
	}
	var m strategy.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("stdio plugin: manifest decode: %w", err)
	}
	if m.ID == "" {
		return nil, fmt.Errorf("stdio plugin: manifest has no id")
	}
	for k, q := range m.Questions {
		if err := q.Validate(); err != nil {
			return nil, fmt.Errorf("stdio plugin: question %q: %w", k, err)
		}
	}
	p.manifest = m
	return p, nil
}

// ProcessRunner returns a Runner that spawns command per call, writes the
// request line to stdin and reads the first line of stdout.
func ProcessRunner(command string, args ...string) Runner {
	return func(ctx context.Context, request []byte) ([]byte, error) {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Stdin = bytes.NewReader(append(request, '\n'))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %s", command, err, strings.TrimSpace(stderr.String()))
		}
		line, err := bufio.NewReader(bytes.NewReader(out)).ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		return bytes.TrimSpace(line), nil
	}
}

func (p *StdioPlugin) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req, err := json.Marshal(rpcRequest{Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	raw, err := p.run(cctx, req)
	if err != nil {
		return nil, err
	}
	var resp rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(raw), &resp); err != nil {
		return nil, fmt.Errorf("%s: malformed response: %w", method, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s: plugin error: %s", method, resp.Error)
	}
	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("%s: response has no result", method)
	}
	return resp.Result, nil
}

// Manifest implements strategy.Strategy.
func (p *StdioPlugin) Manifest() strategy.Manifest { return p.manifest }

// Snapshot implements strategy.Strategy: fetches markets and positions here
// and hands the plugin plain data.
func (p *StdioPlugin) Snapshot(ctx context.Context, v venue.Venue, params strategy.Params) (strategy.State, error) {
	markets, err := v.Markets(ctx, params.Markets(p.manifest))
	if err != nil {
		return nil, err
	}
	positions, err := v.Positions(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := p.call(ctx, "snapshot", map[string]any{
		"markets":   markets,
		"positions": positions,
		"params":    params,
	})
	if err != nil {
		return nil, err
	}
	var state any
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("snapshot: state decode: %w", err)
	}
	return state, nil
}

// Decide implements strategy.Strategy. The interface is pure (no error), so
// a plugin failure here yields no intents and a log line; the decision
// record still lands with the answers.
func (p *StdioPlugin) Decide(state strategy.State, a strategy.Answers, params strategy.Params) []strategy.Intent {
	raw, err := p.call(context.Background(), "decide", map[string]any{
		"state":   state,
		"answers": a,
		"params":  params,
	})
	if err != nil {
		log.Printf("stdio plugin %s: decide: %v", p.manifest.ID, err)
		return nil
	}
	var intents []strategy.Intent
	if err := json.Unmarshal(raw, &intents); err != nil {
		log.Printf("stdio plugin %s: decide: intents decode: %v", p.manifest.ID, err)
		return nil
	}
	return intents
}
