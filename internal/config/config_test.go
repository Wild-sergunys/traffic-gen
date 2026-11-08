package config_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// TestParseHTTPFlagsDefaults verifies that every optional flag falls back to
// its documented default value when only --target is provided.
func TestParseHTTPFlagsDefaults(t *testing.T) {
	cfg, err := config.ParseRunArgs([]string{"--target", "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Background.Target != "http://example.com" {
		t.Errorf("target = %q, want %q", cfg.Background.Target, "http://example.com")
	}
	if cfg.Background.Mode != "http1" {
		t.Errorf("mode = %q, want %q", cfg.Background.Mode, "http1")
	}
	if cfg.Background.RPS != 100 {
		t.Errorf("rps = %d, want %d", cfg.Background.RPS, 100)
	}
	if cfg.Background.RequestTimeout != 5*time.Second {
		t.Errorf("request timeout = %v, want 5s", cfg.Background.RequestTimeout)
	}
	if cfg.Background.Duration != 30*time.Second {
		t.Errorf("duration = %v, want %v", cfg.Background.Duration, 30*time.Second)
	}
	if cfg.Background.Workers != 10 {
		t.Errorf("workers = %d, want %d", cfg.Background.Workers, 10)
	}
	if cfg.Background.DryRun {
		t.Errorf("dry-run = %v, want false", cfg.Background.DryRun)
	}
}

// TestParseHTTPFlagsAllCustomFlags verifies that every flag is parsed and
// stored in the corresponding Config field when all are provided.
func TestParseHTTPFlagsAllCustomFlags(t *testing.T) {
	args := []string{
		"--target", "https://api.example.com",
		"--mode", "http2",
		"--rps", "500",
		"--duration", "5m",
		"--request-timeout", "2s",
		"--workers", "50",
		"--dry-run",
	}

	cfg, err := config.ParseRunArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Background.Target != "https://api.example.com" {
		t.Errorf("target = %q, want %q", cfg.Background.Target, "https://api.example.com")
	}
	if cfg.Background.Mode != "http2" {
		t.Errorf("mode = %q, want %q", cfg.Background.Mode, "http2")
	}
	if cfg.Background.RPS != 500 {
		t.Errorf("rps = %d, want %d", cfg.Background.RPS, 500)
	}
	if cfg.Background.RequestTimeout != 2*time.Second {
		t.Errorf("request timeout = %v, want 2s", cfg.Background.RequestTimeout)
	}
	if cfg.Background.Duration != 5*time.Minute {
		t.Errorf("duration = %v, want %v", cfg.Background.Duration, 5*time.Minute)
	}
	if cfg.Background.Workers != 50 {
		t.Errorf("workers = %d, want %d", cfg.Background.Workers, 50)
	}
	if !cfg.Background.DryRun {
		t.Errorf("dry-run = %v, want true", cfg.Background.DryRun)
	}
}

// TestParseHTTPFlagsValidationErrors verifies that every invalid input is
// rejected with the expected sentinel error.
func TestParseHTTPFlagsValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{"negative request timeout", []string{"--target", "http://x", "--request-timeout=-1s"}, config.ErrRequestTimeoutBad},
		// nil args means "no command-line arguments were provided".
		// flag.FlagSet.Parse(nil) does NOT fall back to os.Args.
		{"missing target", nil, config.ErrTargetRequired},
		{"empty target", []string{"--target", ""}, config.ErrTargetRequired},
		{"unsupported scheme", []string{"--target", "ftp://x"}, config.ErrTargetInvalid},
		{"missing host", []string{"--target", "http://"}, config.ErrTargetInvalid},
		{"unparsable target", []string{"--target", "http://[invalid"}, config.ErrTargetInvalid},
		{"unknown mode", []string{"--target", "http://x", "--mode", "http3"}, config.ErrModeInvalid},
		{"empty mode", []string{"--target", "http://x", "--mode", ""}, config.ErrModeInvalid},
		{"negative rps", []string{"--target", "http://x", "--rps", "-1"}, config.ErrRPSNegative},
		{"rps too large", []string{"--target", "http://x", "--rps", "20000000"}, config.ErrRPSRange},
		{"zero duration", []string{"--target", "http://x", "--duration", "0s"}, config.ErrDurationBad},
		{"negative duration", []string{"--target", "http://x", "--duration", "-1s"}, config.ErrDurationBad},
		{"zero workers", []string{"--target", "http://x", "--workers", "0"}, config.ErrWorkersRange},
		{"too many workers", []string{"--target", "http://x", "--workers", "1001"}, config.ErrWorkersRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.ParseRunArgs(tt.args)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got err = %v, want = %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseHTTPFlagsBoundaryValues verifies that values on the edge of the
// accepted range pass validation without errors.
func TestParseHTTPFlagsBoundaryValues(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantWorkers int
		wantRPS     int
	}{
		{
			"minimum workers",
			[]string{"--target", "http://x", "--workers", "1"},
			1, 100,
		},
		{
			"maximum workers",
			[]string{"--target", "http://x", "--workers", "1000"},
			1000, 100,
		},
		{
			"unlimited rps",
			[]string{"--target", "http://x", "--rps", "0"},
			10, 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.ParseRunArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Background.Workers != tt.wantWorkers {
				t.Errorf("workers = %d, want %d", cfg.Background.Workers, tt.wantWorkers)
			}
			if cfg.Background.RPS != tt.wantRPS {
				t.Errorf("rps = %d, want %d", cfg.Background.RPS, tt.wantRPS)
			}
		})
	}
}

