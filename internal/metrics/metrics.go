package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// ErrWriteSnapshot is written to stderr when a snapshot write fails.
var ErrWriteSnapshot = errors.New("snapshot cannot be written")

// Metrics collects atomic counters for a traffic generation run.
// Workers only call Record; a reporter goroutine reads snapshots.
type Metrics struct {
	sent          atomic.Int64    // completed attempts, including failed ones
	errors        atomic.Int64    // failed attempts
	bytes         atomic.Int64    // response body bytes received
	latencySum    atomic.Int64    // sum of latencies for attempts without errors, ns
	latencyCnt    atomic.Int64    // latency samples for attempts without errors
	statusClasses [4]atomic.Int64 // status code counter, order: 2xx, 3xx, 4xx, 5xx
}

// Snapshot holds independently read counters. During concurrent recording,
// fields may reflect different instants; after all Record calls finish, they
// are consistent. AvgLatency includes only attempts recorded without errors,
// including HTTP 4xx/5xx responses, and is zero when there are no such attempts.
type Snapshot struct {
	Sent          int64         // completed attempts, including failed ones
	Errors        int64         // failed attempts
	StatusClasses [4]int64      // order: 2xx, 3xx, 4xx, 5xx
	Bytes         int64         // response body bytes received
	AvgLatency    time.Duration // mean latency over attempts without errors
}

// Record accounts for one completed request. It is safe for concurrent use.
func (m *Metrics) Record(latency time.Duration, nBytes int64, statusCode int, err error) {
	m.sent.Add(1)

	// Count the response status even if reading its body failed.
	if statusCode >= 200 && statusCode <= 599 {
		m.statusClasses[statusCode/100-2].Add(1)
	}

	// Bytes are also counted in the event of an error.
	m.bytes.Add(nBytes)

	// Failed attempts count as errors and are excluded from AvgLatency.
	if err != nil {
		m.errors.Add(1)
	} else {
		m.latencyCnt.Add(1)
		m.latencySum.Add(int64(latency))
	}
}

// Snapshot reads counters independently. It is safe for concurrent use,
// but the result is not an atomic snapshot of all counters.
func (m *Metrics) Snapshot() Snapshot {
	s := Snapshot{
		Sent:   m.sent.Load(),
		Errors: m.errors.Load(),
		Bytes:  m.bytes.Load(),
	}

	for i := range m.statusClasses {
		s.StatusClasses[i] = m.statusClasses[i].Load()
	}

	if cnt := m.latencyCnt.Load(); cnt > 0 {
		s.AvgLatency = time.Duration(m.latencySum.Load() / cnt)
	}

	return s
}

// Report writes a snapshot to w every interval until ctx is cancelled.
// It blocks until ctx is done; the caller prints the final summary after it returns.
func (m *Metrics) Report(ctx context.Context, w io.Writer, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s := m.Snapshot()
			_, err := fmt.Fprintf(
				w, "sent=%v, errors=%v, 2xx=%v, 3xx=%v, 4xx=%v, 5xx=%v, bytes=%v, avg latency=%v\n",
				s.Sent, s.Errors, s.StatusClasses[0], s.StatusClasses[1], s.StatusClasses[2], s.StatusClasses[3],
				s.Bytes, s.AvgLatency.Round(time.Millisecond),
			)
			// Error written in stderr, because w is broken.
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v: %v\n", ErrWriteSnapshot, err)
			}
		}
	}
}
