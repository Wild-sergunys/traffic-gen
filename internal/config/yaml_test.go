package config

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// TestRunYAMLToConfig checks defaults, explicit overrides, optional blocks,
// and error propagation without involving YAML decoding or network access.
func TestRunYAMLToConfig(t *testing.T) {
	zero, workers, rate := 0, 3, 250
	mode := "http2"
	duration, timeout := 5*time.Second, 500*time.Millisecond
	zeroDuration := time.Duration(0)

	cases := []struct {
		name    string
		change  func(*runYAML, *RunConfig)
		wantErr error
	}{
		{name: "defaults"},
		{
			name: "background_only",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Scenario = nil
				want.Scenario = nil
			},
		},
		{
			name: "scenario_only",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Background = nil
				want.Background = nil
			},
		},
		{
			name: "shared_duration_and_dry_run",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Duration, raw.DryRun = &duration, true
				want.Duration, want.DryRun = 5*time.Second, true
				want.Background.Duration, want.Background.DryRun = 5*time.Second, true
			},
		},
		{
			name: "background_overrides",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Background = &backgroundYAML{
					Target: "https://example.com/api?device=7", Mode: &mode, RPS: &rate, Workers: &workers,
				}
				want.Background = &Config{RequestTimeout: 5 * time.Second,
					Target: "https://example.com/api?device=7", Mode: "http2", RPS: 250,
					Workers: 3, Duration: 30 * time.Second,
				}
			},
		},
		{
			name: "portscan_overrides",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Scenario.Portscan = &portscanYAML{
					Host: "::1", Ports: "443,80-81,80", Timeout: &timeout, ProbesPerSecond: &rate,
				}
				want.Scenario.Portscan = &portscan.Config{
					Host: "::1", Ports: []int{443, 80, 81, 80},
					Timeout: 500 * time.Millisecond, ProbesPerSecond: 250,
				}
			},
		},
		{
			name: "explicit_zero_rates",
			change: func(raw *runYAML, want *RunConfig) {
				raw.Background.RPS = &zero
				raw.Scenario.Portscan.ProbesPerSecond = &zero
				want.Background.RPS = 0
			},
		},
		{
			name: "no_blocks",
			change: func(raw *runYAML, _ *RunConfig) {
				raw.Background, raw.Scenario = nil, nil
			},
			wantErr: ErrNoWorkConfigured,
		},
		{
			name:    "explicit_zero_duration",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Duration = &zeroDuration },
			wantErr: ErrDurationBad,
		},
		{
			name:    "explicit_zero_workers",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Background.Workers = &zero },
			wantErr: ErrWorkersRange,
		},
		{
			name:    "empty_background",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Background = &backgroundYAML{} },
			wantErr: ErrTargetRequired,
		},
		{
			name: "explicit_empty_mode",
			change: func(raw *runYAML, _ *RunConfig) {
				empty := ""
				raw.Background.Mode = &empty
			},
			wantErr: ErrModeInvalid,
		},
		{
			name:    "empty_scenario",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario = &scenarioYAML{} },
			wantErr: ErrUnknownScenarioType,
		},
		{
			name:    "unknown_scenario",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario.Type = "unknown" },
			wantErr: ErrUnknownScenarioType,
		},
		{
			name:    "missing_portscan",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario.Portscan = nil },
			wantErr: ErrInvalidPortscanConfig,
		},
		{
			name:    "invalid_port_expression",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario.Portscan.Ports = "80,1.2" },
			wantErr: ErrScenarioPortInvalid,
		},
		{
			name:    "invalid_scan_host",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario.Portscan.Host = "" },
			wantErr: portscan.ErrInvalidConfig,
		},
		{
			name:    "explicit_zero_timeout",
			change:  func(raw *runYAML, _ *RunConfig) { raw.Scenario.Portscan.Timeout = &zeroDuration },
			wantErr: portscan.ErrInvalidConfig,
		},
		{
			name: "negative_scan_rate",
			change: func(raw *runYAML, _ *RunConfig) {
				negative := -1
				raw.Scenario.Portscan.ProbesPerSecond = &negative
			},
			wantErr: portscan.ErrInvalidConfig,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := runYAML{
				Background: &backgroundYAML{Target: "http://example.com/"},
				Scenario: &scenarioYAML{
					Type: "portscan", Portscan: &portscanYAML{Host: "127.0.0.1", Ports: "22,80-82"},
				},
			}
			want := &RunConfig{
				Duration: 30 * time.Second,
				Background: &Config{RequestTimeout: 5 * time.Second,
					Target: "http://example.com/", Mode: "http1", RPS: 100,
					Workers: 10, Duration: 30 * time.Second,
				},
				Scenario: &ScenarioConfig{
					Type: "portscan",
					Portscan: &portscan.Config{
						Host: "127.0.0.1", Ports: []int{22, 80, 81, 82}, Timeout: time.Second,
					},
				},
			}
			if tc.change != nil {
				tc.change(&raw, want)
			}

			got, err := raw.toConfig()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("toConfig() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Errorf("toConfig() = %+v on error, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("toConfig() = %#v, want %#v", got, want)
			}
		})
	}
}

