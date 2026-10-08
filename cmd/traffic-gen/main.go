// Package main is the entry point of the traffic-gen tool, an HTTP/1.1
// load generator that drives traffic against a single target.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/generator"
	"github.com/wild-sergunys/traffic-gen/internal/metrics"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		if errors.Is(err, config.ErrHelpRequested) {
			config.Usage(os.Stdout)
			os.Exit(0)
		} else {
			fmt.Fprintln(os.Stderr, err)
			config.Usage(os.Stderr)
			os.Exit(1)
		}
	}

	fmt.Printf("config: %s\n", cfg.String())
	m := &metrics.Metrics{}
	g, err := generator.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	start := time.Now()
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Report(ctx, os.Stdout, 2*time.Second)
	}()

	g.Run(ctx, m)
	wg.Wait()

	s := m.Snapshot()
	fmt.Printf("final: sent=%d, errors=%d, bytes=%d, avg latency=%v, run time=%v\n", s.Sent, s.Errors, s.Bytes, s.AvgLatency.Round(time.Millisecond), time.Since(start))

}
