# traffic-gen

[![CI](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wild-sergunys/traffic-gen)](go.mod)

HTTP/1.1 and HTTP/2 traffic generator for load testing and detector
validation. Written in Go, with no external Go dependencies.

## Why

The project is being developed as a traffic source for IDS experiments.
The goal is to generate normal traffic and attack scenarios with one
tool, then replay the same scenarios against different detectors.

The current implementation generates HTTP requests. Attack modes,
multi-phase YAML scenarios, and PCAP replay are planned.

## Status

Implemented:

- HTTP/1.1 over HTTP or HTTPS.
- HTTP/2 over HTTPS, with no fallback to HTTP/1.1.
- Cleartext HTTP/2 (h2c) using prior knowledge: the target must support
  HTTP/2 without an HTTP/1.1 upgrade handshake.
- A shared worker pool, configurable request rate, and connection reuse.
- Periodic metrics, a final summary, and context-based cancellation.
- Dry-run mode, which builds requests without sending them.

Unit and local integration tests cover configuration, metrics, request
construction, response handling, HTTP protocols, dry-run, and cancellation.
A dedicated test of the shared RPS limit across multiple active workers
is still pending.

## Build

Requires Go 1.26.2 or newer and `make`.

```sh
make build
./bin/traffic-gen --help
```

## Usage

Each run sends GET requests to one target. Select the protocol explicitly:

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
| `--target` | Required | Target URL |
| `--mode` | `http1` | `http1`, `http2`, or `h2c` |
| `--rps` | `100` | Target request rate across all workers, from 0 to 10,000,000; `0` means unlimited |
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

A password supplied as `user:password@host` in the target URL is masked in
the configuration summary and target validation errors. The original target
is retained for requests.

Each worker selects a User-Agent from a fixed pool once and keeps it
for the entire run. Workers share the transport and connection pool;
they do not represent independent browser sessions.

Workers check cancellation before each unit of work. If cancellation happens
after that check, a live attempt may still start and is included in the metrics.

Ctrl+C or SIGTERM cancels the run and in-flight requests, then prints the
final summary. Dry-run follows the same duration and rate settings but
leaves all request counters at zero.

### Metrics

Metrics are printed every two seconds and once at the end. During a run,
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
generator setup errors do produce a nonzero exit status.

## License

MIT — see [LICENSE](LICENSE).

Use this tool only against infrastructure you own or have permission
to test.