// TestDecodeYAML checks YAML input against the validated run configuration contract.
func TestDecodeYAML(t *testing.T) {
	const backgroundInput = "background:\n  target: http://example.com/\n"
	const scanInput = "scenario:\n  type: portscan\n  portscan:\n    host: 127.0.0.1\n    ports: \"22,80-82\"\n"
	const secret = "private-test-password"

	backgroundDefaults := &RunConfig{
		Duration: 30 * time.Second,
		Background: &Config{RequestTimeout: 5 * time.Second,
			Target: "http://example.com/", Mode: "http1", RPS: 100,
			Workers: 10, Duration: 30 * time.Second,
		},
	}
	cases := []struct {
		name          string
		input         string
		want          *RunConfig
		wantErr       error
		forbiddenText string
	}{
		{
			name: "background_defaults", input: backgroundInput, want: backgroundDefaults,
		},
		{
			name:  "document_marker_and_comments",
			input: "---\n# One run\n" + backgroundInput + "...\n# End\n",
			want:  backgroundDefaults,
		},
		{
			name: "scan_defaults", input: scanInput,
			want: &RunConfig{
				Duration: 30 * time.Second,
				Scenario: &ScenarioConfig{
					Type: "portscan",
					Portscan: &portscan.Config{
						Host: "127.0.0.1", Ports: []int{22, 80, 81, 82}, Timeout: time.Second,
					},
				},
			},
		},
		{
			name: "combined_custom_settings",
			input: `duration: 5s
dry_run: true
background:
  target: https://example.com/api?device=7
  mode: http2
  rps: 0
  workers: 3
scenario:
  type: portscan
  portscan:
    host: "::1"
    ports: "443,80-81,80"
    timeout: 500ms
    probes_per_second: 20
`,
			want: &RunConfig{
				Duration: 5 * time.Second, DryRun: true,
				Background: &Config{RequestTimeout: 5 * time.Second,
					Target: "https://example.com/api?device=7", Mode: "http2", RPS: 0,
					Workers: 3, Duration: 5 * time.Second, DryRun: true,
				},
				Scenario: &ScenarioConfig{
					Type: "portscan",
					Portscan: &portscan.Config{
						Host: "::1", Ports: []int{443, 80, 81, 80},
						Timeout: 500 * time.Millisecond, ProbesPerSecond: 20,
					},
				},
			},
		},
		{name: "unused_anchor", input: strings.Replace(backgroundInput, "background:", "background: &web", 1), want: backgroundDefaults},
		{name: "scalar_alias", input: "duration: &limit 30s\n" + scanInput + "    timeout: *limit\n", wantErr: ErrYAMLConfigBad},
		{name: "mapping_alias", input: "background: &web {target: http://example.com/}\nscenario: *web\n", wantErr: ErrYAMLConfigBad},
		{name: "recursive_alias", input: "background: &web {target: http://example.com/, nested: *web}\n", wantErr: ErrYAMLConfigBad},
		{name: "inline_merge", input: "background: {<<: {target: http://example.com/}}\n", wantErr: ErrYAMLConfigBad},
		{name: "aliased_merge", input: "background: &web {target: http://example.com/}\nscenario: {<<: *web}\n", wantErr: ErrYAMLConfigBad},
		{name: "fractional_rate", input: backgroundInput + "  rps: 1.2\n", wantErr: ErrYAMLConfigBad},
		{name: "fractional_scan_rate", input: scanInput + "    probes_per_second: 1.2\n", wantErr: ErrYAMLConfigBad},
		{name: "quoted_integer", input: backgroundInput + "  workers: \"10\"\n", wantErr: ErrYAMLConfigBad},
		{name: "numeric_target", input: "background: {target: 123}\n", wantErr: ErrYAMLConfigBad},
		{name: "numeric_mode", input: backgroundInput + "  mode: 1\n", wantErr: ErrYAMLConfigBad},
		{name: "numeric_host", input: "scenario: {type: portscan, portscan: {host: 123, ports: \"80\"}}\n", wantErr: ErrYAMLConfigBad},
		{name: "numeric_scenario_type", input: "scenario: {type: 123}\n", wantErr: ErrYAMLConfigBad},
		{name: "numeric_scan_timeout", input: scanInput + "    timeout: 1\n", wantErr: ErrYAMLConfigBad},
		{name: "legacy_boolean", input: backgroundInput + "dry_run: yes\n", wantErr: ErrYAMLConfigBad},
		{name: "quoted_boolean", input: backgroundInput + "dry_run: \"true\"\n", wantErr: ErrYAMLConfigBad},
		{name: "scalar_root", input: "hello\n", wantErr: ErrYAMLConfigBad},
		{name: "sequence_background", input: "background: []\n", wantErr: ErrYAMLConfigBad},
		{name: "sequence_portscan", input: "scenario: {type: portscan, portscan: []}\n", wantErr: ErrYAMLConfigBad},
		{name: "empty", input: "", wantErr: ErrYAMLConfigBad},
		{name: "comments_only", input: "# No settings\n", wantErr: ErrYAMLConfigBad},
		{name: "empty_mapping", input: "{}", wantErr: ErrNoWorkConfigured},
		{name: "root_null", input: "null", wantErr: ErrYAMLConfigBad},
		{name: "null_background", input: scanInput + "background: null\n", wantErr: ErrYAMLConfigBad},
		{name: "null_scenario", input: backgroundInput + "scenario: null\n", wantErr: ErrYAMLConfigBad},
		{name: "null_rate", input: backgroundInput + "  rps: null\n", wantErr: ErrYAMLConfigBad},
		{name: "implicit_null", input: backgroundInput + "  rps:\n", wantErr: ErrYAMLConfigBad},
		{name: "tilde_null", input: backgroundInput + "  rps: ~\n", wantErr: ErrYAMLConfigBad},
		{name: "null_scan_timeout", input: scanInput + "    timeout: null\n", wantErr: ErrYAMLConfigBad},
		{name: "null_dry_run", input: backgroundInput + "dry_run: null\n", wantErr: ErrYAMLConfigBad},
		{name: "unknown_root_field", input: backgroundInput + "unexpected: true\n", wantErr: ErrYAMLConfigBad},
		{name: "unknown_background_field", input: backgroundInput + "  wrokers: 10\n", wantErr: ErrYAMLConfigBad},
		{name: "unknown_scan_field", input: scanInput + "    unexpected: 1\n", wantErr: ErrYAMLConfigBad},
		{name: "duplicate_root_field", input: backgroundInput + "duration: 1s\nduration: 2s\n", wantErr: ErrYAMLConfigBad},
		{name: "duplicate_nested_field", input: backgroundInput + "  rps: 10\n  rps: 20\n", wantErr: ErrYAMLConfigBad},
		{name: "second_document", input: backgroundInput + "---\n" + scanInput, wantErr: ErrYAMLConfigBad},
		{name: "empty_second_document", input: backgroundInput + "---\n", wantErr: ErrYAMLConfigBad},
		{name: "malformed_trailing_document", input: backgroundInput + "---\n[", wantErr: ErrYAMLConfigBad},
		{name: "malformed_first_document", input: "background: [", wantErr: ErrYAMLConfigBad},
		{name: "root_sequence", input: "- background\n", wantErr: ErrYAMLConfigBad},
		{name: "wrong_block_type", input: "background: text\n", wantErr: ErrYAMLConfigBad},
		{name: "wrong_workers_type", input: backgroundInput + "  workers: many\n", wantErr: ErrYAMLConfigBad},
		{name: "fractional_workers", input: backgroundInput + "  workers: 1.2\n", wantErr: ErrYAMLConfigBad},
		{name: "wrong_ports_type", input: "scenario: {type: portscan, portscan: {host: localhost, ports: 80}}", wantErr: ErrYAMLConfigBad},
		{name: "wrong_duration_type", input: backgroundInput + "duration: 5\n", wantErr: ErrYAMLConfigBad},
		{name: "invalid_duration", input: backgroundInput + "duration: tomorrow\n", wantErr: ErrYAMLConfigBad},
		{name: "zero_duration", input: backgroundInput + "duration: 0s\n", wantErr: ErrDurationBad},
		{name: "zero_workers", input: backgroundInput + "  workers: 0\n", wantErr: ErrWorkersRange},
		{name: "invalid_ports", input: strings.Replace(scanInput, "22,80-82", "80,1.2", 1), wantErr: ErrScenarioPortInvalid},
		{name: "unknown_scenario", input: "scenario: {type: unknown}", wantErr: ErrUnknownScenarioType},
		{name: "missing_scan_settings", input: "scenario: {type: portscan}", wantErr: ErrInvalidPortscanConfig},
		{
			name:    "decode_error_hides_secret",
			input:   backgroundInput + "duration: " + secret + "\n",
			wantErr: ErrYAMLConfigBad, forbiddenText: secret,
		},
		{
			name:    "parse_error_hides_secret",
			input:   "background: {target: \"http://user:" + secret + "@example.com/\n",
			wantErr: ErrYAMLConfigBad, forbiddenText: secret,
		},
		{
			name:    "validation_error_hides_target_password",
			input:   "background:\n  target: http://user:" + secret + "@example.com/\n  mode: http2\n",
			wantErr: ErrInvalidTargetHTTP2, forbiddenText: secret,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeYAML(strings.NewReader(tc.input))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DecodeYAML() error = %v, want %v", err, tc.wantErr)
			}
			if tc.forbiddenText != "" && err != nil && strings.Contains(err.Error(), tc.forbiddenText) {
				t.Error("DecodeYAML() error contains a secret from the input")
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Errorf("DecodeYAML() = %+v on error, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DecodeYAML() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestDecodeYAMLReadError preserves reader failures without returning partial settings.
func TestDecodeYAMLReadError(t *testing.T) {
	got, err := DecodeYAML(iotest.ErrReader(io.ErrUnexpectedEOF))
	if !errors.Is(err, ErrYAMLConfigBad) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeYAML() error = %v, want config and reader errors", err)
	}
	if got != nil {
		t.Errorf("DecodeYAML() = %+v on read error, want nil", got)
	}
}

// TestDecodeYAMLRequestTimeout checks overrides, disabling, and strict input types.
func TestDecodeYAMLRequestTimeout(t *testing.T) {
	tests := []struct {
		name, value string
		want        time.Duration
		wantErr     bool
	}{
		{name: "custom", value: "2s", want: 2 * time.Second},
		{name: "disabled", value: "0s"},
		{name: "negative", value: "-1s", wantErr: true},
		{name: "numeric", value: "2", wantErr: true},
		{name: "null", value: "null", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := DecodeYAML(strings.NewReader("background:\n  target: http://example.com/\n  request_timeout: " + tt.value + "\n"))
			if (err != nil) != tt.wantErr {
				t.Fatalf("DecodeYAML() error = %v, want error %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cfg.Background.RequestTimeout != tt.want {
				t.Errorf("request timeout = %v, want %v", cfg.Background.RequestTimeout, tt.want)
			}
		})
	}
}
