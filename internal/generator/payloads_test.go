package generator

import (
	"errors"
	"slices"
	"testing"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

func TestScenarioPayloads(t *testing.T) {
	for _, kind := range []string{"sqli", "xss"} {
		t.Run(kind, func(t *testing.T) {
			got, err := scenarioPayloads(kind, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 || slices.Contains(got, "") {
				t.Fatalf("built-in payloads = %q, want nonempty strings", got)
			}
			custom := []string{"a&b + %"}
			got, err = scenarioPayloads(kind, custom)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, custom) {
				t.Errorf("payloads = %q, want %q", got, custom)
			}
		})
	}
	if _, err := scenarioPayloads("unknown", nil); !errors.Is(err, config.ErrUnknownScenarioType) {
		t.Errorf("error = %v, want %v", err, config.ErrUnknownScenarioType)
	}
}
