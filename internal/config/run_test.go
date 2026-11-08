package config

import (
	"errors"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// TestRunConfigValidate checks block combinations and preserves nested error identities.
func TestRunConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*RunConfig)
		wantErr error
	}{
		{name: "background_and_scenario"},
		{
			name:   "background_only",
			change: func(c *RunConfig) { c.Scenario = nil },
		},
		{
			name:   "scenario_only",
			change: func(c *RunConfig) { c.Background = nil },
		},
		{
			name:    "zero_duration",
			change:  func(c *RunConfig) { c.Duration = 0 },
			wantErr: ErrDurationBad,
		},
		{
			name:    "negative_duration",
			change:  func(c *RunConfig) { c.Duration = -time.Second },
			wantErr: ErrDurationBad,
		},
		{
			name: "no_work",
			change: func(c *RunConfig) {
				c.Background = nil
				c.Scenario = nil
			},
			wantErr: ErrNoWorkConfigured,
		},
		{
			name:    "invalid_background",
			change:  func(c *RunConfig) { c.Background.Target = "" },
			wantErr: ErrTargetRequired,
		},
		{
			name:    "unknown_scenario_type",
			change:  func(c *RunConfig) { c.Scenario.Type = "unknown" },
			wantErr: ErrUnknownScenarioType,
		},
		{
			name:    "empty_scenario_type",
			change:  func(c *RunConfig) { c.Scenario.Type = "" },
			wantErr: ErrUnknownScenarioType,
		},
		{
			name:    "missing_portscan_config",
			change:  func(c *RunConfig) { c.Scenario.Portscan = nil },
			wantErr: ErrInvalidPortscanConfig,
		},
		{
			name:    "invalid_portscan_config",
			change:  func(c *RunConfig) { c.Scenario.Portscan.Ports = nil },
			wantErr: portscan.ErrInvalidConfig,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RunConfig{
				Duration: time.Second,
				// The run duration supplies the HTTP duration during validation.
				Background: &Config{
					Target: "http://example.com/", Mode: "http1", Workers: 1,
				},
				Scenario: &ScenarioConfig{
					Type: "portscan",
					Portscan: &portscan.Config{
						Host: "127.0.0.1", Ports: []int{80}, Timeout: time.Second,
					},
				},
			}
			if tc.change != nil {
				tc.change(&cfg)
			}

			if err := cfg.Validate(); !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestRunConfigValidatePreservesBackground checks that applying the common
// duration during validation leaves every field of the original HTTP config intact.
func TestRunConfigValidatePreservesBackground(t *testing.T) {
	background := Config{
		Target:   "http://127.0.0.1",
		Mode:     "http1",
		RPS:      0,
		Duration: 0,
		Workers:  10,
		DryRun:   false,
	}
	before := background

	runCfg := RunConfig{
		Duration:   time.Second,
		DryRun:     false,
		Background: &background,
	}

	err := runCfg.Validate()
	if err != nil {
		t.Fatalf("invalid RunConfig: error = %v, want: error = %v", err, nil)
	}

	if background != before {
		t.Fatalf("baground changed: %+v, want: %+v", background, before)
	}
}

// HTTP scenario validation rejects conflicting blocks and keeps caller settings intact.
func TestRunHTTPScenarioValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ScenarioConfig)
		want   error
	}{
		{name: "valid"},
		{"missing_http", func(c *ScenarioConfig) { c.HTTP = nil }, ErrInvalidHTTPScenarioConfig},
		{"conflict_http", func(c *ScenarioConfig) { c.Portscan = &portscan.Config{} }, ErrConflictingScenarioConfig},
		{"conflict_portscan", func(c *ScenarioConfig) { c.Type = "portscan"; c.Portscan = &portscan.Config{} }, ErrConflictingScenarioConfig},
		{"invalid_method", func(c *ScenarioConfig) { c.HTTP.Methods = []string{"DELETE"} }, ErrInvalidMethod},
		{"blank_parameter", func(c *ScenarioConfig) { c.HTTP.Parameter = " \t" }, ErrParameterRequired},
		{"empty_payload", func(c *ScenarioConfig) { c.HTTP.Payloads = []string{""} }, ErrInvalidPayloads},
		{"whitespace_payload", func(c *ScenarioConfig) { c.HTTP.Payloads = []string{" "} }, nil},
	}
	for _, kind := range []string{"sqli", "xss"} {
		for _, tt := range tests {
			t.Run(kind+"/"+tt.name, func(t *testing.T) {
				c := &ScenarioConfig{Type: kind, HTTP: &HTTPScenarioConfig{Cfg: Config{Target: "http://example.com/", Mode: "http1", Workers: 1}, Methods: []string{"GET"}, Parameter: "q"}}
				if tt.change != nil {
					tt.change(c)
				}
				cfg := RunConfig{Duration: time.Second, Scenario: c}
				if err := cfg.Validate(); !errors.Is(err, tt.want) {
					t.Errorf("error = %v, want %v", err, tt.want)
				}
				if c.HTTP != nil && c.HTTP.Cfg.Duration != 0 {
					t.Error("validation changed caller duration")
				}
			})
		}
	}
}
