package config

import (
	"errors"
	"slices"
	"testing"
)

// TestParsePorts checks inclusive ranges, input validation, and rejection of partial results.
func TestParsePorts(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    []int
		wantErr error
	}{
		{name: "single_port", input: "80", want: []int{80}},
		{name: "port_list", input: "443,22,80", want: []int{443, 22, 80}},
		{name: "inclusive_range", input: "80-82", want: []int{80, 81, 82}},
		{name: "mixed_list", input: "22,80-82,443", want: []int{22, 80, 81, 82, 443}},
		{name: "spaces", input: " 22 , 80 - 82 , 443 ", want: []int{22, 80, 81, 82, 443}},
		{name: "whitespace", input: "\t22,\n80\t", want: []int{22, 80}},
		{name: "duplicates", input: "80,80-81,80", want: []int{80, 80, 81, 80}},
		{name: "port_boundaries", input: "1,65535", want: []int{1, 65535}},
		{name: "lower_range_boundary", input: "1-2", want: []int{1, 2}},
		{name: "upper_range_boundary", input: "65534-65535", want: []int{65534, 65535}},
		{name: "equal_bounds", input: "80-80", want: []int{80}},
		{name: "equal_lower_bounds", input: "1-1", want: []int{1}},
		{name: "equal_upper_bounds", input: "65535-65535", want: []int{65535}},
		{name: "empty", input: "", wantErr: ErrScenarioPortInvalid},
		{name: "blank", input: " \t\n ", wantErr: ErrScenarioPortInvalid},
		{name: "leading_comma", input: ",80", wantErr: ErrScenarioPortInvalid},
		{name: "trailing_comma", input: "80,", wantErr: ErrScenarioPortInvalid},
		{name: "empty_element", input: "80,,443", wantErr: ErrScenarioPortInvalid},
		{name: "blank_element", input: "80, ,443", wantErr: ErrScenarioPortInvalid},
		{name: "garbage", input: "abc", wantErr: ErrScenarioPortInvalid},
		{name: "fractional_port", input: "1.2", wantErr: ErrScenarioPortInvalid},
		{name: "fractional_start", input: "1.2-3", wantErr: ErrScenarioPortInvalid},
		{name: "fractional_end", input: "1-2.3", wantErr: ErrScenarioPortInvalid},
		{name: "embedded_space", input: "8 0", wantErr: ErrScenarioPortInvalid},
		{name: "zero_port", input: "0", wantErr: ErrScenarioPortInvalid},
		{name: "negative_port", input: "-1", wantErr: ErrScenarioPortInvalid},
		{name: "port_above_range", input: "65536", wantErr: ErrScenarioPortInvalid},
		{name: "integer_overflow", input: "999999999999999999999999999999", wantErr: ErrScenarioPortInvalid},
		{name: "reversed_range", input: "82-80", wantErr: ErrScenarioPortInvalid},
		{name: "missing_start", input: "-80", wantErr: ErrScenarioPortInvalid},
		{name: "missing_end", input: "80-", wantErr: ErrScenarioPortInvalid},
		{name: "extra_separator", input: "80-81-82", wantErr: ErrScenarioPortInvalid},
		{name: "invalid_start", input: "abc-82", wantErr: ErrScenarioPortInvalid},
		{name: "invalid_end", input: "80-abc", wantErr: ErrScenarioPortInvalid},
		{name: "zero_start", input: "0-80", wantErr: ErrScenarioPortInvalid},
		{name: "start_above_range", input: "65536-65537", wantErr: ErrScenarioPortInvalid},
		{name: "end_above_range", input: "65535-65536", wantErr: ErrScenarioPortInvalid},
		{name: "invalid_after_range", input: "80-82,abc", wantErr: ErrScenarioPortInvalid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePorts(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("parsePorts(%q) error = %v, want %v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Errorf("parsePorts(%q) = %v on error, want nil", tc.input, got)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("parsePorts(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
