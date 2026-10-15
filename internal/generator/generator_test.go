package generator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
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

// TestSend verifies response byte counts and status codes, including HTTP error statuses.
func TestSend(t *testing.T) {
	tests := []struct {
		name             string
		serverStatus     int
		body             string
		wantServerStatus int
		wantBodyLen      int64
	}{
		{
			name: "status_200", serverStatus: 200, body: "hello", wantServerStatus: 200, wantBodyLen: 5,
		},
		{
			name: "status_404", serverStatus: 404, body: "not found", wantServerStatus: 404, wantBodyLen: 9,
		},
		{
			name: "status_503", serverStatus: 503, body: "unavailable", wantServerStatus: 503, wantBodyLen: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)

			g, err := New(&config.Config{Target: server.URL + "/", Mode: "http1",
				Workers: 1, Duration: time.Second})
			if err != nil {
				t.Fatalf("New cannot create Generator: %v", err)
			}

			ctx := context.Background()
			bodyBytes, statusCode, err := g.send(ctx, "UA-test")

			if err != nil {
				t.Fatalf("cannot send request: %v", err)
			}

			if statusCode != tt.wantServerStatus {
				t.Errorf("status = %v, want: status = %v", statusCode, tt.wantServerStatus)
			}

			if bodyBytes != tt.wantBodyLen {
				t.Errorf("bodyBytes = %v, want: bodyBytes = %v", bodyBytes, tt.wantBodyLen)
			}
		})
	}
}

// TestSendCanceled verifies that a pre-canceled request returns no bytes or status.
func TestSendCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, "not found")
	}))
	t.Cleanup(server.Close)

	g, err := New(&config.Config{Target: server.URL + "/", Mode: "http1",
		Workers: 1, Duration: time.Second})
	if err != nil {
		t.Fatalf("New cannot create Generator: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	bodyBytes, statusCode, err := g.send(ctx, "UA-test")

	if statusCode != 0 {
		t.Errorf("status = %v, want: status = %v", statusCode, 0)
	}

	if bodyBytes != 0 {
		t.Errorf("bodyBytes = %v, want: bodyBytes = %v", bodyBytes, 0)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got: err %v, want: %v", err, context.Canceled)
	}
}

// TestSendTruncatedBody verifies that a read failure preserves partial bytes and status.
func TestSendTruncatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Advertise more bytes than we send to force an unexpected EOF.
		w.Header().Set("Content-Length", "20")
		w.WriteHeader(200)
		io.WriteString(w, "hello")

	}))
	t.Cleanup(server.Close)

	g, err := New(&config.Config{Target: server.URL + "/", Mode: "http1",
		Workers: 1, Duration: time.Second})
	if err != nil {
		t.Fatalf("New cannot create Generator: %v", err)
	}

	ctx := context.Background()

	bodyBytes, statusCode, err := g.send(ctx, "UA-test")

	if statusCode != 200 {
		t.Errorf("status = %v, want: status = %v", statusCode, 200)
	}

	if bodyBytes != 5 {
		t.Errorf("bodyBytes = %v, want: bodyBytes = %v", bodyBytes, 5)
	}

	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got: err %v, want: %v", err, io.ErrUnexpectedEOF)
	}
}

// TestSendProtocols verifies the protocol observed by the server in each mode.
func TestSendProtocols(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		tls       bool
		wantProto string
	}{
		{name: "http1", mode: "http1", tls: false, wantProto: "HTTP/1.1"},
		{name: "http2", mode: "http2", tls: true, wantProto: "HTTP/2.0"},
		{name: "h2c", mode: "h2c", tls: false, wantProto: "HTTP/2.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Buffer the observation so the handler can finish before send returns.
			ch := make(chan string, 1)

			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				ch <- r.Proto
				io.WriteString(w, "hello")
			}))
			t.Cleanup(server.Close)

			switch tt.mode {
			case "http1":
				server.Start()
			case "http2":
				server.EnableHTTP2 = true
				server.StartTLS()
			case "h2c":
				var proto http.Protocols
				proto.SetUnencryptedHTTP2(true)
				server.Config.Protocols = &proto
				server.Start()
			default:
				t.Fatal("invalid mode in test data")
			}

			g, err := New(&config.Config{Target: server.URL + "/", Mode: tt.mode,
				Workers: 1, Duration: time.Second})
			if err != nil {
				t.Fatalf("New cannot create Generator: %v", err)
			}
			t.Cleanup(g.client.CloseIdleConnections)

			if tt.tls {
				// Trust the local certificate while retaining normal TLS verification.
				pool := x509.NewCertPool()
				pool.AddCert(server.Certificate())

				transport, ok := g.client.Transport.(*http.Transport)
				if !ok {
					t.Fatal("transport is not *http.Transport")
				}

				transport.TLSClientConfig = &tls.Config{RootCAs: pool}
			}

			ctx := context.Background()

			bodyBytes, statusCode, err := g.send(ctx, "UA-test")

			if err != nil {
				t.Fatalf("got: err %v, want: %v", err, nil)
			}

			if statusCode != 200 {
				t.Errorf("status = %v, want: status = %v", statusCode, 200)
			}

			if bodyBytes != 5 {
				t.Errorf("bodyBytes = %v, want: bodyBytes = %v", bodyBytes, 5)
			}

			proto := <-ch
			if proto != tt.wantProto {
				t.Fatalf("proto = %v, want: proto = %v", proto, tt.wantProto)
			}
		})
	}
}

