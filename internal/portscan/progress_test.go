package portscan

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// Progress is delivered before the next probe; the observer can stop the scan.
func TestRunWithProgress(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var observed []Result
	results, err := RunWithProgress(ctx, Config{Host: "127.0.0.1", Ports: []int{port, port}, Timeout: time.Second}, func(r Result) {
		observed = append(observed, r)
		cancel()
	})
	if !errors.Is(err, context.Canceled) || len(results) != 1 || len(observed) != 1 {
		t.Fatalf("results=%v observed=%v err=%v", results, observed, err)
	}
	if observed[0].Port != port || observed[0].Err != nil || results[0] != observed[0] {
		t.Fatalf("unexpected progress result: %+v", observed[0])
	}
}
