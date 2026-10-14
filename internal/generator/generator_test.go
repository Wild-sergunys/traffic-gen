package generator

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// TestNewRequest verifies that explicit paths, queries and user agents are preserved.
func TestNewRequest(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		ua        string
		wantPath  string
		wantQuery string
		wantUA    string
	}{
		{
			name: "root_with_query", target: "http://example.com/?device=7", ua: "UA-test",
			wantPath: "/", wantQuery: "device=7", wantUA: "UA-test",
		},
		{
			name: "only_host", target: "http://example.com/", ua: "UA-test",
			wantPath: "/", wantQuery: "", wantUA: "UA-test",
		},
		{
			name: "host+path+query", target: "http://example.com/api?device=7", ua: "another-agent",
			wantPath: "/api", wantQuery: "device=7", wantUA: "another-agent",
		},
		{
			name: "query_without_path", target: "http://example.com?device=7", ua: "UA-test",
			wantPath: "", wantQuery: "device=7", wantUA: "UA-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			g := Generator{}

			u, err := url.Parse(tt.target)
			if err != nil {
				t.Fatalf("cannot parse url: %v", err)
			}

			g.target = u

			req, err := g.newRequest(ctx, tt.ua)
			if err != nil {
				t.Fatalf("newRequest returned an error: %v", err)
			}

			if req.Method != http.MethodGet {
				t.Errorf("method = %q, want: %q", req.Method, http.MethodGet)
			}

			if req.URL.Path != tt.wantPath {
				t.Errorf("path = %q, want: %q", req.URL.Path, tt.wantPath)
			}

			if req.URL.RawQuery != tt.wantQuery {
				t.Errorf("query = %q, want: %q", req.URL.RawQuery, tt.wantQuery)
			}

			if req.Header.Get("User-Agent") != tt.wantUA {
				t.Errorf("user-agent = %q, want: %q", req.Header.Get("User-Agent"), tt.wantUA)
			}
		})
	}
}

// TestNewRequestRandomPath verifies the generated path format and target preservation.
func TestNewRequestRandomPath(t *testing.T) {
	target, ua := "http://example.com", "UA-test"
	ctx := context.Background()
	g := Generator{}

	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("cannot parse url: %v", err)
	}

	g.target = u
	// Copy the value: a shared pointer would hide mutations of the original URL.
	targetCopy := *g.target

	req, err := g.newRequest(ctx, ua)
	if err != nil {
		t.Fatalf("newRequest returned an error: %v", err)
	}

	if req.Method != http.MethodGet {
		t.Errorf("method = %q, want: %q", req.Method, http.MethodGet)
	}

	isHex, hasPrefix := strings.CutPrefix(req.URL.Path, "/")
	if !hasPrefix {
		t.Errorf("incorrect path: got = %q, want = %q", req.URL.Path, "/...")
	}

	_, err = strconv.ParseUint(isHex, 16, 64)
	if err != nil {
		t.Errorf("number is not hex: path = %q, want: %q", isHex, "hex")
	}

	if req.URL.RawQuery != "" {
		t.Errorf("query = %q, want: \"\"", req.URL.RawQuery)
	}

	if req.Header.Get("User-Agent") != ua {
		t.Errorf("user-agent = %q, want: %q", req.Header.Get("User-Agent"), ua)
	}

	if targetCopy != *g.target {
		t.Errorf("target is not saved: start target = %+v, finish target = %+v", targetCopy, g.target)
	}
}

// TestNew verifies target parsing, exclusive protocol selection and rejection of unknown modes.
func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		target    string
		wantHTTP1 bool
		wantHTTP2 bool
		wantH2C   bool
		wantErr   bool
	}{
		{
			name: "http1", mode: "http1", target: "http://example.com", wantHTTP1: true,
		},
		{
			name: "http2", mode: "http2", target: "https://example.com", wantHTTP2: true,
		},
		{
			name: "h2c", mode: "h2c", target: "http://example.com", wantH2C: true,
		},
		{
			name: "invalid_mode", mode: "invalid", target: "http://example.com", wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Target:   tt.target,
				Mode:     tt.mode,
				Workers:  1,
				Duration: time.Second,
			}

			g, err := New(&cfg)

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want: %+v", err, tt.wantErr)
			}

			if tt.wantErr {
				if g != nil {
					t.Errorf("generator must be nil on error")
				}
				return
			}

			transport, ok := g.client.Transport.(*http.Transport)

			if !ok {
				t.Fatal("cannot get transport")
			}

			if transport == nil {
				t.Fatal("transport == nil")
			}

			if transport.Protocols == nil {
				t.Fatal("Protocols == nil")
			}

			if transport.Protocols.HTTP1() != tt.wantHTTP1 {
				t.Errorf("HTTP1 = %v, want: %+v", transport.Protocols.HTTP1(), tt.wantHTTP1)
			}

			if transport.Protocols.HTTP2() != tt.wantHTTP2 {
				t.Errorf("HTTP2 = %v, want: %+v", transport.Protocols.HTTP2(), tt.wantHTTP2)
			}

			if transport.Protocols.UnencryptedHTTP2() != tt.wantH2C {
				t.Errorf("H2C = %v, want: %+v", transport.Protocols.UnencryptedHTTP2(), tt.wantH2C)
			}

			if g.target.String() != tt.target {
				t.Errorf("target = %q, want: target = %q", g.target.String(), tt.target)
			}
		})
	}
}
