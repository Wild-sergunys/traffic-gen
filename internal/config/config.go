// Package config loads CLI and YAML settings and validates HTTP traffic
// generation and scenario configurations.
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
	maxRPS     = 10_000_000
)

// Sentinel errors identify command-line parsing and HTTP validation failures.
// Callers can match them with errors.Is.
var (
	ErrHelpRequested      = errors.New("help requested")
	ErrTargetRequired     = errors.New("target is required (use --target)")
	ErrTargetInvalid      = errors.New("target must be a valid http:// or https:// URL")
	ErrModeInvalid        = errors.New("mode must be one of: http1, http2, h2c")
	ErrInvalidTargetHTTP2 = errors.New("mode http2 requires an https:// target")
	ErrInvalidTargetH2C   = errors.New("mode h2c requires an http:// target")
	ErrRPSNegative        = errors.New("rps cannot be negative")
	ErrDurationBad        = errors.New("duration must be positive")
	ErrExtraArgs          = errors.New("unexpected extra arguments")

	// ErrRPSRange is built with fmt.Errorf because the message embeds
	// the maxRPS constant.
	ErrRPSRange = fmt.Errorf("rps must not exceed %d", maxRPS)

	// ErrWorkersRange is built with fmt.Errorf because the message
	// embeds the minWorkers and maxWorkers constants.
	ErrWorkersRange = fmt.Errorf(
		"workers must be between %d and %d", minWorkers, maxWorkers,
	)
)

// Config holds HTTP traffic generation settings. RunConfig adds shared
// settings and an optional scenario.
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

// registerFlags declares legacy HTTP flags, shared by ParseArgs and
// registerRunFlags so their defaults stay consistent.
func registerFlags(fs *flag.FlagSet, cfg *Config) {
	fs.StringVar(&cfg.Target, "target", "", "Target URL (e.g. http://192.168.1.10)")
	fs.StringVar(&cfg.Mode, "mode", "http1", "Generation mode: http1 | http2 | h2c")
	fs.IntVar(&cfg.RPS, "rps", 100, "Target requests per second (0 = unlimited)")
	fs.DurationVar(&cfg.Duration, "duration", 30*time.Second, "Duration of the run")
	fs.IntVar(&cfg.Workers, "workers", 10, "Number of concurrent workers")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Run the pipeline without sending any request")
}

// ParseArgs reads configuration from the given arguments and returns
// a validated Config. Callers that receive ErrHelpRequested should call
// Usage to print help text, then exit with status 0.
func ParseArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("traffic-gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported by the caller

	var cfg Config
	registerFlags(fs, &cfg)

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

// Usage writes the flag help text to w. Call this when ParseRunArgs
// returns ErrHelpRequested.
func Usage(w io.Writer) {
	fs := flag.NewFlagSet("traffic-gen", flag.ContinueOnError)
	fs.SetOutput(w)
	registerRunFlags(fs, &Config{})
	fs.PrintDefaults()
}

// Validate checks that the configuration is sensible.
func (c *Config) Validate() error {
	if c.Target == "" {
		return ErrTargetRequired
	}

	u, err := url.Parse(c.Target)
	if err != nil {
		return ErrTargetInvalid
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme", ErrTargetInvalid)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: missing host", ErrTargetInvalid)
	}

	switch c.Mode {
	case "http1":
		// ok
	case "http2":
		if u.Scheme != "https" {
			return fmt.Errorf("%w: got %q", ErrInvalidTargetHTTP2, u.Redacted())
		}
	case "h2c":
		if u.Scheme != "http" {
			return fmt.Errorf("%w: got %q", ErrInvalidTargetH2C, u.Redacted())
		}
	default:
		return fmt.Errorf("%w: got %q", ErrModeInvalid, c.Mode)
	}

	if c.RPS < 0 {
		return ErrRPSNegative
	}
	if c.RPS > maxRPS {
		return ErrRPSRange
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

	u, err := url.Parse(c.Target)
	urlHidden := "<invalid url>"
	if err == nil {
		urlHidden = u.Redacted()
	}

	return fmt.Sprintf(
		"target=%s mode=%s rps=%s duration=%s workers=%d dry-run=%v",
		urlHidden, c.Mode, rps, c.Duration, c.Workers, c.DryRun,
	)
}
