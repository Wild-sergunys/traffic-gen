package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrScenarioPortInvalid identifies malformed or out-of-range port input.
	ErrScenarioPortInvalid = errors.New("invalid scenario port")
)

// parsePorts expands comma-separated ports and inclusive ranges such as "22,80-82".
// It preserves order and duplicates, accepts surrounding whitespace, and returns
// a nil result on any error so callers cannot use an incomplete list.
func parsePorts(value string) ([]int, error) {
	var res []int
	ports := strings.Split(value, ",")
	for _, p := range ports {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "-") {
			bounds := strings.Split(p, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("%w: invalid range %q", ErrScenarioPortInvalid, p)
			}

			for i, pWhitespace := range bounds {
				bounds[i] = strings.TrimSpace(pWhitespace)
			}

			start, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("%w: invalid range start %q: %w", ErrScenarioPortInvalid, bounds[0], err)
			}

			end, err := strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("%w: invalid range end %q: %w", ErrScenarioPortInvalid, bounds[1], err)
			}

			if end < start {
				return nil, fmt.Errorf("%w: range start must not exceed range end in %q", ErrScenarioPortInvalid, p)
			}

			if start < 1 || start > 65535 {
				return nil, fmt.Errorf("%w: range start %d must be between 1 and 65535", ErrScenarioPortInvalid, start)
			}

			if end < 1 || end > 65535 {
				return nil, fmt.Errorf("%w: range end %d must be between 1 and 65535", ErrScenarioPortInvalid, end)
			}

			for p := start; p <= end; p++ {
				res = append(res, p)
			}

			continue
		}

		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid port %q: %w", ErrScenarioPortInvalid, p, err)
		}

		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("%w: port %d must be between 1 and 65535", ErrScenarioPortInvalid, port)
		}

		res = append(res, port)
	}

	return res, nil
}
