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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
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
		name          string
		duration      time.Duration
		rate          int
		canceled      bool
		ports         int
		failedPorts   int
		wantConnected int
		reason        string
		want          string
	}{
		{name: "complete", duration: time.Minute, ports: 2, wantConnected: 2, want: "portscan: attempted=2 succeeded=2 failed=0\n"},
		{name: "deadline", reason: "deadline", duration: 50 * time.Millisecond, rate: 1, ports: 2, wantConnected: 1, want: "portscan: attempted=1 succeeded=1 failed=0\n"},
		{name: "canceled", reason: "canceled", duration: time.Minute, canceled: true, ports: 2, want: "portscan: attempted=0 succeeded=0 failed=0\n"},
		{name: "mixed_results", duration: time.Minute, ports: 2, failedPorts: 1, wantConnected: 2, want: "portscan: attempted=3 succeeded=2 failed=1\n"},
		{name: "all_failed", duration: time.Minute, failedPorts: 2, want: "portscan: attempted=2 succeeded=0 failed=2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			listener := listenTCP(t)
			scenario := scanConfig(listener)
			port := scenario.Portscan.Ports[0]
			scenario.Portscan.Ports = nil
			for range tt.ports {
				scenario.Portscan.Ports = append(scenario.Portscan.Ports, port)
			}
			if tt.failedPorts > 0 {
				closed := listenTCP(t)
				closedPort := closed.Addr().(*net.TCPAddr).Port
				if err := closed.Close(); err != nil {
					t.Fatalf("close scan target: %v", err)
				}
				for range tt.failedPorts {
					scenario.Portscan.Ports = append(scenario.Portscan.Ports, closedPort)
				}
			}
			scenario.Portscan.ProbesPerSecond = tt.rate
			if tt.canceled {
				cancel()
			}
			var out bytes.Buffer
			cfg := &config.RunConfig{Duration: tt.duration, Scenario: scenario}
			before := time.Now()
			if err := runner.Run(ctx, cfg, &out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !tt.canceled && ctx.Err() != nil {
				t.Errorf("Run waited for the outer context: %v", ctx.Err())
			}
			var connected []string
			for range tt.wantConnected {
				connected = append(connected, strconv.Itoa(port))
			}
			ports := "none"
			if len(connected) > 0 {
				ports = strings.Join(connected, ", ")
			}
			want := "portscan: connected ports: " + ports + "\n" + tt.want
			if !strings.HasPrefix(out.String(), want) {
				t.Errorf("output = %q, want prefix %q", out.String(), want)
			}
			reason := tt.reason
			if reason == "" {
				reason = "completed"
			}
			checkScanTiming(t, strings.TrimPrefix(out.String(), want), scenario.Portscan.Host, reason, before, time.Now())
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
	for _, mode := range []string{"http", "portscan", "portscan_summary", "portscan_timing", "dry_run"} {
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
			writer := &failingWriter{}
			if mode == "portscan_summary" {
				writer.writesLeft = 1
			}
			if mode == "portscan_timing" {
				writer.writesLeft = 2
			}
			if err := runner.Run(ctx, cfg, writer); !errors.Is(err, io.ErrClosedPipe) {
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

type failingWriter struct{ writesLeft int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.writesLeft > 0 {
		w.writesLeft--
		return len(p), nil
	}
	return 0, io.ErrClosedPipe
}

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

// checkScanTiming verifies target, completion reason, and the measured interval.
func checkScanTiming(t *testing.T, line, wantHost, wantReason string, before, after time.Time) time.Time {
	t.Helper()
	var host, started, finished, reason string
	if _, err := fmt.Sscanf(line, "portscan: host=%q started=%s finished=%s reason=%s", &host, &started, &finished, &reason); err != nil {
		t.Fatalf("invalid timing output %q: %v", line, err)
	}
	if host != wantHost || reason != wantReason {
		t.Errorf("host/reason = %q/%q, want %q/%q", host, reason, wantHost, wantReason)
	}
	start, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := time.Parse(time.RFC3339Nano, finished)
	if err != nil {
		t.Fatal(err)
	}
	if start.Before(before) || finish.Before(start) || finish.After(after) {
		t.Errorf("scan interval %s..%s outside run %s..%s", start, finish, before, after)
	}
	return finish
}

// TestRunScanFinishesBeforeBackground checks that a later background deadline
// does not overwrite the scan's earlier completion reason or timestamp.
func TestRunScanFinishesBeforeBackground(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	listener := listenTCP(t)
	cfg := &config.RunConfig{Duration: 150 * time.Millisecond, Background: httpConfig(server.URL), Scenario: scanConfig(listener)}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var out bytes.Buffer
	before := time.Now()
	if err := runner.Run(ctx, cfg, &out); err != nil {
		t.Fatal(err)
	}
	var timing string
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.HasPrefix(line, "portscan: host=") {
			timing = line
		}
	}
	finish := checkScanTiming(t, timing, "127.0.0.1", "completed", before, time.Now())
	if !finish.Before(before.Add(cfg.Duration)) {
		t.Errorf("scan finish %s includes waiting for the HTTP background", finish)
	}
}

// TestRunReportFailure cancels both tasks when periodic output breaks, waits for
// their shutdown, and returns the original writer error instead of hiding it.
func TestRunReportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	listener := listenTCP(t)
	scenario := scanConfig(listener)
	scenario.Portscan.ProbesPerSecond = 1
	for range 20 {
		scenario.Portscan.Ports = append(scenario.Portscan.Ports, scenario.Portscan.Ports[0])
	}
	cfg := &config.RunConfig{Duration: time.Minute, Background: httpConfig(server.URL), Scenario: scenario}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := runner.Run(ctx, cfg, &failingWriter{})
	if !errors.Is(err, metrics.ErrWriteSnapshot) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Run() error = %v, want snapshot and writer errors", err)
	}
	if ctx.Err() != nil {
		t.Errorf("Run waited for the caller deadline: %v", ctx.Err())
	}
}
