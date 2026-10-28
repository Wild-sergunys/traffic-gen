# traffic-gen

[![CI](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wild-sergunys/traffic-gen)](go.mod)

HTTP/1.1 and HTTP/2 traffic generator with TCP connect scanning for load
testing and IDS experiments. Written in Go; YAML configuration uses
`go.yaml.in/yaml/v3`.

## Why

The project is being developed as a traffic source for IDS experiments.
The goal is to generate normal traffic and attack scenarios with one
tool, then replay the same scenarios against different detectors.

The current implementation generates HTTP GET traffic and scans TCP ports.
Additional attack scenarios and dataset-based traffic are planned.

## Status

Implemented:

- HTTP/1.1 over HTTP or HTTPS.
- HTTP/2 over HTTPS, with no fallback to HTTP/1.1.
- Cleartext HTTP/2 (h2c) using prior knowledge: the target must support
  HTTP/2 without an HTTP/1.1 upgrade handshake.
- A shared worker pool, configurable request rate, and connection reuse.
- Periodic metrics, a final summary, and context-based cancellation.
- YAML configuration for HTTP traffic, one port scan, or both together.
- Sequential TCP connect scanning with configurable ports, timeout, and pacing.
- Dry-run mode without HTTP requests or scan connections.

Unit and local integration tests cover configuration, metrics, request
construction, response handling, HTTP protocols, combined execution,
dry-run, and cancellation.

## YAML configuration

Build the binary, then select a configuration file:

```sh
./bin/traffic-gen --config examples/http.yaml
./bin/traffic-gen --config examples/scan-only.yaml
./bin/traffic-gen --config examples/portscan.yaml
```

The examples demonstrate [HTTP only](examples/http.yaml),
[a port scan only](examples/scan-only.yaml), and
[HTTP background with a port scan](examples/portscan.yaml). Adjust their
addresses and ports for your test environment.

Either `background` or `scenario` may be omitted, but at least one must be
present. Only one scenario, `portscan`, is supported per run. The port expression
preserves order and duplicates and expands inclusive ranges such as `8000-8010`.

`--config` cannot be combined with HTTP flags, including `--duration` and
`--dry-run`. Set those values in YAML when using a file. `--help` remains
available without loading the file. Without `--config`, the HTTP flags below
continue to work.

Defaults for omitted fields:

- Run: `duration: 30s`, `dry_run: false`.
- Background: `mode: http1`, `rps: 100`, `workers: 10`, `request_timeout: 5s`; `target` is required.
- Port scan: `timeout: 1s`, `probes_per_second: 0`; `host` and `ports` are required.

Explicit zero rates mean unlimited traffic; zero durations or worker counts
are invalid. Durations and port expressions must be strings, rates and worker
counts integers, and `dry_run` a boolean. Only one YAML document is accepted.
Nulls, aliases, merge keys, duplicate keys, and unknown fields are rejected.

HTTP `background.request_timeout` bounds each request, including reading its
response body. A timed-out attempt is counted as an error and the worker continues.
Zero disables this additional limit; negative values are rejected. The common
run context can always stop a request earlier.

The top-level duration limits the entire run. HTTP background and the scan
start independently; finishing the scan does not stop HTTP background. A
scan-only run exits as soon as its port list is exhausted or the common limit
is reached. `probes_per_second: 0` removes pacing; positive values pace
connection attempts, starting the first immediately. Slow connections can
reduce the achieved scan rate.

Dry-run skips all network activity. With HTTP background enabled it still
builds requests for the configured duration and keeps HTTP counters at zero.
A scan-only dry-run validates the settings, prints the dry-run message, and
exits without waiting for the duration.

A scan prints successfully connected ports in scan order, preserving duplicates,
followed by totals. If no connection succeeded, the list is `none`. Individual
connection errors are counted rather than printed:

```text
portscan: connected ports: 22, 80, 443
portscan: attempted=1000 succeeded=3 failed=997
portscan: host="127.0.0.1" started=2026-10-28T12:00:00Z finished=2026-10-28T12:00:01Z reason=completed
```

The timing line identifies the configured host and the scan execution interval
in UTC (RFC 3339 with fractional seconds as needed). Times are recorded around
the scanner call, before waiting for HTTP background; they are not packet-capture
timestamps. Reasons are `completed` (port list exhausted), `deadline` (context
time limit), `canceled`, or `error`. Individual connection failures do not change
`completed` to `error`. Dry-run emits no scan interval.

The totals report `attempted`, `succeeded`, and `failed`. Success means
connecting and closing completed without error; failures include refused
connections, timeouts, and interrupted attempts. A failed attempt does not
by itself establish that a port is closed. Individual failures do not stop
the scan or make the process exit with an error.

## Build

Requires Go 1.26.2 or newer and `make`.

