package runner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
	"github.com/wild-sergunys/traffic-gen/internal/runner"
)

func httpScenario(kind, target string) *config.ScenarioConfig {
	c := *httpConfig(target)
	return &config.ScenarioConfig{Type: kind, HTTP: &config.HTTPScenarioConfig{Cfg: c, Methods: []string{"GET", "POST"}, Parameter: "q", Payloads: []string{"a&b + %"}}}
}

// Both traffic sources use the common deadline but retain separate counters.
func TestRunHTTPScenario(t *testing.T) {
	for _, kind := range []string{"sqli", "xss"} {
		for _, mode := range []string{"alone", "combined", "dry_run", "canceled"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var background, scenario atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/background" {
						background.Add(1)
						w.WriteHeader(http.StatusNoContent)
						return
					}
					scenario.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("q") != "a&b + %" {
						t.Errorf("payload = %q", r.Form.Get("q"))
					}
					w.WriteHeader(http.StatusForbidden)
				}))
				t.Cleanup(server.Close)
				cfg := &config.RunConfig{Duration: 100 * time.Millisecond, Scenario: httpScenario(kind, server.URL+"/attack"), DryRun: mode == "dry_run"}
				if mode == "combined" || mode == "dry_run" {
					cfg.Background = httpConfig(server.URL + "/background")
				}
				before := cfg.Scenario.HTTP.Cfg
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				if mode == "canceled" {
					cancel()
				}
				var out bytes.Buffer
				if err := runner.Run(ctx, cfg, &out); err != nil {
					t.Fatal(err)
				}
				if mode != "canceled" && ctx.Err() != nil {
					t.Fatal("Run waited for outer deadline")
				}
				if cfg.Scenario.HTTP.Cfg != before {
					t.Error("caller scenario config mutated")
				}
				var line string
				for l := range strings.SplitSeq(out.String(), "\n") {
					if strings.HasPrefix(l, kind+" final:") {
						line = l
					}
				}
				var sent, failed, s2, s3, s4 int
				if _, err := fmt.Sscanf(line, kind+" final: sent=%d, errors=%d, 2xx=%d, 3xx=%d, 4xx=%d,", &sent, &failed, &s2, &s3, &s4); err != nil {
					t.Fatalf("summary %q: %v", out.String(), err)
				}
				if mode == "dry_run" || mode == "canceled" {
					if sent != 0 || scenario.Load() != 0 || background.Load() != 0 {
						t.Fatalf("unexpected traffic: %q", out.String())
					}
				} else {
					if sent == 0 || s4 == 0 || s2 != 0 || s3 != 0 || sent-failed != s4 {
						t.Errorf("unexpected scenario metrics: %q", line)
					}
					if mode == "combined" {
						var backgroundLine string
						for l := range strings.SplitSeq(out.String(), "\n") {
							if strings.HasPrefix(l, "final: sent=") {
								backgroundLine = l
							}
						}
						var bgSent, bgErrors, bg2xx, bg3xx, bg4xx, bg5xx int
						if _, err := fmt.Sscanf(backgroundLine, "final: sent=%d, errors=%d, 2xx=%d, 3xx=%d, 4xx=%d, 5xx=%d,", &bgSent, &bgErrors, &bg2xx, &bg3xx, &bg4xx, &bg5xx); err != nil {
							t.Fatalf("invalid background summary %q: %v", backgroundLine, err)
						}
						if background.Load() == 0 || bg2xx == 0 || bgSent-bgErrors != bg2xx || bg3xx != 0 || bg4xx != 0 || bg5xx != 0 {
							t.Errorf("unexpected background counters: %q", backgroundLine)
						}
					}
				}
			})
		}
	}
}

func TestRunHTTPScenarioOutputError(t *testing.T) {
	for _, periodic := range []bool{false, true} {
		t.Run(fmt.Sprintf("periodic_%t", periodic), func(t *testing.T) {
			cfg := &config.RunConfig{Duration: time.Minute, Scenario: httpScenario("sqli", "http://127.0.0.1/"), DryRun: true}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if !periodic {
				cancel()
				cfg.DryRun = false
			}
			err := runner.Run(ctx, cfg, &failingWriter{})
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("error = %v", err)
			}
			if periodic && !errors.Is(err, metrics.ErrWriteSnapshot) {
				t.Errorf("missing periodic report error: %v", err)
			}
		})
	}
}

// The shipped examples must decode and run with only their endpoints and duration changed.
func TestHTTPScenarioExamples(t *testing.T) {
	for _, kind := range []string{"sqli", "xss", "control-sqli", "control-xss"} {
		t.Run(kind, func(t *testing.T) {
			data, err := os.ReadFile("../../examples/" + kind + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.DecodeYAML(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get(cfg.Scenario.HTTP.Parameter) == "" {
					t.Error("empty scenario payload")
				}
				requests.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			cfg.Scenario.HTTP.Cfg.Target = server.URL + "/search?device=7"
			cfg.Scenario.HTTP.Cfg.RPS = 0
			// Background is separately covered; avoid sending its example URL over the network.
			cfg.Background = nil
			cfg.Duration = 50 * time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := runner.Run(ctx, cfg, io.Discard); err != nil {
				t.Fatal(err)
			}
			if requests.Load() == 0 {
				t.Fatal("example produced no requests")
			}
		})
	}
}

// A single reporter serializes both sources even when out is not concurrency-safe.
func TestRunCombinedHTTPReports(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cfg := &config.RunConfig{Duration: time.Minute, DryRun: true, Background: httpConfig("http://127.0.0.1/"), Scenario: httpScenario("xss", "http://127.0.0.1/")}
	out := &reportWriter{cancel: cancel}
	if err := runner.Run(ctx, cfg, out); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"sent=", "xss: sent=", "final: sent=", "xss final: sent="} {
		if !strings.Contains(out.String(), prefix) {
			t.Errorf("missing %q in %q", prefix, out.String())
		}
	}
}

// Load the shipped normal-traffic example and exercise the real runner pipeline.
func TestRunWebExample(t *testing.T) {
	data, err := os.ReadFile("../../examples/web-background.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.DecodeYAML(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var gets, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gets.Add(1)
		case http.MethodPost:
			posts.Add(1)
		default:
			t.Errorf("unexpected method %q", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if gets.Load() >= 5 && posts.Load() >= 3 {
			cancel()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg.Background.Target = server.URL + "/?device=7"
	cfg.Background.RPS = 0
	var out bytes.Buffer
	if err := runner.Run(ctx, cfg, &out); err != nil {
		t.Fatal(err)
	}
	if gets.Load() < 5 || posts.Load() < 3 {
		t.Fatalf("GET/POST counts = %d/%d", gets.Load(), posts.Load())
	}
	if !strings.HasPrefix(out.String(), "final: sent=") {
		t.Errorf("output = %q", out.String())
	}
}
