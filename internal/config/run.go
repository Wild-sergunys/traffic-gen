package config

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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
// Type "portscan" requires Portscan; "sqli" and "xss" require HTTP.
type ScenarioConfig struct {
	Type     string
	HTTP     *HTTPScenarioConfig
	Portscan *portscan.Config
}

// HTTPScenarioConfig combines HTTP settings with methods and payload input
// for a SQLi or XSS scenario.
type HTTPScenarioConfig struct {
	Cfg     Config
	Methods []string
	// Payloads is nil for a built-in profile; explicit lists must be nonempty.
	Payloads []string
	// Parameter names the query or form field that receives each payload.
	Parameter string
}

var (
	// ErrNoWorkConfigured means neither background traffic nor a scenario was provided.
	ErrNoWorkConfigured = errors.New("background or scenario is required")
	// ErrInvalidPortscanConfig means the selected portscan block is missing.
	ErrInvalidPortscanConfig = errors.New("invalid portscan config")
	// ErrUnknownScenarioType identifies an unsupported or empty scenario type.
	ErrUnknownScenarioType = errors.New("unknown scenario type")
	// ErrMethodsRequired identifies an empty request method list.
	ErrMethodsRequired = errors.New("at least one method is required")
	// ErrInvalidMethod identifies a request method other than GET or POST.
	ErrInvalidMethod = errors.New("method must be GET or POST")
	// ErrParameterRequired identifies a missing or whitespace-only parameter name.
	ErrParameterRequired = errors.New("parameter cannot be empty or whitespace")
	// ErrInvalidPayloads identifies an empty explicit list or an empty payload string.
	ErrInvalidPayloads = errors.New("payloads must contain at least one nonempty string")
	// ErrInvalidHTTPScenarioConfig identifies a missing HTTP settings block.
	ErrInvalidHTTPScenarioConfig = errors.New("invalid HTTP scenario config")
	// ErrConflictingScenarioConfig identifies simultaneous HTTP and portscan settings.
	ErrConflictingScenarioConfig = errors.New("HTTP and portscan settings cannot be combined")
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

			if c.Scenario.HTTP != nil {
				return fmt.Errorf("scenario.portscan: %w", ErrConflictingScenarioConfig)
			}

			err := c.Scenario.Portscan.Validate()
			if err != nil {
				return fmt.Errorf("scenario.portscan: %w", err)
			}
		case "sqli", "xss":
			if c.Scenario.HTTP == nil {
				return fmt.Errorf("%w: SQLi/XSS config cannot be nil", ErrInvalidHTTPScenarioConfig)
			}

			if c.Scenario.Portscan != nil {
				return fmt.Errorf("scenario.http: %w", ErrConflictingScenarioConfig)
			}

			httpCfg := *c.Scenario.HTTP
			httpCfg.Cfg.Duration = c.Duration

			err := httpCfg.Validate()
			if err != nil {
				return fmt.Errorf("scenario.http: %w", err)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnknownScenarioType, c.Scenario.Type)
		}
	}

	return nil
}

// Validate checks HTTP settings and payload inputs without applying defaults
// or modifying the configuration. A nil payload list selects a built-in profile.
func (c *HTTPScenarioConfig) Validate() error {
	if c.Cfg.Profile != "" {
		return fmt.Errorf("%w: profile is only supported for background traffic", ErrInvalidHTTPScenarioConfig)
	}

	err := c.Cfg.Validate()
	if err != nil {
		return fmt.Errorf("HTTP scenario config: %w", err)
	}

	if len(c.Methods) == 0 {
		return fmt.Errorf("HTTP scenario config: %w", ErrMethodsRequired)
	}

	for _, method := range c.Methods {
		if method != http.MethodGet && method != http.MethodPost {
			return fmt.Errorf("HTTP scenario config: %w", ErrInvalidMethod)
		}
	}

	if strings.TrimSpace(c.Parameter) == "" {
		return fmt.Errorf("HTTP scenario config: %w", ErrParameterRequired)
	}

	if c.Payloads != nil && len(c.Payloads) == 0 {
		return fmt.Errorf("HTTP scenario config: %w", ErrInvalidPayloads)
	}

	for _, payload := range c.Payloads {
		if payload == "" {
			return fmt.Errorf("HTTP scenario config: %w", ErrInvalidPayloads)
		}
	}

	return nil
}
