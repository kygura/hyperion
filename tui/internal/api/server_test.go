package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/store"
)

// testDeps builds minimal Deps for scaffold tests: a real bus (so the cache
// goroutine has something to subscribe to), a real store rooted in a temp
// dir, and no Engine/Exec — the scaffold must degrade gracefully when those
// are nil rather than panic.
func testDeps(t *testing.T, mutate func(*config.Config)) Deps {
	t.Helper()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg)
	}
	return Deps{
		Bus:     bus.New(),
		Store:   st,
		Engine:  nil,
		Exec:    nil,
		Cfg:     cfg,
		Version: "test",
	}
}

func TestHealthReturnsExpectedShape(t *testing.T) {
	s := NewServer(testDeps(t, nil))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Connected bool   `json:"connected"`
		Mode      string `json:"mode"`
		Providers struct {
			Batch string `json:"batch"`
			Chat  string `json:"chat"`
		} `json:"providers"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "test" {
		t.Errorf("version = %q, want %q", body.Version, "test")
	}
	// Engine is nil in this Deps: providers must come back as empty strings,
	// not panic and not be omitted from the JSON shape.
	if body.Providers.Batch != "" || body.Providers.Chat != "" {
		t.Errorf("providers = %+v, want empty (nil engine)", body.Providers)
	}
}

func TestAuthRequiresBearerTokenWhenConfigured(t *testing.T) {
	s := NewServer(testDeps(t, func(c *config.Config) { c.API.Token = "s3cret" }))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	cases := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing header rejected", "", http.StatusUnauthorized},
		{"correct bearer token accepted", "Bearer s3cret", http.StatusOK},
		{"wrong bearer token rejected", "Bearer wrong", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				var body map[string]string
				json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] != "unauthorized" {
					t.Errorf("error = %q, want %q", body["error"], "unauthorized")
				}
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	s := NewServer(testDeps(t, func(c *config.Config) {
		c.API.CORSOrigins = []string{"http://localhost:5173"}
	}))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	t.Run("allowed origin gets 204 with echoed header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodOptions, srv.URL+"/api/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "http://localhost:5173")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("Access-Control-Allow-Origin = %q, want echoed origin", got)
		}
	})

	t.Run("disallowed origin gets no CORS headers", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodOptions, srv.URL+"/api/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "http://evil.example")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
		}
	})
}
