// Package portscan performs sequential TCP connect scans of a single host.
package portscan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Config defines the target, connection timeout, and scan pacing.
type Config struct {
	// Host is a hostname or IP address without a scheme, port, or IPv6 brackets.
	Host string
	// Ports is a nonempty list of ports in scan order; duplicates are preserved.
	Ports []int
	// Timeout limits each connection attempt and must be positive.
	Timeout time.Duration
	// ProbesPerSecond sets ticker-based pacing; zero disables pacing.
	// Slow connections and scheduling can reduce the actual rate.
	ProbesPerSecond int
}

// Result describes one attempted TCP connection.
// Err is nil only if both connecting and closing the connection succeeded.
type Result struct {
	Port int
	Err  error
}

var (
	// ErrInvalidConfig identifies invalid scan settings.
	ErrInvalidConfig = errors.New("invalid config")
)

// probe connects and immediately closes without sending application data.
func probe(ctx context.Context, address string, timeout time.Duration) error {
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	n, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	err = n.Close()
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

// Run validates cfg and probes its ports sequentially, starting the first immediately.
// Probe errors are recorded in results and do not stop the scan. Cancellation
// returns accumulated results, including any interrupted probe, and ctx.Err().
func Run(ctx context.Context, cfg Config) ([]Result, error) {
	var res []Result

	err := cfg.Validate()
	if err != nil {
		return res, err
	}

	var ticket <-chan time.Time
	if cfg.ProbesPerSecond > 0 {
		ticker := time.NewTicker(time.Second / time.Duration(cfg.ProbesPerSecond))
		ticket = ticker.C
		defer ticker.Stop()
	}

	for i, p := range cfg.Ports {
		if ticket != nil && i > 0 {
			select {
			case <-ticket:
			case <-ctx.Done():
				return res, ctx.Err()
			}
		}

		// A ready ticket can be selected even when cancellation is also ready.
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		err = probe(ctx, net.JoinHostPort(cfg.Host, strconv.Itoa(p)), cfg.Timeout)
		res = append(res, Result{Port: p, Err: err})
	}

	return res, ctx.Err()
}

// Validate checks scan settings without resolving names or contacting the target.
// It checks host formatting, not the complete DNS hostname syntax.
func (c Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("%w: host cannot be empty", ErrInvalidConfig)
	}

	if strings.ContainsFunc(c.Host, unicode.IsSpace) {
		return fmt.Errorf("%w: host cannot contain spaces", ErrInvalidConfig)
	}

	if strings.ContainsAny(c.Host, "/?@#") {
		return fmt.Errorf("%w: host cannot contain any %q", ErrInvalidConfig, "/?@#")
	}

	// Colons are valid in IPv6 addresses, but not in the hostname fallback.
	if _, err := netip.ParseAddr(c.Host); err != nil {
		if strings.Contains(c.Host, ":") {
			return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
	}

	if len(c.Ports) == 0 {
		return fmt.Errorf("%w: ports cannot be empty", ErrInvalidConfig)
	}

	for i, p := range c.Ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%w: port %d = %d not in range (1-65535)", ErrInvalidConfig, i, p)
		}
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be greater than zero", ErrInvalidConfig)
	}

	if c.ProbesPerSecond < 0 {
		return fmt.Errorf("%w: probes per second cannot be < 0", ErrInvalidConfig)
	}

	if c.ProbesPerSecond > 0 {
		if time.Second/time.Duration(c.ProbesPerSecond) == 0 {
			return fmt.Errorf("%w: probes per second is too large", ErrInvalidConfig)
		}
	}

	return nil
}
