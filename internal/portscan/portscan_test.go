package portscan

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestProbe verifies successful probes and preservation of context.Canceled
// for attempts started with an already canceled context.
func TestProbe(t *testing.T) {
	tests := []struct {
		name        string
		ctxCanceled bool
		wantErr     error
	}{
		{name: "default_ctx", ctxCanceled: false, wantErr: nil},
		{name: "canceled_ctx", ctxCanceled: true, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.ctxCanceled {
				cancel()
			}

			var lc net.ListenConfig
			listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("cannot get free OS port: %v", err)
			}
			t.Cleanup(func() {
				if err = listener.Close(); err != nil {
					t.Fatalf("cannot close listener: %v", err)
				}
			})

			addr := listener.Addr().String()
			err = probe(ctx, addr, time.Second)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want: error = %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigValidate checks accepted scan settings and invalid input boundaries.
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Config)
		wantErr error
	}{
		{name: "valid_ipv4"},
		{name: "hostname", change: func(c *Config) { c.Host = "example.com" }},
		{name: "localhost", change: func(c *Config) { c.Host = "localhost" }},
		{name: "ipv6", change: func(c *Config) { c.Host = "::1" }},
		{name: "port_boundaries", change: func(c *Config) { c.Ports = []int{1, 65535} }},
		{name: "unlimited_rate", change: func(c *Config) { c.ProbesPerSecond = 0 }},
		{name: "minimum_interval", change: func(c *Config) { c.ProbesPerSecond = 1_000_000_000 }},
		{name: "empty_host", change: func(c *Config) { c.Host = "" }, wantErr: ErrInvalidConfig},
		{name: "blank_host", change: func(c *Config) { c.Host = " \t\n" }, wantErr: ErrInvalidConfig},
		{name: "embedded_space", change: func(c *Config) { c.Host = "example .com" }, wantErr: ErrInvalidConfig},
		{name: "unicode_space", change: func(c *Config) { c.Host = "example\u00a0.com" }, wantErr: ErrInvalidConfig},
		{name: "url_scheme", change: func(c *Config) { c.Host = "http://example.com" }, wantErr: ErrInvalidConfig},
		{name: "path", change: func(c *Config) { c.Host = "example.com/path" }, wantErr: ErrInvalidConfig},
		{name: "query", change: func(c *Config) { c.Host = "example.com?device=7" }, wantErr: ErrInvalidConfig},
		{name: "fragment", change: func(c *Config) { c.Host = "example.com#fragment" }, wantErr: ErrInvalidConfig},
		{name: "userinfo", change: func(c *Config) { c.Host = "user@example.com" }, wantErr: ErrInvalidConfig},
		{name: "host_with_port", change: func(c *Config) { c.Host = "example.com:80" }, wantErr: ErrInvalidConfig},
		{name: "ipv4_with_port", change: func(c *Config) { c.Host = "127.0.0.1:80" }, wantErr: ErrInvalidConfig},
		{name: "bracketed_ipv6", change: func(c *Config) { c.Host = "[::1]" }, wantErr: ErrInvalidConfig},
		{name: "invalid_ipv6", change: func(c *Config) { c.Host = ":::1" }, wantErr: ErrInvalidConfig},
		{name: "nil_ports", change: func(c *Config) { c.Ports = nil }, wantErr: ErrInvalidConfig},
		{name: "empty_ports", change: func(c *Config) { c.Ports = []int{} }, wantErr: ErrInvalidConfig},
		{name: "negative_port", change: func(c *Config) { c.Ports = []int{-1} }, wantErr: ErrInvalidConfig},
		{name: "zero_port", change: func(c *Config) { c.Ports = []int{0} }, wantErr: ErrInvalidConfig},
		{name: "port_above_range", change: func(c *Config) { c.Ports = []int{65536} }, wantErr: ErrInvalidConfig},
		{name: "invalid_later_port", change: func(c *Config) { c.Ports = []int{80, 0} }, wantErr: ErrInvalidConfig},
		{name: "zero_timeout", change: func(c *Config) { c.Timeout = 0 }, wantErr: ErrInvalidConfig},
		{name: "negative_timeout", change: func(c *Config) { c.Timeout = -time.Second }, wantErr: ErrInvalidConfig},
		{name: "negative_rate", change: func(c *Config) { c.ProbesPerSecond = -1 }, wantErr: ErrInvalidConfig},
		{name: "rate_above_limit", change: func(c *Config) { c.ProbesPerSecond = 1_000_000_001 }, wantErr: ErrInvalidConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Host:            "127.0.0.1",
				Ports:           []int{80},
				Timeout:         time.Second,
				ProbesPerSecond: 10,
			}
			if tt.change != nil {
				tt.change(&cfg)
			}

			if err := cfg.Validate(); !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestRun checks result order, continuation after connection errors, and
// pre-canceled contexts with and without pacing; it does not measure the rate.
func TestRun(t *testing.T) {
	tests := []struct {
		name            string
		pps             int
		closeFirst      bool
		cancelBeforeRun bool
		wantErr         error
		wantResults     int
	}{
		{name: "PPS_unlimited", pps: 0, wantResults: 2},
		{name: "PPS_limited", pps: 100, wantResults: 2},
		{name: "closed_first_unlimited", pps: 0, closeFirst: true, wantResults: 2},
		{name: "closed_first_limited", pps: 100, closeFirst: true, wantResults: 2},
		{name: "canceled_unlimited", pps: 0, cancelBeforeRun: true, wantErr: context.Canceled},
		{name: "canceled_limited", pps: 100, cancelBeforeRun: true, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := Config{
				Host:            "127.0.0.1",
				Timeout:         time.Second,
				ProbesPerSecond: tt.pps,
			}

			var firstListener net.Listener
			for i := range 2 {
				var lc net.ListenConfig
				listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("cannot get free OS port: %v", err)
				}

				cfg.Ports = append(cfg.Ports, listener.Addr().(*net.TCPAddr).Port)
				if i == 0 {
					firstListener = listener
				}

				t.Cleanup(func() {
					if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
						t.Fatalf("cannot close listener: %v", err)
					}
				})
			}

			if tt.closeFirst {
				// Close after allocating both ports so the second listener cannot reuse the first.
				if err := firstListener.Close(); err != nil {
					t.Fatalf("cannot close first listener: %v", err)
				}
			}

			if tt.cancelBeforeRun {
				cancel()
			}

			res, err := Run(ctx, cfg)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Run() error = %v, want %v", err, tt.wantErr)
			}

			if len(res) != tt.wantResults {
				t.Fatalf("got %d results, want %d", len(res), tt.wantResults)
			}

			for i, result := range res {
				if result.Port != cfg.Ports[i] {
					t.Errorf("port %d = %d, want: port = %d", i+1, res[i].Port, cfg.Ports[i])
				}

				wantErr := tt.closeFirst && i == 0
				if (res[i].Err != nil) != wantErr {
					t.Errorf("result %d error = %v, want error present: %t", i+1, res[i].Err, wantErr)
				}
			}
		})
	}
}

