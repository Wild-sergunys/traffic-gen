package config_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// TestParseArgsDefaults verifies that every optional flag falls back to
// its documented default value when only --target is provided.
func TestParseArgsDefaults(t *testing.T) {
	cfg, err := config.ParseArgs([]string{"--target", "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Target != "http://example.com" {
		t.Errorf("target = %q, want %q", cfg.Target, "http://example.com")
	}
	if cfg.Mode != "http1" {
		t.Errorf("mode = %q, want %q", cfg.Mode, "http1")
	}
	if cfg.RPS != 100 {
		t.Errorf("rps = %d, want %d", cfg.RPS, 100)
	}
	if cfg.Duration != 30*time.Second {
		t.Errorf("duration = %v, want %v", cfg.Duration, 30*time.Second)
	}
	if cfg.Workers != 10 {
		t.Errorf("workers = %d, want %d", cfg.Workers, 10)
	}
	if cfg.DryRun {
		t.Errorf("dry-run = %v, want false", cfg.DryRun)
	}
}

// TestParseArgsAllCustomFlags verifies that every flag is parsed and
// stored in the corresponding Config field when all are provided.
func TestParseArgsAllCustomFlags(t *testing.T) {
	args := []string{
		"--target", "https://api.example.com",
		"--mode", "http2",
		"--rps", "500",
		"--duration", "5m",
		"--workers", "50",
		"--dry-run",
	}

	cfg, err := config.ParseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Target != "https://api.example.com" {
		t.Errorf("target = %q, want %q", cfg.Target, "https://api.example.com")
	}
	if cfg.Mode != "http2" {
		t.Errorf("mode = %q, want %q", cfg.Mode, "http2")
	}
	if cfg.RPS != 500 {
		t.Errorf("rps = %d, want %d", cfg.RPS, 500)
	}
	if cfg.Duration != 5*time.Minute {
		t.Errorf("duration = %v, want %v", cfg.Duration, 5*time.Minute)
	}
	if cfg.Workers != 50 {
		t.Errorf("workers = %d, want %d", cfg.Workers, 50)
	}
	if !cfg.DryRun {
		t.Errorf("dry-run = %v, want true", cfg.DryRun)
	}
}

// TestParseArgsValidationErrors verifies that every invalid input is
// rejected with the expected sentinel error.
func TestParseArgsValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
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

			_, err := config.ParseArgs(tt.args)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got err = %v, want = %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseArgsBoundaryValues verifies that values on the edge of the
// accepted range pass validation without errors.
func TestParseArgsBoundaryValues(t *testing.T) {
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

			cfg, err := config.ParseArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Workers != tt.wantWorkers {
				t.Errorf("workers = %d, want %d", cfg.Workers, tt.wantWorkers)
			}
			if cfg.RPS != tt.wantRPS {
				t.Errorf("rps = %d, want %d", cfg.RPS, tt.wantRPS)
			}
		})
	}
}

// TestParseArgsExtraArgs verifies that positional arguments after the
// flags are rejected instead of silently ignored.
func TestParseArgsExtraArgs(t *testing.T) {
	_, err := config.ParseArgs([]string{"--target", "http://x", "foo", "bar"})
	if !errors.Is(err, config.ErrExtraArgs) {
		t.Errorf("got err = %v, want = %v", err, config.ErrExtraArgs)
	}
}

// TestParseArgsHelp verifies that both short and long help flags are
// translated into ErrHelpRequested so the caller can exit with status 0.
func TestParseArgsHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			_, err := config.ParseArgs([]string{arg})
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
// manually constructed Config, without going through ParseArgs.
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

	for _, flag := range []string{"target", "mode", "rps", "duration", "workers", "dry-run"} {
		if !strings.Contains(out, "-"+flag) {
			t.Errorf("usage output does not mention -%s", flag)
		}
	}
}
