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

// Generator sends HTTP/1.1 requests to a single target.
type Generator struct {
	cfg    *config.Config
	target *url.URL
	client *http.Client
}

// userAgents is a pool of real browser strings. A load stream that
// always presents as Go-http-client/1.1 is a giveaway to an IDS.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:126.0) Gecko/20100101 Firefox/126.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.2535.67",
}

// New prepares the target for the given validated config.
// It returns an error if the mode is not implemented yet.
func New(cfg *config.Config) (*Generator, error) {
	if cfg.Mode != "http1" {
		return nil, fmt.Errorf("generator: mode %q not implemented", cfg.Mode)
	}

	u, err := url.Parse(cfg.Target)
	// cfg.Validate already parsed the target, so this cannot fail,
	// but Go requires the error branch to be handled.
	if err != nil {
		return nil, err
	}

	return &Generator{
		cfg:    cfg,
		target: u,
		client: &http.Client{
			Transport: &http.Transport{
				// The pool is sized to the worker count: with the default
				// of two idle connections per host, most workers reopen a
				// connection on every request.
				MaxIdleConns:        cfg.Workers * 2,
				MaxIdleConnsPerHost: cfg.Workers * 2,
				IdleConnTimeout:     30 * time.Second,
				// Compression is disabled so that bytes and CPU reflect
				// the traffic actually sent over the wire.
				DisableCompression: true,
			},
		},
	}, nil
}

// newRequest builds a GET request against cfg.Target with a randomised
// path and User-Agent so the stream does not look like a single client.
func (g *Generator) newRequest(ctx context.Context) (*http.Request, error) {
	u := *g.target
	u.Path = randomPath()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])

	return req, nil
}

// randomPath returns "/" followed by a random hex segment.
func randomPath() string {
	return "/" + fmt.Sprintf("%x", rand.Int63())
}

// send performs one request and returns the number of response body
// bytes read. A 4xx/5xx status counts as an error: the request failed
// even though the transfer itself succeeded.
func (g *Generator) send(ctx context.Context) (int64, error) {
	req, err := g.newRequest(ctx)
	if err != nil {
		return 0, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// The body is drained before the status check: a fully consumed
	// response lets the transport reuse the connection.
	bodyBytes, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return bodyBytes, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return bodyBytes, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return bodyBytes, nil
}

// worker runs one goroutine of the pool until ctx is cancelled. Each
// unit of work is a single request, recorded in m. In dry-run mode work
// never sends. At rps == 0 work runs back-to-back, otherwise it is paced
// by the tickets channel.
func (g *Generator) worker(ctx context.Context, tickets <-chan time.Time, m *metrics.Metrics, wg *sync.WaitGroup) {
	defer wg.Done()

	if g.cfg.RPS == 0 {
		for {
			if ctx.Err() != nil {
				return
			}
			g.work(ctx, m)
		}
	} else {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tickets:
				g.work(ctx, m)
			}
		}
	}
}

// work performs one unit of traffic. In dry-run mode it builds the
// request exactly like a live run, but never sends it; otherwise it
// sends the request and records the outcome in m.
func (g *Generator) work(ctx context.Context, m *metrics.Metrics) {
	if g.cfg.DryRun {
		// Keep the request construction on the live path, so dry-run
		// differs from a real run only in that nothing leaves the process.
		if _, err := g.newRequest(ctx); err != nil {
			return
		}
		return
	}

	start := time.Now()
	sendBytes, err := g.send(ctx)
	latency := time.Since(start)
	m.Record(latency, sendBytes, err)
}

// Run generates traffic until ctx is done. At rps > 0 a time.Ticker
// feeds the ticket channel; rps == 0 runs the workers as fast as they
// can.
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
