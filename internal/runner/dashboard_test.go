package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// Rates use completed attempts since the previous update, not the configured RPS.
func TestDashboardRates(t *testing.T) {
	start := time.Unix(0, 0)
	var out bytes.Buffer
	task := &httpTask{}
	d := &dashboard{out: &out, tasks: []*httpTask{task}, start: start, previous: start,
		duration: 10 * time.Second, width: 80, fd: -1, sent: make([]int64, 1)}
	for range 4 {
		task.metrics.Record(10*time.Millisecond, 100, 200, nil)
	}
	if err := d.write(start.Add(2*time.Second), "RUNNING"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"20%", "2.0", "Background", "10ms"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
	out.Reset()
	task.metrics.Record(time.Millisecond, 0, 0, context.Canceled)
	if err := d.write(start.Add(4*time.Second), "RUNNING"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0.5") || !strings.Contains(out.String(), "\x1b[") {
		t.Errorf("want interval rate and in-place redraw, got %q", out.String())
	}
	out.Reset()
	if err := d.write(start.Add(5*time.Second), "CANCELED"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CANCELED", "50%", "1.0", "2xx: 4", "bytes: 400"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("final output missing %q: %q", want, out.String())
		}
	}
}

func TestDashboardNarrowScan(t *testing.T) {
	start := time.Unix(0, 0)
	var out bytes.Buffer
	progress := &scanProgress{total: 4}
	progress.record(portscan.Result{Port: 8080})
	progress.record(portscan.Result{Port: 8081, Err: io.EOF})
	d := &dashboard{out: &out, start: start, previous: start, duration: time.Minute,
		width: 40, fd: -1, scan: progress}
	if err := d.write(start.Add(time.Second), "RUNNING"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2/4", "50%", "connected: 1", "8080"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if utf8.RuneCountInString(line) >= 40 {
			t.Errorf("line would wrap: %q", line)
		}
	}
}

func TestDashboardOutputError(t *testing.T) {
	d := &dashboard{out: brokenDashboardWriter{}, start: time.Now(), duration: time.Second, width: 80, fd: -1}
	if err := d.write(time.Now(), "DONE"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v, want %v", err, io.ErrClosedPipe)
	}
}

type brokenDashboardWriter struct{}

func (brokenDashboardWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// Buffers and files keep the existing plain-text reporting contract.
func TestDashboardPlainOutput(t *testing.T) {
	var out bytes.Buffer
	if got := newDashboard(&out, &config.RunConfig{Duration: time.Second}, nil, time.Now()); got != nil {
		t.Fatal("non-terminal output enabled dashboard")
	}
}

// Narrow layouts retain complete counters instead of cutting off their digits.
func TestDashboardNarrowHTTP(t *testing.T) {
	start := time.Unix(0, 0)
	var out bytes.Buffer
	task := &httpTask{name: "sqli"}
	task.metrics.Record(time.Second, 9223372036854775807, 500, nil)
	d := &dashboard{out: &out, tasks: []*httpTask{task}, start: start, previous: start,
		duration: time.Second, width: 24, fd: -1, sent: make([]int64, 1)}
	if err := d.write(start.Add(time.Second), "TIME LIMIT"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SQLi", "9223372036854775807", "5xx: 1", "errors: 0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if utf8.RuneCountInString(line) >= d.width {
			t.Errorf("line would wrap: %q", line)
		}
	}
}

// Rendering can read scan progress while the scanner delivers completed probes.
func TestScanProgressConcurrent(t *testing.T) {
	progress := &scanProgress{total: 1000}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			progress.record(portscan.Result{Port: 8080})
		}
	}()
	for range 100 {
		_ = progress.lines()
	}
	<-done
	output := strings.Join(progress.lines(), "\n")
	if !strings.Contains(output, "1000/1000") || !strings.Contains(output, "connected: 1000") || len(progress.ports) != 8 {
		t.Fatalf("unexpected final progress: %q; preview size = %d", output, len(progress.ports))
	}
}
