package generator

import (
	"context"
	"net/http"
	"strings"
)

// webRequests is a deterministic synthetic workload, not a browser session model.
// Ordinary punctuation and technical text exercise overly broad detection rules.
var webRequests = []struct{ method, path, parameter, value string }{
	{http.MethodGet, "/health", "check", "ready"},
	{http.MethodGet, "/search", "q", "temperature sensor"},
	{http.MethodGet, "/search", "q", "O'Reilly maintenance manual"},
	{http.MethodGet, "/search", "q", "pressure < 10 & temperature > 20"},
	{http.MethodPost, "/feedback", "message", "Pump A&B: efficiency 95% + spare parts"},
	{http.MethodPost, "/feedback", "message", `Operator said "restart after inspection".`},
	{http.MethodGet, "/docs", "q", "HTML <div> and SQL SELECT examples"},
	{http.MethodPost, "/search", "q", "датчик температуры №7"},
}

func (g *Generator) newWebRequest(ctx context.Context, userAgent string) (*http.Request, error) {
	n := g.backgroundNext.Add(1) - 1
	entry := webRequests[n%uint64(len(webRequests))]
	target := *g.target
	// Append below the configured base path, preserving escaped path segments.
	target.Path = strings.TrimRight(g.target.Path, "/") + entry.path
	target.RawPath = strings.TrimRight(g.target.EscapedPath(), "/") + entry.path
	return newPayloadRequest(ctx, &target, userAgent, entry.method, entry.parameter, entry.value)
}
