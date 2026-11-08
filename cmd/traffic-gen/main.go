// Package main starts background traffic and attack scenarios from CLI flags
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

// Build metadata is populated by Makefile and GoReleaser through linker flags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg, err := config.ParseRunArgs(os.Args[1:])
	if err != nil {
		switch {
		case errors.Is(err, config.ErrHelpRequested):
			config.Usage(os.Stdout)
			os.Exit(0)
		case errors.Is(err, config.ErrVersionRequested):
			if _, err := fmt.Fprintf(os.Stdout, "traffic-gen %s (commit=%s, built=%s)\n", version, commit, date); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		default:
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