// TestParseHTTPFlagsExtraArgs verifies that positional arguments after the
// flags are rejected instead of silently ignored.
func TestParseHTTPFlagsExtraArgs(t *testing.T) {
	_, err := config.ParseRunArgs([]string{"--target", "http://x", "foo", "bar"})
	if !errors.Is(err, config.ErrExtraArgs) {
		t.Errorf("got err = %v, want = %v", err, config.ErrExtraArgs)
	}
}

// TestParseHTTPFlagsHelp verifies that both short and long help flags are
// translated into ErrHelpRequested so the caller can exit with status 0.
func TestParseHTTPFlagsHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			_, err := config.ParseRunArgs([]string{arg})
			if !errors.Is(err, config.ErrHelpRequested) {
				t.Errorf("got err = %v, want = %v", err, config.ErrHelpRequested)
			}
		})
	}
}

// TestConfigString verifies the human-readable representation of Config.
func TestConfigString(t *testing.T) {
	t.Run("normal values", func(t *testing.T) {
		cfg := &config.Config{
			Target:   "http://x",
			Mode:     "http1",
			RPS:      100,
			Duration: 30 * time.Second,
			Workers:  10,
			DryRun:   false,
		}
		s := cfg.String()

		checks := map[string]string{
			"target":   "target=http://x",
			"mode":     "mode=http1",
			"rps":      "rps=100",
			"duration": "duration=30s",
			"workers":  "workers=10",
			"dry-run":  "dry-run=false",
		}

		for name, expected := range checks {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				if !strings.Contains(s, expected) {
					t.Errorf("string %q does not contain %q", s, expected)
				}
			})
		}
	})

	t.Run("unlimited rps", func(t *testing.T) {
		cfg := &config.Config{
			Target:   "http://x",
			Mode:     "http1",
			RPS:      0,
			Duration: 30 * time.Second,
			Workers:  10,
			DryRun:   false,
		}
		s := cfg.String()

		const unlimitedRPS = "rps=unlimited"
		const forbiddenRPS = "rps=0"

		if !strings.Contains(s, unlimitedRPS) {
			t.Errorf("string %q does not contain %q", s, unlimitedRPS)
		}
		if strings.Contains(s, forbiddenRPS) {
			t.Errorf("string %q should not contain %q", s, forbiddenRPS)
		}
	})
}

// TestConfigValidateDirect verifies that Validate can be called on a
// manually constructed Config, without going through command-line parsing.
func TestConfigValidateDirect(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.Config{
			Target:   "http://x",
			Mode:     "http1",
			RPS:      100,
			Duration: 30 * time.Second,
			Workers:  10,
		}

		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		cfg := &config.Config{
			Target:   "ftp://x",
			Mode:     "http1",
			RPS:      100,
			Duration: 30 * time.Second,
			Workers:  10,
		}

		err := cfg.Validate()
		if !errors.Is(err, config.ErrTargetInvalid) {
			t.Errorf("got err = %v, want = %v", err, config.ErrTargetInvalid)
		}
	})
}

