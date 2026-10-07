package metrics_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/metrics"
)

// Record holds the arguments of a single Metrics.Record call.
type Record struct {
	latency time.Duration
	nBytes  int64
	err     error
}

// brokenWriter fails every Write to exercise Report stderr fallback.
type brokenWriter struct{}

// errTestFail is the sentinel error for the one_error case.
var errTestFail = errors.New("test failed")

// newMetrics returns a Metrics with every record applied in order.
func newMetrics(records []Record) *metrics.Metrics {
	m := &metrics.Metrics{}

	for _, data := range records {
		m.Record(data.latency, data.nBytes, data.err)
	}

	return m
}

func (brokenWriter) Write(p []byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// TestMetrics verifies Record accumulation and Snapshot aggregation.
func TestMetrics(t *testing.T) {
	tests := []struct {
		name    string
		records []Record
		want    metrics.Snapshot
	}{
		{
			name: "all_ok",
			records: []Record{
				{latency: 100 * time.Millisecond, nBytes: 300, err: nil},
				{latency: 300 * time.Millisecond, nBytes: 280, err: nil},
				{latency: 200 * time.Millisecond, nBytes: 500, err: nil}},
			want: metrics.Snapshot{
				Sent: 3, Errors: 0, Bytes: 1080, AvgLatency: 200 * time.Millisecond,
			}},
		{
			name: "one_error",
			records: []Record{
				{latency: 100 * time.Millisecond, nBytes: 300, err: nil},
				{latency: 300 * time.Millisecond, nBytes: 280, err: nil},
				{latency: 200 * time.Millisecond, nBytes: 0, err: errTestFail}},
			want: metrics.Snapshot{
				Sent: 3, Errors: 1, Bytes: 580, AvgLatency: 200 * time.Millisecond,
			}},
		{
			name:    "empty",
			records: []Record{},
			want:    metrics.Snapshot{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMetrics(tt.records)
			s := m.Snapshot()

			if s != tt.want {
				t.Errorf("got snapshot = %+v, want snapshot = %+v", s, tt.want)
			}
		})
	}
}

// TestReportCancel verifies that Report returns immediately on an already
// cancelled context and writes nothing.
func TestReportCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := newMetrics(nil)
	got := new(bytes.Buffer)
	want := 0

	// A short interval would let the ticker fire. select picks at random
	// when both cases are ready, so the test would fail intermittently.
	m.Report(ctx, got, 2*time.Second)

	if got.Len() != 0 {
		t.Errorf("buffer_len = %d, want = %d", got.Len(), want)
	}
}

// TestReportTicker verifies that Report writes a snapshot on every tick.
func TestReportTicker(t *testing.T) {
	m := newMetrics([]Record{{latency: 100 * time.Millisecond, nBytes: 300, err: nil}})
	got := new(bytes.Buffer)
	want := "sent=1, errors=0, bytes=300, avg latency=100ms\n"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	m.Report(ctx, got, 20*time.Millisecond)

	// Contains instead of ==: the tick count depends on machine load, so
	// the number of lines in the buffer cannot be predicted.
	if !strings.Contains(got.String(), want) {
		t.Errorf("buffer = %v, want = %v", got, want)
	}
}

// TestReportBrokenWriter verifies that a failed write falls back to
// os.Stderr with ErrWriteSnapshot.
func TestReportBrokenWriter(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stderr

	// Report writes snapshots to its writer argument, but write failures to
	// os.Stderr. Redirecting stderr to the pipe makes that fallback visible.
	os.Stderr = w

	m := newMetrics([]Record{{latency: 100 * time.Millisecond, nBytes: 300, err: nil}})
	want := metrics.ErrWriteSnapshot
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	m.Report(ctx, brokenWriter{}, 20*time.Millisecond)

	// Restore stderr for the remaining tests, then close the write end:
	// io.ReadAll returns only on EOF, and EOF requires Close - without it
	// the read blocks forever.
	os.Stderr = old
	w.Close()
	got, _ := io.ReadAll(r)
	r.Close()

	if !strings.Contains(string(got), want.Error()) {
		t.Errorf("stderr = %s, want = %v", got, want)
	}
}
