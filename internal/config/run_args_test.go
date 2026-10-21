package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// TestParseRunArgs checks file loading, legacy HTTP flags, and mutually exclusive input sources.
func TestParseRunArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	data := []byte("background:\n  target: http://example.com/\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	badPath := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(badPath, []byte("background: ["), 0o600); err != nil {
		t.Fatalf("cannot write invalid config: %v", err)
	}
	scanPath := filepath.Join(dir, "scan.yaml")
	scanData := []byte("scenario:\n  type: portscan\n  portscan:\n    host: 127.0.0.1\n    ports: \"80-81\"\n")
	if err := os.WriteFile(scanPath, scanData, 0o600); err != nil {
		t.Fatalf("cannot write scan config: %v", err)
	}
	missingPath := filepath.Join(dir, "missing.yaml")

	cases := []struct {
		name      string
		args      []string
		want      *RunConfig
		wantErr   bool
		wantCause error
	}{
		{
			name: "yaml_defaults",
			args: []string{"--config", path},
			want: &RunConfig{Duration: 30 * time.Second,
				Background: &Config{Target: "http://example.com/", Mode: "http1",
					RPS: 100, Duration: 30 * time.Second, Workers: 10}},
		},
		{
			name: "yaml_equals_syntax", args: []string{"--config=" + path},
			want: &RunConfig{
				Duration: 30 * time.Second,
				Background: &Config{
					Target: "http://example.com/", Mode: "http1", RPS: 100,
					Duration: 30 * time.Second, Workers: 10,
				},
			},
		},
		{
			name: "yaml_scan_only", args: []string{"--config", scanPath},
			want: &RunConfig{
				Duration: 30 * time.Second,
				Scenario: &ScenarioConfig{
					Type:     "portscan",
					Portscan: &portscan.Config{Host: "127.0.0.1", Ports: []int{80, 81}, Timeout: time.Second},
				},
			},
		},
		{
			name: "http_defaults", args: []string{"--target", "http://example.com/"},
			want: &RunConfig{
				Duration: 30 * time.Second,
				Background: &Config{
					Target: "http://example.com/", Mode: "http1", RPS: 100,
					Duration: 30 * time.Second, Workers: 10,
				},
			},
		},
		{
			name: "http_custom",
			args: []string{"--target", "https://example.com/api?device=7", "--mode", "http2",
				"--rps", "0", "--workers", "3", "--duration", "5s", "--dry-run"},
			want: &RunConfig{
				Duration: 5 * time.Second, DryRun: true,
				Background: &Config{
					Target: "https://example.com/api?device=7", Mode: "http2", RPS: 0,
					Duration: 5 * time.Second, Workers: 3, DryRun: true,
				},
			},
		},
		{name: "no_arguments", wantErr: true, wantCause: ErrTargetRequired},
		{name: "invalid_http_workers", args: []string{"--target", "http://example.com/", "--workers=0"}, wantErr: true, wantCause: ErrWorkersRange},
		{name: "help", args: []string{"--help"}, wantErr: true, wantCause: ErrHelpRequested},
		{name: "short_help", args: []string{"-h"}, wantErr: true, wantCause: ErrHelpRequested},
		{name: "help_without_opening_file", args: []string{"--config", missingPath, "--help"}, wantErr: true, wantCause: ErrHelpRequested},
		{name: "unknown_flag", args: []string{"--unknown"}, wantErr: true},
		{name: "missing_config_value", args: []string{"--config"}, wantErr: true},
		{name: "invalid_flag_value", args: []string{"--rps=many"}, wantErr: true},
		{name: "positional_argument", args: []string{"extra"}, wantErr: true, wantCause: ErrExtraArgs},
		{name: "extra_after_config", args: []string{"--config", path, "extra"}, wantErr: true, wantCause: ErrExtraArgs},
		{name: "empty_config_path", args: []string{"--config="}, wantErr: true, wantCause: ErrConfigEmptyPath},
		{name: "missing_file", args: []string{"--config", missingPath}, wantErr: true, wantCause: os.ErrNotExist},
		{name: "directory_as_file", args: []string{"--config", dir}, wantErr: true},
		{name: "invalid_yaml", args: []string{"--config", badPath}, wantErr: true, wantCause: ErrYAMLConfigBad},
		{name: "mixed_target", args: []string{"--config", path, "--target=http://example.com/"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_default_mode", args: []string{"--config", path, "--mode=http1"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_default_rate", args: []string{"--config", path, "--rps=100"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_default_workers", args: []string{"--config", path, "--workers=10"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_default_duration", args: []string{"--config", path, "--duration=30s"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_explicit_false", args: []string{"--config", path, "--dry-run=false"}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_reverse_order", args: []string{"--dry-run=false", "--config", path}, wantErr: true, wantCause: ErrConfigFlagMixed},
		{name: "mixed_before_file_access", args: []string{"--config", missingPath, "--rps=100"}, wantErr: true, wantCause: ErrConfigFlagMixed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRunArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseRunArgs() error = %v, want error present: %t", err, tc.wantErr)
			}

			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Fatalf("cause = %v, want cause = %v", err, tc.wantCause)
			}

			if tc.wantErr && got != nil {
				t.Fatalf("%+v != nil", got)
			}

			if tc.wantErr {
				return
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%+v, want: %+v", got, tc.want)
			}
		})
	}
}
