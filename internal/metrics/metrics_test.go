package metrics_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/metrics"
)

// Record holds the arguments of a single Metrics.Record call.
type Record struct {
	latency    time.Duration
	nBytes     int64
	statusCode int
	err        error
}

// brokenWriter exposes a stable cause for report failures.
type brokenWriter struct{}

// errTestFail is the sentinel error for the one_error case.
var errTestFail = errors.New("test failed")

// newMetrics returns a Metrics with every record applied in order.
func newMetrics(records []Record) *metrics.Metrics {
	m := &metrics.Metrics{}

	for _, data := range records {
		m.Record(data.latency, data.nBytes, data.statusCode, data.err)
	}

	return m
}

func (brokenWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
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
				{latency: 100 * time.Millisecond, nBytes: 300, statusCode: 200, err: nil},
				{latency: 300 * time.Millisecond, nBytes: 280, statusCode: 200, err: nil},
				{latency: 200 * time.Millisecond, nBytes: 500, statusCode: 200, err: nil}},
			want: metrics.Snapshot{
				Sent: 3, Errors: 0, Bytes: 1080, StatusClasses: [4]int64{3, 0, 0, 0}, AvgLatency: 200 * time.Millisecond,
			}},
		{
			name: "one_error",
			// The failed request runs 1s; counting it would skew the mean to
			// 466ms. AvgLatency must average only the successful requests.
			records: []Record{
				{latency: 100 * time.Millisecond, nBytes: 300, statusCode: 200, err: nil},
				{latency: 300 * time.Millisecond, nBytes: 280, statusCode: 200, err: nil},
				{latency: time.Second, nBytes: 0, statusCode: 0, err: errTestFail}},
			want: metrics.Snapshot{
				Sent: 3, Errors: 1, Bytes: 580, StatusClasses: [4]int64{2, 0, 0, 0}, AvgLatency: 200 * time.Millisecond,
			}},
		{
			name: "err_statusCode200",
			records: []Record{
				{latency: 100 * time.Millisecond, nBytes: 17, statusCode: 200, err: errTestFail}},
			want: metrics.Snapshot{
				Sent: 1, Errors: 1, Bytes: 17, StatusClasses: [4]int64{1, 0, 0, 0}, AvgLatency: 0,
			}},
		{
			name: "all_codes",
			records: []Record{
				{latency: 100 * time.Millisecond, nBytes: 10, statusCode: 200, err: nil},
				{latency: 100 * time.Millisecond, nBytes: 10, statusCode: 302, err: nil},
				{latency: 100 * time.Millisecond, nBytes: 10, statusCode: 404, err: nil},
				{latency: 100 * time.Millisecond, nBytes: 10, statusCode: 503, err: nil}},
			want: metrics.Snapshot{
				Sent: 4, Errors: 0, Bytes: 40, StatusClasses: [4]int64{1, 1, 1, 1}, AvgLatency: 100 * time.Millisecond,
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
	if err := m.Report(ctx, got, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	if got.Len() != 0 {
		t.Errorf("buffer_len = %d, want = %d", got.Len(), want)
	}
}

// TestReportTicker verifies that Report writes a snapshot on every tick.
func TestReportTicker(t *testing.T) {
	m := newMetrics([]Record{{latency: 100 * time.Millisecond, nBytes: 300, statusCode: 200, err: nil}})
	got := new(bytes.Buffer)
	want := "sent=1, errors=0, 2xx=1, 3xx=0, 4xx=0, 5xx=0, bytes=300, avg latency=100ms\n"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	if err := m.Report(ctx, got, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Contains instead of ==: the tick count depends on machine load, so
	// the number of lines in the buffer cannot be predicted.
	if !strings.Contains(got.String(), want) {
		t.Errorf("buffer = %v, want = %v", got, want)
	}
}

// TestReportBrokenWriter checks both the report classification and writer cause.
func TestReportBrokenWriter(t *testing.T) {
	m := newMetrics(nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := m.Report(ctx, brokenWriter{}, time.Millisecond)
	if !errors.Is(err, metrics.ErrWriteSnapshot) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Report() error = %v, want snapshot and writer errors", err)
	}
	if ctx.Err() != nil {
		t.Errorf("Report waited for context cancellation: %v", ctx.Err())
	}
}
