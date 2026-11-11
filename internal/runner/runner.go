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
// Terminals show a live panel; other writers receive plain-text metrics.
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

	var tasks []*httpTask
	if cfg.Background != nil {
		// Apply shared settings without changing the caller's configuration.
		back := *cfg.Background
		back.Duration, back.DryRun = cfg.Duration, cfg.DryRun
		g, err := generator.New(&back)
		if err != nil {
			return err
		}
		tasks = append(tasks, &httpTask{generator: g})
	}
	if cfg.Scenario != nil && cfg.Scenario.HTTP != nil {
		scenario := *cfg.Scenario.HTTP
		scenario.Cfg.Duration, scenario.Cfg.DryRun = cfg.Duration, cfg.DryRun
		g, err := generator.NewScenario(cfg.Scenario.Type, &scenario)
		if err != nil {
			return err
		}
		tasks = append(tasks, &httpTask{name: cfg.Scenario.Type, generator: g})
	}
	var wg, reportWG sync.WaitGroup
	var reportErr error
	start := time.Now()
	panel := newDashboard(out, cfg, tasks, start)
	if panel != nil {
		if err := panel.write(start, "RUNNING"); err != nil {
			return fmt.Errorf("report progress: %w", err)
		}
	}
	reportCtx, stopReport := context.WithCancel(ctx)
	defer stopReport()
	for _, task := range tasks {
		wg.Go(func() { task.generator.Run(ctx, &task.metrics) })
	}
	if len(tasks) > 0 || panel != nil {
		reportWG.Go(func() {
			reportErr = reportProgress(reportCtx, out, tasks, panel)
			if reportErr != nil {
				cancel()
			}
		})
	}

	var res []portscan.Result
	var scanErr error
	var scanStarted, scanFinished time.Time

	if cfg.Scenario != nil && cfg.Scenario.Type == "portscan" && !cfg.DryRun {
		scanStarted = time.Now()
		var onResult func(portscan.Result)
		if panel != nil {
			onResult = panel.scan.record
		}
		res, scanErr = portscan.RunWithProgress(ctx, *cfg.Scenario.Portscan, onResult)
		// Record completion before waiting for the independent HTTP background.
		scanFinished = time.Now()
	}

	wg.Wait()
	// Stop the reporter even when a scan-only run finishes before its deadline.
	stopReport()
	reportWG.Wait()
	// WaitGroup also synchronizes the reporter error before reading it here.
	if reportErr != nil {
		return fmt.Errorf("report progress: %w", reportErr)
	}

	if panel != nil {
		state := "DONE"
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			state = "CANCELED"
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			state = "TIME LIMIT"
		case scanErr != nil:
			state = "ERROR"
		}
		if err := panel.write(time.Now(), state); err != nil {
			return fmt.Errorf("result output error: %w", err)
		}
	}

	if cfg.DryRun {
		_, err = fmt.Fprintln(out, "dry-run: no traffic sent")
		if err != nil {
			return fmt.Errorf("result output error: %w", err)
		}
	}

	if panel == nil {
		for _, task := range tasks {
			s := task.metrics.Snapshot()
			prefix := ""
			if task.name != "" {
				prefix = task.name + " "
			}

			_, err = fmt.Fprintf(out, "%sfinal: sent=%d, errors=%d, 2xx=%v, 3xx=%v, 4xx=%v, 5xx=%v, bytes=%d, avg latency=%v, run time=%v\n",
				prefix, s.Sent, s.Errors, s.StatusClasses[0], s.StatusClasses[1], s.StatusClasses[2], s.StatusClasses[3], s.Bytes,
				s.AvgLatency.Round(time.Millisecond), time.Since(start))
			if err != nil {
				return fmt.Errorf("result output error: %w", err)
			}
		}
	}

	if cfg.Scenario != nil && cfg.Scenario.Type == "portscan" && !cfg.DryRun {
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

// httpTask owns counters for one traffic source; only the reporter writes to out.
type httpTask struct {
	name      string
	generator *generator.Generator
	metrics   metrics.Metrics
}

func reportProgress(ctx context.Context, out io.Writer, tasks []*httpTask, panel *dashboard) error {
	interval := 2 * time.Second
	if panel != nil {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if panel != nil {
				if err := panel.write(time.Now(), "RUNNING"); err != nil {
					return err
				}
				continue
			}
			for _, task := range tasks {
				prefix := ""
				if task.name != "" {
					prefix = task.name + ": "
				}
				if err := task.metrics.WriteSnapshot(out, prefix); err != nil {
					return err
				}
			}
		}
	}
}
