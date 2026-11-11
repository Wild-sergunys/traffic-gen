package runner

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/wild-sergunys/traffic-gen/internal/config"
	"github.com/wild-sergunys/traffic-gen/internal/portscan"
)

// dashboard belongs to the reporter until it stops, then to the final writer.
// Only scan progress is shared with a running task.
type dashboard struct {
	out      io.Writer
	tasks    []*httpTask
	scan     *scanProgress
	start    time.Time
	duration time.Duration
	previous time.Time
	sent     []int64
	fd       int
	width    int
	lines    int
	color    bool
}

func newDashboard(out io.Writer, cfg *config.RunConfig, tasks []*httpTask, start time.Time) *dashboard {
	file, ok := out.(*os.File)
	if !ok || cfg.DryRun || os.Getenv("TERM") == "dumb" {
		return nil
	}
	fd := int(file.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	width, height, err := term.GetSize(fd)
	if err != nil || width < 40 || height < 16 {
		return nil
	}
	d := &dashboard{out: out, tasks: tasks, start: start, duration: cfg.Duration,
		previous: start, sent: make([]int64, len(tasks)), fd: fd, width: width,
		color: os.Getenv("NO_COLOR") == ""}
	if cfg.Scenario != nil && cfg.Scenario.Portscan != nil {
		d.scan = &scanProgress{total: len(cfg.Scenario.Portscan.Ports)}
	}
	return d
}

func (d *dashboard) write(now time.Time, state string) error {
	if d.fd >= 0 {
		width, height, err := term.GetSize(d.fd)
		if err == nil && width > 1 {
			// Resizing can reflow old rows. Start a fresh block rather than
			// moving the cursor into unrelated terminal history.
			if width != d.width || d.lines >= height {
				d.lines = 0
			}
			d.width = width
		}
	}
	elapsed := now.Sub(d.start)
	lines := []string{"traffic-gen | " + state, ""}
	if len(d.tasks) > 0 {
		percent := min(100, int(float64(elapsed)/float64(d.duration)*100))
		lines = append(lines, progressBar(percent), fmt.Sprintf("elapsed %s / %s", elapsed.Round(time.Second), d.duration))
	} else {
		lines = append(lines, "elapsed "+elapsed.Round(time.Second).String())
	}
	lines = append(lines, "")
	if len(d.tasks) > 0 && d.width >= 76 {
		lines = append(lines, fmt.Sprintf("%-12s %12s %12s %10s %10s", "SOURCE", "REQ/S", "SENT", "ERRORS", "AVG"))
	}
	for i, task := range d.tasks {
		s := task.metrics.Snapshot()
		count, interval := s.Sent-d.sent[i], now.Sub(d.previous)
		if state != "RUNNING" {
			// The final rate covers the whole run, not just its last fraction.
			count, interval = s.Sent, elapsed
		}
		var rate float64
		if interval > 0 {
			rate = float64(count) / interval.Seconds()
		}
		name := "Background"
		switch task.name {
		case "sqli":
			name = "SQLi"
		case "xss":
			name = "XSS"
		}
		latency := s.AvgLatency.Round(time.Microsecond)
		if d.width >= 76 {
			lines = append(lines, fmt.Sprintf("%-12s %12.1f %12d %10d %10s", name, rate, s.Sent, s.Errors, latency))
		} else {
			lines = append(lines, fmt.Sprintf("%s | %.1f req/s", name, rate),
				fmt.Sprintf("sent: %d | errors: %d | avg: %s", s.Sent, s.Errors, latency))
		}
		if state != "RUNNING" {
			lines = append(lines,
				fmt.Sprintf("  2xx: %d | 3xx: %d | 4xx: %d | 5xx: %d", s.StatusClasses[0], s.StatusClasses[1], s.StatusClasses[2], s.StatusClasses[3]),
				fmt.Sprintf("  bytes: %d", s.Bytes))
		}
		d.sent[i] = s.Sent
	}
	d.previous = now
	if d.scan != nil {
		lines = append(lines, d.scan.lines()...)
	}
	if state == "RUNNING" {
		lines = append(lines, "", "Ctrl+C to stop")
	} else if len(d.tasks) > 0 {
		lines = append(lines, "", "REQ/S: average over the whole run")
	}

	var frame strings.Builder
	if d.lines > 0 {
		fmt.Fprintf(&frame, "\r\x1b[%dA\x1b[J", d.lines)
	}
	d.lines = 0
	for i, line := range lines {
		// Leave the last column unused to avoid automatic terminal wrapping.
		for _, row := range wrapLine(line, d.width-1) {
			if d.color && i == 0 {
				frame.WriteString("\x1b[36m")
				frame.WriteString(row)
				frame.WriteString("\x1b[0m\n")
			} else {
				frame.WriteString(row)
				frame.WriteByte('\n')
			}
			d.lines++
		}
	}
	_, err := io.WriteString(d.out, frame.String())
	return err
}

func progressBar(percent int) string {
	filled := percent / 5
	return fmt.Sprintf("[%s%s] %d%%", strings.Repeat("█", filled), strings.Repeat("░", 20-filled), percent)
}

// wrapLine handles only the panel's own text; ANSI styling is added afterwards.
func wrapLine(line string, width int) []string {
	width = max(1, width)
	var lines []string
	runes := []rune(line)
	for len(runes) > width {
		end := width
		for i := width; i > 0; i-- {
			if runes[i] == ' ' {
				end = i
				break
			}
		}
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
		if len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return append(lines, string(runes))
}

type scanProgress struct {
	mu        sync.Mutex
	total     int
	attempted int
	connected int
	ports     []int
}

func (s *scanProgress) record(r portscan.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted++
	if r.Err == nil {
		s.connected++
		// Keep the live preview bounded; the final report lists every success.
		if len(s.ports) < 8 {
			s.ports = append(s.ports, r.Port)
		}
	}
}

func (s *scanProgress) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := []string{"", fmt.Sprintf("Port scan | %d/%d attempted", s.attempted, s.total),
		progressBar(s.attempted * 100 / s.total),
		fmt.Sprintf("connected: %d | failed: %d", s.connected, s.attempted-s.connected)}
	if len(s.ports) > 0 {
		preview := fmt.Sprintf("ports: %v", s.ports)
		if s.connected > len(s.ports) {
			preview += " ..."
		}
		lines = append(lines, preview)
	}
	return lines
}
