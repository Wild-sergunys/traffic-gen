package metrics

import (
	"sync/atomic"
	"time"
)

// Metrics collects atomic counters for a traffic generation run.
// Workers only call Record; a reporter goroutine reads snapshots.
type Metrics struct {
	sent       atomic.Int64 // attempts, including failed ones
	errors     atomic.Int64 // failed attempts
	bytes      atomic.Int64 // response body bytes received
	latencySum atomic.Int64 // sum of latencies, ns
	latencyCnt atomic.Int64 // latency samples
}

// Snapshot holds a consistent read of the counters at the moment of the call.
// AvgLatency is the arithmetic mean over all recorded requests and is zero
// when no request has been recorded yet.
type Snapshot struct {
	Sent       int64         // attempts, including failed ones
	Errors     int64         // failed attempts
	Bytes      int64         // response body bytes received
	AvgLatency time.Duration // mean request latency over all recorded requests
}

// Record accounts for one completed request. It is safe for concurrent use.
func (m *Metrics) Record(latency time.Duration, nBytes int64, err error) {
	// sent, latencyCnt, and latencySum are always incremented, as they
	// do not depend on whether the request is successful or not.
	m.sent.Add(1)
	m.latencyCnt.Add(1)
	m.latencySum.Add(int64(latency))

	// No response body arrived, so there are no bytes to count.
	if err != nil {
		m.errors.Add(1)
	} else {
		m.bytes.Add(nBytes)
	}
}

// Snapshot captures the current counter values. It is safe for concurrent use.
func (m *Metrics) Snapshot() Snapshot {
	s := Snapshot{
		Sent:   m.sent.Load(),
		Errors: m.errors.Load(),
		Bytes:  m.bytes.Load(),
	}

	if cnt := m.latencyCnt.Load(); cnt > 0 {
		s.AvgLatency = time.Duration(m.latencySum.Load() / cnt)
	}

	return s
}
