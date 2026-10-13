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

HTTP generator unit tests and HTTP/1.1 versus HTTP/2 benchmarks are
pending. Configuration and metrics already have unit tests.

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
| `--rps` | `100` | Target request rate across all workers; `0` means unlimited |
| `--duration` | `30s` | Run duration |
| `--workers` | `10` | Number of workers, from 1 to 1000 |
| `--dry-run` | `false` | Build requests without sending them |

RPS is a target, not a guaranteed achieved rate: server latency and the
available workers can limit throughput.

An explicit path other than `/` is preserved. An empty path or `/` is
replaced with a random path for every request. User-Agent values are
selected from a fixed pool. If your server only serves known paths,
specify one: random paths may return 404, which counts as an error.

Ctrl+C or SIGTERM cancels the run and in-flight requests, then prints the
final summary. Dry-run follows the same duration and rate settings but
leaves all request counters at zero.

### Metrics

Metrics are printed every two seconds and once at the end:

- `sent`: completed request attempts, including failed attempts.
- `errors`: failed attempts, including HTTP 4xx/5xx responses.
- `bytes`: response body bytes from successful attempts; excludes headers
  and protocol overhead.
- `avg latency`: mean duration of successful attempts, including reading
  the response body, displayed rounded to milliseconds.

Requests cancelled when the run ends can increase the error count.
Request errors currently appear only in the counters; they do not make
the process exit with a nonzero status. Invalid configuration and
generator setup errors do produce a nonzero exit status.

## License

MIT — see [LICENSE](LICENSE).

Use this tool only against infrastructure you own or have permission
to test.