// TestWork verifies metrics for completed and canceled requests and ensures dry-run sends no traffic.
func TestWork(t *testing.T) {
	tests := []struct {
		name              string
		serverStatus      int
		body              string
		cancelContext     bool
		dryRun            bool
		wantSend          int64
		wantErrors        int64
		wantBytes         int64
		wantStatusClasses [4]int64 // pos of classes: [2xx, 3xx, 4xx, 5xx]
	}{
		{
			name:              "200",
			serverStatus:      200,
			body:              "hello",
			cancelContext:     false,
			dryRun:            false,
			wantSend:          1,
			wantErrors:        0,
			wantBytes:         5,
			wantStatusClasses: [4]int64{0: 1},
		},
		{
			name:              "503",
			serverStatus:      503,
			body:              "unavailable",
			cancelContext:     false,
			dryRun:            false,
			wantSend:          1,
			wantErrors:        0,
			wantBytes:         11,
			wantStatusClasses: [4]int64{3: 1},
		},
		{
			name:              "ctx_done",
			serverStatus:      200,
			body:              "hello",
			cancelContext:     true,
			dryRun:            false,
			wantSend:          1,
			wantErrors:        1,
			wantBytes:         0,
			wantStatusClasses: [4]int64{},
		},
		{
			name:              "dry_run",
			serverStatus:      200,
			body:              "hello",
			cancelContext:     false,
			dryRun:            true,
			wantSend:          0,
			wantErrors:        0,
			wantBytes:         0,
			wantStatusClasses: [4]int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var serverRequests atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			if tt.cancelContext {
				cancel()
			}

			m := &metrics.Metrics{}

			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				io.WriteString(w, tt.body)
				serverRequests.Add(1)
			}))
			t.Cleanup(server.Close)

			server.Start()

			g, err := New(&config.Config{Target: server.URL + "/", Mode: "http1",
				Workers: 1, Duration: time.Second, DryRun: tt.dryRun})
			if err != nil {
				t.Fatalf("New cannot create Generator: %v", err)
			}

			g.work(ctx, m, "UA-test")

			s := m.Snapshot()

			if s.Sent != tt.wantSend {
				t.Errorf("send = %v, want: send = %v", s.Sent, tt.wantSend)
			}

			if s.Errors != tt.wantErrors {
				t.Errorf("errors = %v, want: errors = %v", s.Errors, tt.wantErrors)
			}

			if s.Bytes != tt.wantBytes {
				t.Errorf("bytes = %v, want: bytes = %v", s.Bytes, tt.wantBytes)
			}

			if s.StatusClasses != tt.wantStatusClasses {
				t.Errorf("%+v, want: %+v", s.StatusClasses, tt.wantStatusClasses)
			}

			if tt.serverStatus >= 200 && tt.serverStatus <= 599 {
				if int64(serverRequests.Load()) != tt.wantStatusClasses[(tt.serverStatus/100)-2] {
					t.Errorf("server req = %v, want: server req = %v", serverRequests.Load(), tt.wantStatusClasses[tt.serverStatus/100-2])
				}
			}
		})
	}
}

// TestRunCanceled verifies that Run returns without recording attempts when its context is already canceled, with and without rate limiting.
func TestRunCanceled(t *testing.T) {
	tests := []struct {
		name    string
		rps     int
		workers int
	}{
		{
			name: "unlimited", rps: 0, workers: 3,
		},
		{
			name: "limited", rps: 10, workers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			m := metrics.Metrics{}
			cfg := &config.Config{
				Target:   "http://ex.com",
				Mode:     "http1",
				RPS:      tt.rps,
				Duration: time.Second,
				Workers:  tt.workers,
				DryRun:   false,
			}

			g, err := New(cfg)
			if err != nil {
				t.Fatalf("cannot create new Generator: %v", err)
			}

			ch := make(chan struct{})

			go func(ctx context.Context) {
				ctxRun, cancelRun := context.WithCancel(ctx)
				cancelRun()
				g.Run(ctxRun, &m)
				close(ch)
			}(ctx)

			select {
			case <-ch:
				want := metrics.Snapshot{}
				s := m.Snapshot()
				if s != want {
					t.Errorf("snapshot = %+v, want: snapshot = %+v", s, want)
				}
			case <-ctx.Done():
				t.Error("timeout exit")
			}
		})
	}
}
