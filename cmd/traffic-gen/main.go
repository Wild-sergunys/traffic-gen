// Package main starts HTTP traffic generation and port scanning from CLI flags
// or a YAML configuration file, with shutdown on interrupt or SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/runner"
)

func main() {
	cfg, err := config.ParseRunArgs(os.Args[1:])
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err = runner.Run(ctx, cfg, os.Stdout)
	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
