package generator

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// This verifies transport fixtures, not detector accuracy or exploit success.
func TestControlCorpus(t *testing.T) {
	for _, kind := range []string{"sqli", "xss"} {
		t.Run(kind, func(t *testing.T) {
			data, err := os.ReadFile("../../examples/control-" + kind + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.DecodeYAML(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Scenario.Type != kind {
				t.Fatal("wrong scenario type")
			}
			c := cfg.Scenario.HTTP
			builtins, err := scenarioPayloads(kind, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Payloads) < 4 {
				t.Fatal("incomplete control corpus")
			}
			seen := make(map[string]bool)
			for _, p := range c.Payloads {
				if seen[p] || slices.Contains(builtins, p) {
					t.Fatalf("duplicate or built-in control input: %q", p)
				}
				seen[p] = true
			}
			g, err := NewScenario(kind, c)
			if err != nil {
				t.Fatal(err)
			}
			for _, payload := range c.Payloads {
				for _, method := range c.Methods {
					req, err := g.newRequest(t.Context(), "corpus-test")
					if err != nil {
						t.Fatal(err)
					}
					if err := req.ParseForm(); err != nil {
						t.Fatal(err)
					}
					req.Body.Close()
					if req.Method != method || req.Form.Get(c.Parameter) != payload || req.URL.Query().Get("device") != "7" {
						t.Fatalf("corpus input changed: %s %v", req.Method, req.Form)
					}
				}
			}
		})
	}
}
