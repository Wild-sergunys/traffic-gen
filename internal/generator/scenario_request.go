package generator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

type requestProfile struct {
	methods   []string
	payloads  []string
	parameter string
	next      atomic.Uint64
}

// NewScenario prepares a SQLi or XSS profile using the regular HTTP worker pool.
// Each payload visits all configured methods. Concurrent dispatch order may differ.
func NewScenario(kind string, cfg *config.HTTPScenarioConfig) (*Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	payloads, err := scenarioPayloads(kind, cfg.Payloads)
	if err != nil {
		return nil, err
	}
	base := cfg.Cfg
	g, err := New(&base)
	if err != nil {
		return nil, err
	}
	// Reject malformed queries instead of silently dropping parameters during encoding.
	if _, err := url.ParseQuery(g.target.RawQuery); err != nil {
		return nil, fmt.Errorf("%w: malformed scenario query", config.ErrTargetInvalid)
	}
	g.profile = &requestProfile{methods: slices.Clone(cfg.Methods), payloads: slices.Clone(payloads), parameter: cfg.Parameter}
	// Keep attempts on the configured endpoint and preserve the chosen method.
	g.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return g, nil
}

// newPayloadRequest preserves the target path and unrelated query parameters.
// GET replaces the selected query field; POST leaves the query intact and sends a form.
func newPayloadRequest(ctx context.Context, target *url.URL, userAgent, method, parameter, payload string) (*http.Request, error) {
	u := *target
	var body io.Reader = http.NoBody
	switch method {
	case http.MethodGet:
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed scenario query", config.ErrTargetInvalid)
		}
		query.Set(parameter, payload)
		u.RawQuery = query.Encode()
	case http.MethodPost:
		form := url.Values{parameter: []string{payload}}
		body = strings.NewReader(form.Encode())
	default:
		return nil, config.ErrInvalidMethod
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}
