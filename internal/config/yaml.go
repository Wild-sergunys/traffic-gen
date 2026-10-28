package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/portscan"
	"go.yaml.in/yaml/v3"
)

// backgroundYAML uses pointers where omission must differ from an explicit zero value.
type backgroundYAML struct {
	RequestTimeout *time.Duration `yaml:"request_timeout"`
	Target         string         `yaml:"target"`
	Mode           *string        `yaml:"mode"`
	RPS            *int           `yaml:"rps"`
	Workers        *int           `yaml:"workers"`
}

// portscanYAML keeps the port expression intact until conversion expands its ranges.
type portscanYAML struct {
	Host            string         `yaml:"host"`
	Ports           string         `yaml:"ports"`
	Timeout         *time.Duration `yaml:"timeout"`
	ProbesPerSecond *int           `yaml:"probes_per_second"`
}

// scenarioYAML keeps a missing settings block visible to scenario validation.
type scenarioYAML struct {
	Type     string        `yaml:"type"`
	Portscan *portscanYAML `yaml:"portscan"`
}

// runYAML represents input fields before defaults and semantic validation.
// Nil optional blocks remain disabled; rejecting explicit YAML null is the decoder's responsibility.
type runYAML struct {
	Duration   *time.Duration  `yaml:"duration"`
	DryRun     bool            `yaml:"dry_run"`
	Background *backgroundYAML `yaml:"background"`
	Scenario   *scenarioYAML   `yaml:"scenario"`
}

var (
	ErrYAMLConfigBad = errors.New("YAML config bad")
)

// toConfig applies defaults only to omitted fields and returns a validated run.
// Conversion or validation failure returns no partial configuration.
func (raw runYAML) toConfig() (*RunConfig, error) {
	runCfg := RunConfig{
		Duration: 30 * time.Second,
		DryRun:   raw.DryRun,
	}

	if raw.Duration != nil {
		runCfg.Duration = *raw.Duration
	}

	if raw.Background != nil {
		cfg := Config{
			RequestTimeout: 5 * time.Second,
			Target:         raw.Background.Target,
			Mode:           "http1",
			RPS:            100,
			Workers:        10,
			Duration:       runCfg.Duration,
			DryRun:         runCfg.DryRun,
		}

		if raw.Background.RequestTimeout != nil {
			cfg.RequestTimeout = *raw.Background.RequestTimeout
		}

		if raw.Background.Mode != nil {
			cfg.Mode = *raw.Background.Mode
		}

		// An explicit zero disables pacing instead of selecting the default rate.
		if raw.Background.RPS != nil {
			cfg.RPS = *raw.Background.RPS
		}

		if raw.Background.Workers != nil {
			cfg.Workers = *raw.Background.Workers
		}

		runCfg.Background = &cfg
	}

	if raw.Scenario != nil {
		scenarioCfg := ScenarioConfig{
			Type: raw.Scenario.Type,
		}

		if raw.Scenario.Portscan != nil {
			ports, err := parsePorts(raw.Scenario.Portscan.Ports)

			if err != nil {
				return nil, fmt.Errorf("scenario.portscan.ports: %w", err)
			}

			portscanCfg := portscan.Config{
				Host:            raw.Scenario.Portscan.Host,
				Timeout:         time.Second,
				Ports:           ports,
				ProbesPerSecond: 0,
			}

			if raw.Scenario.Portscan.Timeout != nil {
				portscanCfg.Timeout = *raw.Scenario.Portscan.Timeout
			}

			if raw.Scenario.Portscan.ProbesPerSecond != nil {
				portscanCfg.ProbesPerSecond = *raw.Scenario.Portscan.ProbesPerSecond
			}

			scenarioCfg.Portscan = &portscanCfg
		}
		runCfg.Scenario = &scenarioCfg
	}

	err := runCfg.Validate()

	if err != nil {
		return nil, err
	}

	return &runCfg, nil
}

