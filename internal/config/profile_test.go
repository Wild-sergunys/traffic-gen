package config

import (
	"errors"
	"strings"
	"testing"
)

func TestBackgroundProfile(t *testing.T) {
	for _, profile := range []string{"", "simple", "web", "unknown"} {
		t.Run(profile, func(t *testing.T) {
			cfg, err := ParseRunArgs([]string{"--target", "http://example.com/", "--profile", profile})
			if profile == "unknown" {
				if !errors.Is(err, ErrProfileInvalid) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Background.Profile != profile {
				t.Errorf("profile = %q", cfg.Background.Profile)
			}
			input := "background:\n  target: http://example.com/\n  profile: \"" + profile + "\"\n"
			got, err := DecodeYAML(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if got.Background.Profile != profile {
				t.Errorf("YAML profile = %q", got.Background.Profile)
			}
		})
	}
	for _, profile := range []string{"web", "simple"} {
		input := "scenario:\n  type: sqli\n  http:\n    target: http://example.com/\n    profile: " + profile + "\n"
		if _, err := DecodeYAML(strings.NewReader(input)); err == nil {
			t.Error("scenario accepted a background profile")
		}
	}
	if _, err := ParseRunArgs([]string{"--target", "http://example.com/?bad=%ZZ", "--profile", "web"}); !errors.Is(err, ErrTargetInvalid) {
		t.Errorf("malformed web query error = %v", err)
	}
}