// TestUsage verifies that the exported Usage helper writes a
// description for every supported flag.
func TestUsage(t *testing.T) {
	var buf bytes.Buffer
	config.Usage(&buf)

	out := buf.String()
	if out == "" {
		t.Fatal("usage output is empty")
	}

	for _, flag := range []string{"target", "mode", "rps", "duration", "workers", "dry-run", "request-timeout", "config", "version", "profile"} {
		if !strings.Contains(out, "-"+flag) {
			t.Errorf("usage output does not mention -%s", flag)
		}
	}
}

// TestConfigTargetRedaction verifies that summaries and validation errors hide
// passwords without changing the target used for requests.
func TestConfigTargetRedaction(t *testing.T) {
	const password = "test-password"

	tests := []struct {
		name       string
		target     string
		mode       string
		wantTarget string
		wantErr    error
	}{
		{
			name:       "valid target",
			target:     "http://alice:test-password@example.com/api?device=7",
			mode:       "http1",
			wantTarget: "http://alice:xxxxx@example.com/api?device=7",
		},
		{
			name:       "http2 requires https",
			target:     "http://alice:test-password@example.com/api?device=7",
			mode:       "http2",
			wantTarget: "http://alice:xxxxx@example.com/api?device=7",
			wantErr:    config.ErrInvalidTargetHTTP2,
		},
		{
			name:       "h2c requires http",
			target:     "https://alice:test-password@example.com/api?device=7",
			mode:       "h2c",
			wantTarget: "https://alice:xxxxx@example.com/api?device=7",
			wantErr:    config.ErrInvalidTargetH2C,
		},
		{
			name:       "unparsable target",
			target:     "http://alice:test-password@[invalid",
			mode:       "http1",
			wantTarget: "<invalid url>",
			wantErr:    config.ErrTargetInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Target:   tt.target,
				Mode:     tt.mode,
				RPS:      100,
				Duration: time.Second,
				Workers:  1,
			}

			summary := cfg.String()
			if !strings.Contains(summary, "target="+tt.wantTarget+" ") {
				t.Errorf("summary = %q, want target %q", summary, tt.wantTarget)
			}
			if strings.Contains(summary, password) {
				t.Errorf("summary exposes password: %q", summary)
			}
			if cfg.Target != tt.target {
				t.Errorf("target after String = %q, want %q", cfg.Target, tt.target)
			}

			err := cfg.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate error = %v, want %v", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), password) {
				t.Errorf("Validate error exposes password: %q", err.Error())
			}
			if cfg.Target != tt.target {
				t.Errorf("target after Validate = %q, want %q", cfg.Target, tt.target)
			}
		})
	}
}

// Invalid explicit ports are configuration errors, even in dry-run mode.
func TestHTTPPortValidation(t *testing.T) {
	for _, tt := range []struct {
		target string
		valid  bool
	}{
		{"http://example.com/", true},
		{"http://example.com:1/", true},
		{"https://example.com:65535/", true},
		{"http://[::1]:8080/", true},
		{"http://[::1]/", true},
		{"http://example.com:0/", false},
		{"http://example.com:65536/", false},
		{"http://example.com:999999999999999999999/", false},
		{"http://example.com:/", false},
		{"http://[::1]:65536/", false},
		{"http://alice:test-password@example.com:99999/", false},
	} {
		t.Run(tt.target, func(t *testing.T) {
			_, err := config.ParseRunArgs([]string{"--target", tt.target, "--dry-run"})
			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, config.ErrTargetInvalid) {
				t.Errorf("error = %v, want invalid target", err)
			}
			if err != nil && strings.Contains(err.Error(), "test-password") {
				t.Error("error exposes password")
			}
			input := "background:\n  target: " + tt.target + "\n"
			if _, err := config.DecodeYAML(strings.NewReader(input)); !errors.Is(err, config.ErrTargetInvalid) {
				t.Errorf("YAML error = %v, want invalid target", err)
			}
		})
	}
}
