package runner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/portscan"
	"github.com/wild-sergunys/traffic-gen/internal/runner"
)

// TestRunHTTP checks active cancellation and that a completed scan does not stop
// the background. Requests wait for the scan when both parts are enabled.
func TestRunHTTP(t *testing.T) {
	for _, withScan := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_scan_%t", withScan), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var requests atomic.Int32
			scanned := make(chan struct{})
			if !withScan {
				close(scanned)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-scanned:
				case <-r.Context().Done():
					return
				}
				if requests.Add(1) == 2 {
					cancel()
				}
				_, _ = io.WriteString(w, "ok")
			}))
			t.Cleanup(server.Close)
			background := httpConfig(server.URL)
			before := *background
			cfg := &config.RunConfig{Duration: time.Minute, Background: background}
			if withScan {
				listener := listenTCP(t)
				cfg.Scenario = scanConfig(listener)
				accepted := make(chan error, 1)
				go func() {
					conn, err := listener.Accept()
					if err == nil {
						err = conn.Close()
						close(scanned)
					}
					accepted <- err
				}()
				t.Cleanup(func() {
					_ = listener.Close()
					if err := <-accepted; err != nil {
						t.Errorf("accept scan: %v", err)
					}
				})
			}

			var out bytes.Buffer
			if err := runner.Run(ctx, cfg, &out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := requests.Load(); got < 2 {
				t.Fatalf("server requests = %d, want at least 2", got)
			}
			if *background != before {
				t.Errorf("Run changed background config: got %+v, want %+v", *background, before)
			}
			var sent, failed int
			if _, err := fmt.Sscanf(out.String(), "final: sent=%d, errors=%d,", &sent, &failed); err != nil {
				t.Fatalf("cannot read HTTP summary %q: %v", out.String(), err)
			}
			if sent < 2 || sent-failed < 1 {
				t.Errorf("summary sent=%d failed=%d, want at least 2 attempts and 1 success", sent, failed)
			}
			if got := strings.Contains(out.String(), "portscan: attempted=1 succeeded=1 failed=0\n"); got != withScan {
				t.Errorf("scan summary present = %t, want %t; output: %q", got, withScan, out.String())
			}
		})
	}
}

// TestRunDuration verifies that the common duration stops HTTP traffic even
// when the caller's context and the nested HTTP duration allow a longer run.
func TestRunDuration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg := &config.RunConfig{Duration: 100 * time.Millisecond, Background: httpConfig(server.URL)}
	if err := runner.Run(ctx, cfg, io.Discard); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Run waited for the outer context: %v", ctx.Err())
	}
	if requests.Load() == 0 {
		t.Error("server received no HTTP requests")
	}
}

