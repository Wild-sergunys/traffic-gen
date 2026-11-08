package generator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

func scenarioConfig(target string) config.HTTPScenarioConfig {
	return config.HTTPScenarioConfig{Cfg: config.Config{Target: target, Mode: "http1", Workers: 2, Duration: time.Second, RequestTimeout: time.Second}, Methods: []string{http.MethodGet, http.MethodPost}, Parameter: "q", Payloads: []string{"a&b + %", "<script>alert(1)</script>"}}
}

// Every payload visits every method, without altering the shared target or caller input.
func TestScenarioRequests(t *testing.T) {
	cfg := scenarioConfig("http://example.com/search?device=7&q=old&q=older")
	g, err := NewScenario("xss", &cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Methods[0] = "DELETE"
	cfg.Payloads[0] = "changed"
	target := g.target.String()
	for i := range 8 {
		req, err := g.newRequest(t.Context(), "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		method := []string{http.MethodGet, http.MethodPost}[i%2]
		payload := []string{"a&b + %", "<script>alert(1)</script>"}[(i/2)%2]
		if req.Method != method || req.URL.Path != "/search" || req.URL.Query().Get("device") != "7" || req.Header.Get("User-Agent") != "test-agent" {
			t.Fatalf("unexpected request: %v", req)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if method == http.MethodGet {
			if got := req.URL.Query()["q"]; !slices.Equal(got, []string{payload}) {
				t.Errorf("query q = %q, want %q", got, payload)
			}
		} else {
			if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.PostForm.Get("q") != payload || !slices.Equal(req.URL.Query()["q"], []string{"old", "older"}) {
				t.Fatalf("incorrect form/query: %v / %v", req.PostForm, req.URL.Query())
			}
			req.Body.Close()
		}
	}
	if g.target.String() != target {
		t.Error("shared target changed")
	}
}

func TestNewScenarioValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*config.HTTPScenarioConfig)
		want   error
	}{
		{"empty_methods", func(c *config.HTTPScenarioConfig) { c.Methods = nil }, config.ErrMethodsRequired},
		{"empty_payloads", func(c *config.HTTPScenarioConfig) { c.Payloads = []string{} }, config.ErrInvalidPayloads},
		{"bad_query", func(c *config.HTTPScenarioConfig) { c.Cfg.Target = "http://example.com/?q=%ZZ" }, config.ErrTargetInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := scenarioConfig("http://example.com/")
			tt.change(&cfg)
			if _, err := NewScenario("sqli", &cfg); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// Concurrent workers must share a cycle without racing or losing combinations.
func TestScenarioConcurrentRequests(t *testing.T) {
	cfg := scenarioConfig("http://example.com/")
	g, err := NewScenario("sqli", &cfg)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan string, 40)
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			r, err := g.newRequest(t.Context(), "ua")
			if err != nil {
				t.Error(err)
				return
			}
			defer r.Body.Close()
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			results <- r.Method + ":" + r.Form.Get("q")
		})
	}
	wg.Wait()
	close(results)
	counts := make(map[string]int)
	for key := range results {
		counts[key]++
	}
	if len(counts) != 4 {
		t.Fatalf("combinations = %v", counts)
	}
	for key, n := range counts {
		if n != 10 {
			t.Errorf("%q count = %d, want 10", key, n)
		}
	}
}

// Exercise real form decoding and ensure redirects do not move the scenario to another endpoint.
func TestScenarioSend(t *testing.T) {
	for _, kind := range []string{"sqli", "xss"} {
		t.Run(kind, func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				if r.URL.Path != "/search" {
					t.Errorf("followed redirect: %q", r.URL.Path)
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("q") != "a&b + %" || r.URL.Query().Get("device") != "7" {
					t.Errorf("form = %v", r.Form)
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(http.StatusFound)
				_, _ = io.WriteString(w, "ok")
			}))
			t.Cleanup(server.Close)
			cfg := scenarioConfig(server.URL + "/search?device=7")
			cfg.Payloads = []string{"a&b + %"}
			g, err := NewScenario(kind, &cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(g.client.CloseIdleConnections)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			for range 2 {
				n, status, err := g.send(ctx, "ua")
				if err != nil || n != 2 || status != http.StatusFound {
					t.Fatalf("send = %d, %d, %v", n, status, err)
				}
			}
			if received.Load() != 2 {
				t.Errorf("requests = %d, want 2", received.Load())
			}
		})
	}
}
