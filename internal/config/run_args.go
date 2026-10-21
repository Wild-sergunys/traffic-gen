package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

var (
	// ErrConfigFlagMixed rejects YAML combined with explicitly supplied HTTP flags.
	ErrConfigFlagMixed = errors.New("--config cannot be combined with HTTP flags")
	// ErrConfigEmptyPath identifies an explicitly supplied empty config path.
	ErrConfigEmptyPath = errors.New("config file path cannot be empty")
)

// ParseRunArgs loads and validates either a YAML file or legacy HTTP flags.
// YAML cannot be combined with HTTP flags, even when their values match defaults.
// Help returns ErrHelpRequested without opening the configuration file.
func ParseRunArgs(args []string) (*RunConfig, error) {
	fs := flag.NewFlagSet("traffic-gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var cfg Config
	configPath := registerRunFlags(fs, &cfg)

	err := fs.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ErrHelpRequested
		}
		return nil, err
	}

	if fs.NArg() > 0 {
		return nil, ErrExtraArgs
	}

	// Visit distinguishes omitted flags from explicit default or empty values.
	var configSet, httpFlagsSet bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configSet = true
		} else {
			httpFlagsSet = true
		}
	})

	if configSet && httpFlagsSet {
		return nil, ErrConfigFlagMixed
	}

	if !configSet {
		err := cfg.Validate()
		if err != nil {
			return nil, err
		}
		cfgRun := RunConfig{
			Duration:   cfg.Duration,
			DryRun:     cfg.DryRun,
			Background: &cfg,
		}

		return &cfgRun, nil
	}

	if *configPath == "" {
		return nil, ErrConfigEmptyPath
	}

	f, err := os.Open(*configPath)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}

	// Always close the file; a decoding failure takes precedence over a close error.
	runCfg, err := DecodeYAML(f)
	closeErr := f.Close()
	if err != nil {
		return nil, fmt.Errorf("decode config file: %w", err)
	}

	if closeErr != nil {
		return nil, fmt.Errorf("close config file: %w", closeErr)
	}

	return runCfg, nil
}

// registerRunFlags shares the complete flag definitions with Usage.
func registerRunFlags(fs *flag.FlagSet, cfg *Config) *string {
	registerFlags(fs, cfg)
	return fs.String("config", "", "Path to YAML configuration file")
}