// yamlFieldTags defines input types before the decoder can coerce scalar values.
// Keep these paths in sync with the YAML input structs.
var yamlFieldTags = map[string]string{
	"duration":                            "!!str",
	"dry_run":                             "!!bool",
	"background":                          "!!map",
	"background.request_timeout":          "!!str",
	"background.target":                   "!!str",
	"background.mode":                     "!!str",
	"background.rps":                      "!!int",
	"background.workers":                  "!!int",
	"scenario":                            "!!map",
	"scenario.type":                       "!!str",
	"scenario.portscan":                   "!!map",
	"scenario.portscan.host":              "!!str",
	"scenario.portscan.ports":             "!!str",
	"scenario.portscan.timeout":           "!!str",
	"scenario.portscan.probes_per_second": "!!int",
}

// DecodeYAML reads exactly one document, applies defaults, and validates the run.
// Null values, aliases, merge keys, unknown fields, and mismatched tags are rejected.
func DecodeYAML(r io.Reader) (*RunConfig, error) {
	var raw runYAML
	// Keep the bytes for separate tree validation and strict struct decoding.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrYAMLConfigBad, err)
	}

	treeDecoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	err = treeDecoder.Decode(&document)
	if err != nil {
		// Parser diagnostics may include secrets from the input.
		return nil, fmt.Errorf(
			"%w: cannot parse YAML document",
			ErrYAMLConfigBad,
		)
	}

	// Reject unsupported constructs before decoding can hide their original form.
	err = rejectUnsupportedYAML(&document)
	if err != nil {
		return nil, err
	}

	// The document node wraps one root value; validate that value as a mapping.
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, ErrYAMLConfigBad
	}
	err = validateYAMLTypes(document.Content[0], "")
	if err != nil {
		return nil, err
	}

	// Restart from the beginning to decode against the known configuration fields.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	err = decoder.Decode(&raw)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: cannot decode configuration fields",
			ErrYAMLConfigBad,
		)
	}

	// A successful second decode would silently leave another configuration unused.
	var node yaml.Node
	err = decoder.Decode(&node)
	if err == nil {
		return nil, fmt.Errorf("%w: only one YAML document is allowed", ErrYAMLConfigBad)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(
			"%w: cannot parse trailing YAML content",
			ErrYAMLConfigBad,
		)
	}

	runCfg, err := raw.toConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrYAMLConfigBad, err)
	}

	return runCfg, nil
}

// rejectUnsupportedYAML rejects null, aliases, and merge keys without following links.
func rejectUnsupportedYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("%w: YAML aliases are not supported at line %d, column %d",
			ErrYAMLConfigBad, node.Line, node.Column)
	}

	if node.Tag == "!!merge" {
		return fmt.Errorf("%w: YAML merge keys are not supported at line %d, column %d",
			ErrYAMLConfigBad, node.Line, node.Column)
	}

	if node.Tag == "!!null" {
		return fmt.Errorf(
			"%w: null is not allowed at line %d, column %d",
			ErrYAMLConfigBad, node.Line, node.Column,
		)
	}

	for _, n := range node.Content {
		err := rejectUnsupportedYAML(n)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateYAMLTypes checks known fields using paths relative to the root mapping.
// An empty path denotes the root; nested calls extend it with the field name.
func validateYAMLTypes(node *yaml.Node, path string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: expected a YAML mapping at line %d, column %d",
			ErrYAMLConfigBad, node.Line, node.Column)
	}

	// Mapping content alternates between keys and their values.
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		fieldPath := key.Value
		if path != "" {
			fieldPath = path + "." + key.Value
		}

		expectedTag, known := yamlFieldTags[fieldPath]
		if !known {
			// Strict struct decoding reports unknown fields after this pass.
			continue
		}

		if value.Tag != expectedTag {
			return fmt.Errorf("%w: field %q must have type %s",
				ErrYAMLConfigBad, fieldPath, expectedTag)
		}

		if value.Kind == yaml.MappingNode {
			err := validateYAMLTypes(value, fieldPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
