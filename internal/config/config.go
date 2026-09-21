// Package config handles command-line flags and configuration validation
// for the traffic-gen tool.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"
)

const (
	minWorkers = 1
	maxWorkers = 1000
)

// Sentinel errors returned by Parse and Validate. Callers can match
// them with errors. Is to distinguish between different failures.
var (
	ErrHelpRequested  = errors.New("help requested")
	ErrTargetRequired = errors.New("target is required (use --target)")
	ErrTargetInvalid  = errors.New("target must be a valid http:// or https:// URL")
	ErrModeInvalid    = errors.New("mode must be one of: http1, http2")
	ErrRPSNegative    = errors.New("rps cannot be negative")
	ErrDurationBad    = errors.New("duration must be positive")
	ErrExtraArgs      = errors.New("unexpected extra arguments")

	ErrWorkersRange = fmt.Errorf(
		"workers must be between %d and %d", minWorkers, maxWorkers,
	)
)

// Config holds all configuration for a traffic generation run.
type Config struct {
	Target string
	Mode   string

	// RPS is the target requests per second. 0 means no throttling.
	RPS int

	Duration time.Duration
	Workers  int
	DryRun   bool
}

// Parse reads configuration from os.Args and returns a validated Config.
func Parse() (*Config, error) {
	return ParseArgs(os.Args[1:])
}

// ParseArgs reads configuration from the given arguments and returns
// a validated Config. Callers that receive ErrHelpRequested should
// print usage and exit with status 0.
func ParseArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("traffic-gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported by the caller

	var cfg Config
	fs.StringVar(&cfg.Target, "target", "", "Target URL (e.g. http://192.168.1.10)")
	fs.StringVar(&cfg.Mode, "mode", "http1", "Generation mode: http1 | http2")
	fs.IntVar(&cfg.RPS, "rps", 100, "Target requests per second (0 = unlimited)")
	fs.DurationVar(&cfg.Duration, "duration", 30*time.Second, "Duration of the run")
	fs.IntVar(&cfg.Workers, "workers", 10, "Number of concurrent workers")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Log what would be sent without sending")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ErrHelpRequested
		}
		return nil, err
	}

	if fs.NArg() > 0 {
		return nil, fmt.Errorf("%w: %v", ErrExtraArgs, fs.Args())
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks that the configuration is sensible.
func (c *Config) Validate() error {
	if c.Target == "" {
		return ErrTargetRequired
	}

	u, err := url.Parse(c.Target)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTargetInvalid, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme %q", ErrTargetInvalid, u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: missing host", ErrTargetInvalid)
	}

	switch c.Mode {
	case "http1", "http2":
		// ok
	default:
		return fmt.Errorf("%w: got %q", ErrModeInvalid, c.Mode)
	}

	if c.RPS < 0 {
		return ErrRPSNegative
	}

	if c.Duration <= 0 {
		return ErrDurationBad
	}

	if c.Workers < minWorkers || c.Workers > maxWorkers {
		return ErrWorkersRange
	}

	return nil
}

// String returns a human-readable summary of the configuration.
func (c *Config) String() string {
	rps := fmt.Sprintf("%d", c.RPS)
	if c.RPS == 0 {
		rps = "unlimited"
	}

	return fmt.Sprintf(
		"target=%s mode=%s rps=%s duration=%s workers=%d dry-run=%v",
		c.Target, c.Mode, rps, c.Duration, c.Workers, c.DryRun,
	)
}
