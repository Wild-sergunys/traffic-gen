package generator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
)

// BenchmarkRun measures full 10-second runs over TLS, including connection setup.
// Use -benchtime=1x; ns/op is time per run, while req/s counts successful requests.
func BenchmarkRun(b *testing.B) {
	cases := []struct {
		name    string
		mode    string
		workers int
	}{
		{name: "http1_4w", mode: "http1", workers: 4},
		{name: "http2_4w", mode: "http2", workers: 4},
		{name: "http1_32w", mode: "http1", workers: 32},
		{name: "http2_32w", mode: "http2", workers: 32},
	}

	for _, tc := range cases {
		// Reuse a fixed payload so response construction does not add per-request work.
		respData := strings.Repeat("@", 1024)
		b.Run(tc.name, func(b *testing.B) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				io.WriteString(w, respData)
			}))
			// Keep TLS in both cases so encryption is not a difference between modes.
			server.EnableHTTP2 = tc.mode == "http2"
			server.StartTLS()
			b.Cleanup(server.Close)

			cfg := config.Config{
				Target:   server.URL + "/",
				Mode:     tc.mode,
				RPS:      0,
				Duration: 10 * time.Second,
				Workers:  tc.workers,
				DryRun:   false,
			}

			g, err := New(&cfg)
			if err != nil {
				b.Fatalf("cannot create Generator: %v", err)
			}

			// Trust the test certificate without disabling certificate verification.
			poolCA := x509.NewCertPool()
			poolCA.AddCert(server.Certificate())

			transport, ok := g.client.Transport.(*http.Transport)
			if !ok {
				b.Fatal("transport is not *http.Transport")
			}

			transport.TLSClientConfig = &tls.Config{RootCAs: poolCA}
			b.Cleanup(g.client.CloseIdleConnections)

			// Exclude fixture setup; initial connections are still established inside Run.
			b.ResetTimer()
			var success, fail int64
			for range b.N {
				// Run is stopped by its context; cfg.Duration alone does not stop workers.
				ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
				m := metrics.Metrics{}

				g.Run(ctx, &m)
				cancel()
				// Run has joined all workers, so no metric writers remain.
				s := m.Snapshot()
				// A failed workload must not appear as a successful performance sample.
				if s.Sent-s.Errors == 0 {
					b.Fatalf("no successful requests: sent=%d, errors=%d", s.Sent, s.Errors)
				}
				success += s.Sent - s.Errors
				fail += s.Errors
			}
			b.StopTimer()

			// Exclude failed attempts from throughput and report them separately.
			b.ReportMetric(float64(success)/b.Elapsed().Seconds(), "req/s")
			b.ReportMetric(float64(fail)/float64(b.N), "errors/run")
		})
	}
}
