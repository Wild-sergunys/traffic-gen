package generator

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

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
			name: "host+path+query", target: "http://example.com/api?device=7", ua: "UA-test",
			wantPath: "/api", wantQuery: "device=7", wantUA: "UA-test",
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

			if req.URL.Path != tt.wantPath {
				t.Errorf("path = %v, want: %q", req.URL.Path, tt.wantPath)
			}

			if req.URL.RawQuery != tt.wantQuery {
				t.Errorf("query = %v, want: %q", req.URL.RawQuery, tt.wantQuery)
			}

			if req.Header.Get("User-Agent") != tt.wantUA {
				t.Errorf("user-agent = %v, want: %q", req.Header.Get("User-Agent"), tt.wantUA)
			}
		})
	}
}

func TestNewRequestRandomPath(t *testing.T) {
	target, ua := "http://example.com", "UA-test"
	ctx := context.Background()
	g := Generator{}

	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("cannot parse url: %v", err)
	}

	g.target = u
	targetCopy := *g.target

	req, err := g.newRequest(ctx, ua)
	if err != nil {
		t.Fatalf("newRequest returned an error: %v", err)
	}

	isHex, hasPrefix := strings.CutPrefix(req.URL.Path, "/")
	if !hasPrefix {
		t.Errorf("incorrect path: got = %q, want = %q", req.URL.Path, "/...")
	}

	_, err = strconv.ParseUint(isHex, 16, 64)
	if err != nil {
		t.Errorf("number is not hex: path = %v, want: %q", isHex, "hex")
	}

	if req.URL.RawQuery != "" {
		t.Errorf("query = %v, want: \"\"", req.URL.RawQuery)
	}

	if req.Header.Get("User-Agent") != ua {
		t.Errorf("user-agent = %v, want: %q", req.Header.Get("User-Agent"), ua)
	}

	if targetCopy != *g.target {
		t.Errorf("target is not saved: start target = %+v, finish target = %+v", targetCopy, g.target)
	}
}
