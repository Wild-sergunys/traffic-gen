package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// RunConfig describes a run with optional HTTP background traffic and one scenario.
// A nil block is disabled; at least one block must be present.
type RunConfig struct {
	// Duration is the common time limit, including for HTTP background validation.
	Duration   time.Duration
	DryRun     bool
	Background *Config
	Scenario   *ScenarioConfig
}

// ScenarioConfig selects a scenario and its type-specific settings.
// Type "portscan" requires a non-nil Portscan configuration.
type ScenarioConfig struct {
	Type     string
	Portscan *portscan.Config
}

var (
	// ErrNoWorkConfigured means neither background traffic nor a scenario was provided.
	ErrNoWorkConfigured = errors.New("background or scenario is required")
	// ErrInvalidPortscanConfig means the selected portscan block is missing.
	ErrInvalidPortscanConfig = errors.New("invalid portscan config")
	// ErrUnknownScenarioType identifies an unsupported or empty scenario type.
	ErrUnknownScenarioType = errors.New("unknown scenario type")
)

// Validate checks enabled blocks without modifying them or performing network I/O.
// Nested validation errors remain available through errors.Is and errors.As.
func (c *RunConfig) Validate() error {
	if c.Duration <= 0 {
		return ErrDurationBad
	}

	if c.Background == nil && c.Scenario == nil {
		return fmt.Errorf("%w: background and scenario cannot be nil at the same time", ErrNoWorkConfigured)
	}

	if c.Background != nil {
		// Validate against the common duration without changing the caller's HTTP config.
		background := *c.Background
		background.Duration = c.Duration
		err := background.Validate()
		if err != nil {
			return fmt.Errorf("background: %w", err)
		}
	}

	if c.Scenario != nil {
		switch c.Scenario.Type {
		case "portscan":
			if c.Scenario.Portscan == nil {
				return fmt.Errorf("%w: portscan config cannot be nil", ErrInvalidPortscanConfig)
			}
			err := c.Scenario.Portscan.Validate()
			if err != nil {
				return fmt.Errorf("scenario.portscan: %w", err)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnknownScenarioType, c.Scenario.Type)
		}
	}

	return nil
}
