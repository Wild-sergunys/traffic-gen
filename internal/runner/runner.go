// Package runner coordinates HTTP background traffic and a scenario.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/generator"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// Run validates cfg, applies its common timeout, and runs the enabled tasks.
// HTTP background continues after the scenario finishes. All background tasks
// finish before final summaries are written to out. Dry-run sends no traffic.
// Context cancellation and deadlines are normal completion. Validation failures
// and periodic or final output failures are returned to the caller.
func Run(ctx context.Context, cfg *config.RunConfig, out io.Writer) error {
	err := cfg.Validate()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	var g *generator.Generator

	if cfg.Background != nil {
		// Apply shared settings without changing the caller's HTTP configuration.
		back := *cfg.Background
		back.Duration = cfg.Duration
		back.DryRun = cfg.DryRun
		g, err = generator.New(&back)
		if err != nil {
			return err
		}
	}

	var wg sync.WaitGroup
	var m metrics.Metrics
	var reportErr error

	start := time.Now()
	if g != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Run(ctx, &m)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			reportErr = m.Report(ctx, out, 2*time.Second)
			if reportErr != nil {
				cancel()
			}
		}()
	}

	var res []portscan.Result
	var scanErr error
	var scanStarted, scanFinished time.Time

	if cfg.Scenario != nil && !cfg.DryRun {
		scanStarted = time.Now()
		res, scanErr = portscan.Run(ctx, *cfg.Scenario.Portscan)
		// Record completion before waiting for the independent HTTP background.
		scanFinished = time.Now()
	}

	// Wait for the reporter too, so final writes never overlap its output.
	wg.Wait()
	// WaitGroup also synchronizes the reporter error before reading it here.
	if reportErr != nil {
		return fmt.Errorf("report HTTP metrics: %w", reportErr)
	}

	if cfg.DryRun {
		_, err = fmt.Fprintln(out, "dry-run: no traffic sent")
		if err != nil {
			return fmt.Errorf("result output error: %w", err)
		}
	}

	if g != nil {
		s := m.Snapshot()

		_, err = fmt.Fprintf(out, "final: sent=%d, errors=%d, 2xx=%v, 3xx=%v, 4xx=%v, 5xx=%v, bytes=%d, avg latency=%v, run time=%v\n",
			s.Sent, s.Errors, s.StatusClasses[0], s.StatusClasses[1], s.StatusClasses[2], s.StatusClasses[3], s.Bytes,
			s.AvgLatency.Round(time.Millisecond), time.Since(start))
		if err != nil {
			return fmt.Errorf("result output error: %w", err)
		}
	}

	if cfg.Scenario != nil && !cfg.DryRun {
		var connected []string
		for _, r := range res {
			if r.Err == nil {
				connected = append(connected, strconv.Itoa(r.Port))
			}
		}

		ports := "none"
		if len(connected) > 0 {
			ports = strings.Join(connected, ", ")
		}
		_, err = fmt.Fprintf(out, "portscan: connected ports: %s\n", ports)
		if err != nil {
			return fmt.Errorf("write connected ports: %w", err)
		}

		succeeded := len(connected)
		_, err = fmt.Fprintf(out, "portscan: attempted=%d succeeded=%d failed=%d\n", len(res), succeeded, len(res)-succeeded)
		if err != nil {
			return fmt.Errorf("write portscan summary: %w", err)
		}

		reason := "completed"
		switch {
		case errors.Is(scanErr, context.DeadlineExceeded):
			reason = "deadline"
		case errors.Is(scanErr, context.Canceled):
			reason = "canceled"
		case scanErr != nil:
			reason = "error"
		}
		_, err = fmt.Fprintf(out, "portscan: host=%q started=%s finished=%s reason=%s\n",
			cfg.Scenario.Portscan.Host, scanStarted.UTC().Format(time.RFC3339Nano),
			scanFinished.UTC().Format(time.RFC3339Nano), reason)
		if err != nil {
			return fmt.Errorf("write portscan timing: %w", err)
		}
	}

	// Interrupted probes remain in the summary, but cancellation is not a CLI failure.
	if scanErr != nil {
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			return nil
		}
		return fmt.Errorf("run portscan: %w", scanErr)
	}

	return nil
}
