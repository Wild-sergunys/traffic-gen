package generator

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
)

// Generator sends HTTP/1.1, HTTP/2 and h2c requests to a single target.
type Generator struct {
	cfg    *config.Config
	target *url.URL
	client *http.Client
}

// userAgents contains browser strings selected once per worker.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.2 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:157.0) Gecko/20100101 Firefox/157.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36 Edg/154.0.0.0",
}

// New prepares the target for the given validated config.
func New(cfg *config.Config) (*Generator, error) {
	var protocol http.Protocols

	switch cfg.Mode {
	case "http1":
		protocol.SetHTTP1(true)
	case "http2":
		protocol.SetHTTP2(true)
	case "h2c":
		protocol.SetUnencryptedHTTP2(true)
	default:
		return nil, fmt.Errorf("generator: mode %q not implemented", cfg.Mode)
	}

	u, err := url.Parse(cfg.Target)
	if err != nil {
		return nil, err
	}

	return &Generator{
		cfg:    cfg,
		target: u,
		client: &http.Client{
			Transport: &http.Transport{
				Protocols: &protocol,
				// Size the idle pool for the workers instead of the default
				// limit of two idle connections per host.
				MaxIdleConns:        cfg.Workers * 2,
				MaxIdleConnsPerHost: cfg.Workers * 2,
				IdleConnTimeout:     30 * time.Second,
				// Disable automatic gzip negotiation and decompression;
				// byte counts include response bodies only, not wire overhead.
				DisableCompression: true,
			},
		},
	}, nil
}

// newRequest builds a GET request against cfg.Target, preserving its path
// and query. With neither a path nor a query, a random path is generated
// for each request.
func (g *Generator) newRequest(ctx context.Context, userAgent string) (*http.Request, error) {
	u := *g.target
	if u.Path == "" && u.RawQuery == "" {
		u.Path = randomPath()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)

	return req, nil
}

// randomPath returns "/" followed by a random hex segment.
func randomPath() string {
	return "/" + fmt.Sprintf("%x", rand.Int63())
}

// send performs one request and returns the number of response body
// bytes read, HTTP status code and error. The HTTP status code is not considered an error.
func (g *Generator) send(ctx context.Context, userAgent string) (bodyBytes int64, statusCode int, err error) {
	req, err := g.newRequest(ctx, userAgent)
	if err != nil {
		return 0, 0, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	// Drain the body so the transport can reuse the connection.
	bBytes, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return bBytes, resp.StatusCode, err
	}

	return bBytes, resp.StatusCode, nil
}

// worker runs one goroutine of the pool until ctx is cancelled. Each
// unit of live work is a request attempt recorded in m. Dry-run work
// sends nothing and records no metrics. At rps == 0 work runs back-to-back;
// otherwise it is paced by the tickets channel.
func (g *Generator) worker(ctx context.Context, tickets <-chan time.Time, m *metrics.Metrics, wg *sync.WaitGroup) {
	defer wg.Done()

	ua := userAgents[rand.Intn(len(userAgents))]

	if g.cfg.RPS == 0 {
		for {
			if ctx.Err() != nil {
				return
			}
			g.work(ctx, m, ua)
		}
	} else {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tickets:
				if ctx.Err() != nil {
					return
				}
				g.work(ctx, m, ua)
			}
		}
	}
}

// work performs one unit of traffic. In dry-run mode it builds the
// request exactly like a live run, but never sends it; otherwise it
// sends the request and records the outcome in m.
func (g *Generator) work(ctx context.Context, m *metrics.Metrics, userAgent string) {
	if g.cfg.DryRun {
		// Keep the request construction on the live path, so dry-run
		// differs from a real run only in that nothing leaves the process.
		if _, err := g.newRequest(ctx, userAgent); err != nil {
			return
		}
		return
	}

	start := time.Now()
	sendBytes, statusCode, err := g.send(ctx, userAgent)
	latency := time.Since(start)
	m.Record(latency, sendBytes, statusCode, err)
}

// Run generates traffic until ctx is done and waits for all workers
// to finish. At rps > 0 one shared ticker paces all workers; ticks may
// be dropped when workers cannot keep up. At rps == 0 workers run as fast
// as they can. The caller controls the run duration through ctx.
func (g *Generator) Run(ctx context.Context, m *metrics.Metrics) {
	wg := sync.WaitGroup{}
	var tickets <-chan time.Time

	if g.cfg.RPS > 0 {
		ticker := time.NewTicker(time.Second / time.Duration(g.cfg.RPS))
		defer ticker.Stop()
		tickets = ticker.C
	}
	for range g.cfg.Workers {
		wg.Add(1)
		go func() {
			g.worker(ctx, tickets, m, &wg)
		}()
	}
	wg.Wait()
}