// TestRunPortscan checks early completion, partial results at the common
// deadline, and an already canceled context without requiring an HTTP server.
func TestRunPortscan(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		rate     int
		canceled bool
		want     string
	}{
		{name: "complete", duration: time.Minute, want: "portscan: attempted=2 succeeded=2 failed=0\n"},
		{name: "deadline", duration: 50 * time.Millisecond, rate: 1, want: "portscan: attempted=1 succeeded=1 failed=0\n"},
		{name: "canceled", duration: time.Minute, canceled: true, want: "portscan: attempted=0 succeeded=0 failed=0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			listener := listenTCP(t)
			scenario := scanConfig(listener)
			scenario.Portscan.Ports = append(scenario.Portscan.Ports, scenario.Portscan.Ports[0])
			scenario.Portscan.ProbesPerSecond = tt.rate
			if tt.canceled {
				cancel()
			}
			var out bytes.Buffer
			cfg := &config.RunConfig{Duration: tt.duration, Scenario: scenario}
			if err := runner.Run(ctx, cfg, &out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !tt.canceled && ctx.Err() != nil {
				t.Errorf("Run waited for the outer context: %v", ctx.Err())
			}
			if got := out.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRunDryRun leaves the listener unaccepted so any accidental HTTP or scan
// connection remains observable in its backlog after Run returns.
func TestRunDryRun(t *testing.T) {
	for _, mode := range []string{"http", "portscan", "combined"} {
		t.Run(mode, func(t *testing.T) {
			listener := listenTCP(t)
			cfg := &config.RunConfig{Duration: 30 * time.Millisecond, DryRun: true}
			if mode != "portscan" {
				cfg.Background = httpConfig("http://" + listener.Addr().String())
				cfg.Background.DryRun = false
			}
			if mode != "http" {
				cfg.Scenario = scanConfig(listener)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var out bytes.Buffer
			if err := runner.Run(ctx, cfg, &out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !strings.Contains(out.String(), "dry-run: no traffic sent\n") {
				t.Errorf("missing dry-run summary in %q", out.String())
			}
			if strings.Contains(out.String(), "portscan:") {
				t.Errorf("dry-run printed actual scan results: %q", out.String())
			}
			if cfg.Background != nil && !strings.Contains(out.String(), "final: sent=0, errors=0,") {
				t.Errorf("dry-run HTTP metrics are not zero: %q", out.String())
			}
			assertNoConnection(t, listener)
		})
	}
}

// TestRunInvalidConfig verifies validation before any network or output activity.
func TestRunInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		change func(*config.RunConfig)
		want   error
	}{
		{name: "duration", change: func(c *config.RunConfig) { c.Duration = 0 }, want: config.ErrDurationBad},
		{name: "background", change: func(c *config.RunConfig) { c.Background.Workers = 0 }, want: config.ErrWorkersRange},
		{name: "scenario", change: func(c *config.RunConfig) { c.Scenario.Portscan.Timeout = 0 }, want: portscan.ErrInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := listenTCP(t)
			cfg := &config.RunConfig{
				Duration: time.Second, Background: httpConfig("http://" + listener.Addr().String()),
				Scenario: scanConfig(listener),
			}
			tt.change(cfg)
			var out bytes.Buffer
			if err := runner.Run(t.Context(), cfg, &out); !errors.Is(err, tt.want) {
				t.Errorf("Run() error = %v, want %v", err, tt.want)
			}
			if out.Len() != 0 {
				t.Errorf("invalid config produced output: %q", out.String())
			}
			assertNoConnection(t, listener)
		})
	}
}

// TestRunOutputError checks that each final output path preserves writer errors.
func TestRunOutputError(t *testing.T) {
	for _, mode := range []string{"http", "portscan", "dry_run"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cfg := &config.RunConfig{Duration: time.Second, DryRun: mode == "dry_run"}
			if mode == "http" {
				cfg.Background = httpConfig("http://127.0.0.1/")
			} else {
				cfg.Scenario = &config.ScenarioConfig{Type: "portscan", Portscan: &portscan.Config{
					Host: "127.0.0.1", Ports: []int{80}, Timeout: time.Second,
				}}
			}
			if err := runner.Run(ctx, cfg, failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
				t.Errorf("Run() error = %v, want %v", err, io.ErrClosedPipe)
			}
		})
	}
}

// TestRunPeriodicReport cancels on the first intermediate report, proving that
// reporting happens during the run and finishes before the final summary.
func TestRunPeriodicReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	out := &reportWriter{cancel: cancel}
	cfg := &config.RunConfig{Duration: time.Minute, Background: httpConfig(server.URL)}
	if err := runner.Run(ctx, cfg, out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.HasPrefix(out.String(), "sent=") || !strings.Contains(out.String(), "\nfinal: sent=") {
		t.Errorf("want intermediate report followed by final summary, got %q", out.String())
	}
}

// Conflicting nested defaults expose failure to apply common run settings.
func httpConfig(target string) *config.Config {
	return &config.Config{Target: target, Mode: "http1", RPS: 100, Workers: 1, Duration: time.Hour, DryRun: true}
}

func scanConfig(listener net.Listener) *config.ScenarioConfig {
	return &config.ScenarioConfig{Type: "portscan", Portscan: &portscan.Config{
		Host: "127.0.0.1", Ports: []int{listener.Addr().(*net.TCPAddr).Port}, Timeout: time.Second,
	}}
}

func listenTCP(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func assertNoConnection(t *testing.T, listener *net.TCPListener) {
	t.Helper()
	if err := listener.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatalf("set accept deadline: %v", err)
	}
	conn, err := listener.Accept()
	if err == nil {
		_ = conn.Close()
		t.Fatal("unexpected network connection")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("accept error = %v, want timeout without connections", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type reportWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *reportWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if bytes.HasPrefix(p, []byte("sent=")) {
		w.cancel()
	}
	return n, err
}
