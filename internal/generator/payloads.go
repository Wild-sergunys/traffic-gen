package generator

import (
	"fmt"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// scenarioPayloads selects a small built-in profile or a validated custom list.
// These inputs represent attempts; they do not establish exploit success.
func scenarioPayloads(kind string, custom []string) ([]string, error) {
	if kind != "sqli" && kind != "xss" {
		return nil, fmt.Errorf("%w: %q", config.ErrUnknownScenarioType, kind)
	}
	if custom != nil {
		return custom, nil
	}

	if kind == "sqli" {
		return []string{
			"' OR '1'='1' -- ",
			"1 OR 1=1 -- ",
			"' UNION SELECT NULL,NULL -- ",
		}, nil
	}
	return []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"<svg onload=alert(1)>",
	}, nil
}