```sh
make build
./bin/traffic-gen --help
```

## Usage

HTTP traffic sends GET requests to one target. Select the protocol explicitly:

| Mode | Target scheme | Protocol |
|------|---------------|----------|
| `http1` | `http://` or `https://` | HTTP/1.1 |
| `http2` | `https://` | HTTP/2 over TLS |
| `h2c` | `http://` | HTTP/2 without TLS |

```sh
# HTTP/1.1
./bin/traffic-gen --target http://127.0.0.1:8080/health --mode http1 --rps 100 --duration 30s

# HTTP/2 over TLS; the server must support HTTP/2 and have a trusted certificate
./bin/traffic-gen --target https://localhost:8443/health --mode http2 --rps 100 --duration 30s

# Cleartext HTTP/2; the server must accept h2c with prior knowledge
./bin/traffic-gen --target http://127.0.0.1:8080/health --mode h2c --rps 100 --duration 30s
```

The selected mode is enforced. A scheme that does not match the mode is
rejected before the run. Protocol negotiation and connection failures
are counted as request errors.

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--config` | Unset | YAML file; cannot be combined with HTTP settings flags |
| `--target` | Required without `--config` | HTTP target URL |
| `--mode` | `http1` | `http1`, `http2`, or `h2c` |
| `--rps` | `100` | Target request rate across all workers, from 0 to 10,000,000; `0` means unlimited |
| `--request-timeout` | `5s` | Limit one HTTP exchange, including body reads; `0` leaves only the run limit |
| `--duration` | `30s` | Run duration |
| `--workers` | `10` | Number of workers, from 1 to 1000 |
| `--dry-run` | `false` | Build requests without sending them |

RPS is a target, not a guaranteed achieved rate. One shared ticker paces
all workers; each tick permits one request attempt. Server latency, worker
availability, and timer scheduling can reduce the achieved rate. Ticks may
be dropped when workers cannot keep up; missed ticks are not accumulated
for later replay. The accepted maximum is a configuration limit, not a
throughput guarantee.

An explicit path, including `/`, and query parameters are preserved.
For example, `--target 'http://127.0.0.1:8080/?device=7'` sends that path
and query unchanged. Quote URLs containing shell-special characters
such as `&`. A random path is generated only when both the path and
query are empty. If your server only serves known paths, specify one
instead of generating random paths.

Target validation errors omit or mask passwords supplied as
`user:password@host` in the URL. The original target is retained for requests.

Each worker selects a User-Agent from a fixed pool once and keeps it
for the entire run. Workers share the transport and connection pool;
they do not represent independent browser sessions. Once all workers finish,
the generator closes idle HTTP connections; a later run can open new ones.

Workers check cancellation before each unit of work. If cancellation happens
after that check, a live attempt may still start and is included in the metrics.

Ctrl+C or SIGTERM cancels enabled tasks and in-flight operations, waits for
background goroutines, and prints final summaries. Cancellation and the common
time limit are normal completion, with exit status zero.

### Metrics

When HTTP background is enabled, metrics are printed every two seconds and
once at the end. Port scan results are printed after both tasks finish. During a run,
counters are read independently, so a report may combine values from
different instants. The final summary is read after all workers finish
and no longer update the counters.

The reported values are:

- `sent`: completed request attempts, including failed attempts.
- `errors`: request construction, connection, TLS, transfer, or response
  body read failures. HTTP 4xx/5xx status codes alone are not errors.
- `2xx`, `3xx`, `4xx`, `5xx`: response counts by HTTP status class. A
  received status is counted even if reading its body subsequently fails.
- `bytes`: all response body bytes actually read, including partial bodies
  on failed attempts; excludes headers and protocol overhead.
- `avg latency`: mean duration of attempts whose response bodies were
  fully read, including HTTP 4xx/5xx responses, displayed rounded to
  milliseconds. Failed attempts are excluded; with no completed response,
  the value is zero.

Status counts and errors can overlap: a 200 response with a truncated
body increments both `2xx` and `errors`. A connection failure has no
response status. The HTTP client follows redirects by default, so status
counts describe the final response rather than every redirect hop.

Requests cancelled when the run ends can increase the error count.
Request errors currently appear only in the counters; they do not make
the process exit with a nonzero status. Invalid configuration and
generator setup errors do produce a nonzero exit status, as do failures to
write periodic reports or final summaries. A periodic report write failure
cancels enabled tasks, waits for their shutdown, and returns a nonzero status.

## Benchmarks

See the [HTTP/1.1 and HTTP/2 benchmark report](docs/benchmarks/http-protocols.md)
for the full-generator workload, reproduction command, results, and limitations.

## License

MIT — see [LICENSE](LICENSE).

Use this tool only against infrastructure you own or have permission
to test.
