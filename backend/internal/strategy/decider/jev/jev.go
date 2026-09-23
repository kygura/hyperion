// Package jev is the net/http client for TypeSafe's System One endpoint
// (POST {base_url}/v1/systemone). Request {model, state, questions}, response
// {model, answers, usage}. 5 s timeout, one retry on 408 / 429 / 529 / 5xx /
// timeout / network error honouring Retry-After (capped at 2 s), never on
// other 4xx (the 4xx body is surfaced in the error). Failures carry the
// x-typesafe-request-id header. Alternative transports (Vercel AI Gateway,
// OpenRouter) are a base_url + model change.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/strategy/decider"
)

// DefaultBaseURL is TypeSafe's public endpoint.
const DefaultBaseURL = "https://api.typesafe.ai"

// DefaultModel is the alias TypeSafe resolves to the current release.
const DefaultModel = "jev-latest"

// Timeout bounds one HTTP attempt.
const Timeout = 5 * time.Second

// MaxRetryAfter caps how long a Retry-After header can make the retry wait.
const MaxRetryAfter = 2 * time.Second

// defaultBackoff is the retry wait when the server sends no Retry-After.
const defaultBackoff = 200 * time.Millisecond

// RequestIDHeader is the response header TypeSafe stamps on every reply.
const RequestIDHeader = "x-typesafe-request-id"

// Client is one configured endpoint + model. Safe for concurrent use.
type Client struct {
	baseURL string
	model   string
	key     func() string
	http    *http.Client
	retries int
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying http.Client (tests, custom transports).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithKey uses a literal API key instead of reading an environment variable.
func WithKey(key string) Option { return func(c *Client) { c.key = func() string { return key } } }

// WithRetries sets the number of retries after the first attempt (default 1).
func WithRetries(n int) Option { return func(c *Client) { c.retries = n } }

// New builds a client. apiKeyEnv names the environment variable holding the
// bearer key; it is read per request so a key exported after startup works.
func New(baseURL, model, apiKeyEnv string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	if apiKeyEnv == "" {
		apiKeyEnv = "TYPESAFE_API_KEY"
	}
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		key:     func() string { return os.Getenv(apiKeyEnv) },
		http:    &http.Client{Timeout: Timeout},
		retries: 1,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Model returns the configured model id.
func (c *Client) Model() string { return c.model }

// Available implements decider.Availability: an empty key is a configuration
// error, reported before any request is attempted.
func (c *Client) Available() error {
	if c.key() == "" {
		return fmt.Errorf("%w: no API key", decider.ErrUnavailable)
	}
	return nil
}

type request struct {
	Model     string                      `json:"model"`
	State     any                         `json:"state"`
	Questions map[string]decider.Question `json:"questions"`
}

type response struct {
	Model   string                    `json:"model"`
	Answers map[string]decider.Answer `json:"answers"`
	Usage   decider.Usage             `json:"usage"`
}

// StatusError is a non-2xx response; Body carries the (truncated) response
// body so a 4xx validation message reaches the operator, RequestID the
// x-typesafe-request-id header for support, RetryAfter the parsed header.
type StatusError struct {
	Status     int
	Body       string
	RequestID  string
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("jev: status %d (request %s): %s", e.Status, e.RequestID, e.Body)
	}
	return fmt.Sprintf("jev: status %d: %s", e.Status, e.Body)
}

// Evaluate posts one systemone request and decodes the answers.
func (c *Client) Evaluate(ctx context.Context, state any, questions map[string]decider.Question) (decider.Result, error) {
	if err := c.Available(); err != nil {
		return decider.Result{}, err
	}
	for k, q := range questions {
		if err := q.Validate(); err != nil {
			return decider.Result{}, fmt.Errorf("jev: question %q: %w", k, err)
		}
	}
	body, err := json.Marshal(request{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return decider.Result{}, fmt.Errorf("jev: marshal: %w", err)
	}

	start := time.Now()
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return decider.Result{}, ctx.Err()
			case <-time.After(backoff(lastErr)):
			}
		}
		resp, err := c.once(ctx, body)
		if err == nil {
			res := decider.Result{
				Answers:   resp.Answers,
				Model:     resp.Model,
				Usage:     resp.Usage,
				LatencyMs: time.Since(start).Milliseconds(),
			}
			if res.Model == "" {
				res.Model = c.model
			}
			for k := range questions {
				if _, ok := res.Answers[k]; !ok {
					return decider.Result{}, fmt.Errorf("jev: response missing answer for %q", k)
				}
			}
			return res, nil
		}
		lastErr = err
		if !retryable(err) {
			break
		}
	}
	return decider.Result{}, lastErr
}

// once performs a single HTTP attempt.
func (c *Client) once(ctx context.Context, body []byte) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jev: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jev: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	reqID := resp.Header.Get(RequestIDHeader)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return nil, &StatusError{
			Status:     resp.StatusCode,
			Body:       msg,
			RequestID:  reqID,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("jev: decode (request %s): %w", reqID, err)
	}
	if out.Answers == nil {
		return nil, fmt.Errorf("jev: response has no answers (request %s)", reqID)
	}
	return &out, nil
}

// parseRetryAfter reads a Retry-After header as delay-seconds or an HTTP
// date; 0 when absent or unparseable.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(h, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoff is the wait before the retry: Retry-After when the failed attempt
// carried one, capped at MaxRetryAfter, else a short fixed pause.
func backoff(err error) time.Duration {
	var se *StatusError
	if errors.As(err, &se) && se.RetryAfter > 0 {
		if se.RetryAfter > MaxRetryAfter {
			return MaxRetryAfter
		}
		return se.RetryAfter
	}
	return defaultBackoff
}

// retryable: 408 / 429 / 529 / 5xx, timeouts and transport errors retry once;
// other 4xx and decode errors do not (the same request would fail the same
// way).
func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		switch {
		case se.Status >= 500:
			return true
		case se.Status == http.StatusRequestTimeout, se.Status == http.StatusTooManyRequests:
			return true
		}
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true // timeout or transport failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// url.Error wrapping a transport failure (connection refused, EOF).
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "EOF") {
		return true
	}
	return false
}