// TestRunCanceledAfterFirstConnection checks cancellation after a connection
// is accepted, without assuming Run has already entered its ticker wait.
func TestRunCanceledAfterFirstConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		Host:            "127.0.0.1",
		Timeout:         time.Second,
		ProbesPerSecond: 1,
	}

	listeners := make([]net.Listener, 0, 2)
	for range 2 {
		var lc net.ListenConfig
		listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("cannot create listener: %v", err)
		}
		t.Cleanup(func() {
			if err := listener.Close(); err != nil {
				t.Errorf("cannot close listener: %v", err)
			}
		})
		listeners = append(listeners, listener)
		cfg.Ports = append(cfg.Ports, listener.Addr().(*net.TCPAddr).Port)
	}

	type chanResults struct {
		res []Result
		err error
	}

	// Buffer the result so the sender can finish if a test assertion exits early.
	ch := make(chan chanResults, 1)
	go func() {
		res, err := Run(ctx, cfg)
		chanRes := chanResults{res: res, err: err}
		ch <- chanRes
	}()

	// Bound Accept independently of the scan context.
	listener := listeners[0].(*net.TCPListener)
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("cannot set listener deadline: %v", err)
	}

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("cannot accept connection: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("cannot close connection: %v", err)
	}
	cancel()

	select {
	case got := <-ch:
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("error = %v, want: error = %v", got.err, context.Canceled)
		}
		if len(got.res) != 1 {
			t.Fatalf("got %v results, want: %d results", len(got.res), 1)
		}
		if got.res[0].Port != cfg.Ports[0] {
			t.Errorf("port = %d, want: port = %d", got.res[0].Port, cfg.Ports[0])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}

}
