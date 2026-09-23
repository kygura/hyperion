// The WS bridge: consumes the daemon's /api/ws push stream and forwards the
// strategy.* frames (PROTOCOL.md) into the Bubble Tea program. Same
// reconnect/backoff shape as the cockpit's PumpWS; it holds no cache of its
// own because every strategy event is state the model owns directly.
package operator

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Sender is the minimal interface the bridge needs from a tea.Program.
type Sender interface {
	Send(tea.Msg)
}

const healthyConnDuration = 3 * time.Second

func nextBackoff(prevBackoff, upDuration, maxBackoff time.Duration) time.Duration {
	if upDuration >= healthyConnDuration {
		return time.Second
	}
	next := prevBackoff * 2
	if next > maxBackoff {
		next = maxBackoff
	}
	return next
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// PumpWS connects to the daemon's /api/ws and forwards strategy.* frames as
// messages until ctx is cancelled, reconnecting with capped exponential
// backoff. It sends connMsg{true} after every successful dial — Update
// answers that with a fresh REST snapshot, so frames missed while
// disconnected are repaired — and connMsg{false} on every loss.
//
// httpBaseURL is the daemon's HTTP base URL; the ws(s) URL is derived.
func PumpWS(ctx context.Context, httpBaseURL string, p Sender) {
	wsURL := wsURLFrom(httpBaseURL)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		dialStart := time.Now()
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			backoff = nextBackoff(backoff, 0, maxBackoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			continue
		}
		p.Send(connMsg{Connected: true})
		readLoop(ctx, conn, p)
		conn.Close()
		p.Send(connMsg{Connected: false})
		up := time.Since(dialStart)
		if ctx.Err() != nil {
			return
		}
		backoff = nextBackoff(backoff, up, maxBackoff)
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, p Sender) {
	for {
		if ctx.Err() != nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msg := frameToMsg(data); msg != nil {
			p.Send(msg)
		}
	}
}

// frameToMsg turns one raw WS frame into the model's message for it, or nil
// for legacy topics (bar, mids, verdict, journal, thesis, status) and
// undecodable frames. Pure so it can be tested without a socket.
func frameToMsg(data []byte) tea.Msg {
	var e apiclient.Envelope
	if json.Unmarshal(data, &e) != nil {
		return nil
	}
	payload, ok, err := apiclient.DecodeStrategyEvent(e)
	if !ok || err != nil {
		return nil
	}
	switch v := payload.(type) {
	case apiclient.DecisionRecord:
		return decisionMsg(v)
	case apiclient.StrategyVerdictEvent:
		return verdictMsg(v)
	case apiclient.StrategyStatus:
		return configMsg(v)
	case apiclient.GovernorSettings:
		return governorMsg(v)
	case apiclient.VenueStatus:
		return venueMsg(v)
	}
	return nil
}

func wsURLFrom(httpBaseURL string) string {
	u, err := url.Parse(httpBaseURL)
	if err != nil {
		log.Printf("operator: bad base url %q: %v", httpBaseURL, err)
		return httpBaseURL
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/ws"
	return u.String()
}
