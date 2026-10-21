// Package runner coordinates HTTP background traffic and a scenario.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
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
// Context cancellation and deadlines are normal completion; validation and final
// output failures are returned to the caller.
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
			m.Report(ctx, out, 2*time.Second)
		}()
	}

	var res []portscan.Result
	var scanErr error

	if cfg.Scenario != nil && !cfg.DryRun {
		res, scanErr = portscan.Run(ctx, *cfg.Scenario.Portscan)
	}

	// Wait for the reporter too, so final writes never overlap its output.
	wg.Wait()

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
		succeeded := 0
		for _, r := range res {
			if r.Err == nil {
				succeeded++
			}
		}

		_, err = fmt.Fprintf(out, "portscan: attempted=%d succeeded=%d failed=%d\n", len(res), succeeded, len(res)-succeeded)
		if err != nil {
			return fmt.Errorf("write portscan summary: %w", err)
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
