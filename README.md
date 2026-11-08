# traffic-gen

[![CI](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wild-sergunys/traffic-gen)](go.mod)

HTTP/1.1 and HTTP/2 traffic generator with TCP connect scanning and SQLi/XSS
request scenarios for load
testing and IDS experiments. Written in Go; YAML configuration uses
`go.yaml.in/yaml/v3`.

## Why

The project is being developed as a traffic source for IDS experiments.
The goal is to generate normal traffic and attack scenarios with one
tool, then replay the same scenarios against different detectors.

The current implementation generates simple GET or mixed GET/POST background traffic, scans TCP
ports, and sends SQLi/XSS attempts using GET queries and POST forms.
Additional attack scenarios and dataset-based traffic are planned.

## Status

Implemented:

- HTTP/1.1 over HTTP or HTTPS.
- HTTP/2 over HTTPS, with no fallback to HTTP/1.1.
- Cleartext HTTP/2 (h2c) using prior knowledge: the target must support
  HTTP/2 without an HTTP/1.1 upgrade handshake.
- A shared worker pool, configurable request rate, and connection reuse.
- Periodic metrics, a final summary, and context-based cancellation.
- YAML configuration for background traffic and an optional portscan, SQLi, or XSS scenario.
- Built-in SQLi/XSS payloads or custom lists, sent through GET and/or POST.
- An optional deterministic web background with ordinary searches, forms, and special characters.
- Separate source-informed SQLi/XSS control examples for detector evaluation.
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
./bin/traffic-gen --config examples/sqli.yaml
./bin/traffic-gen --config examples/xss.yaml
./bin/traffic-gen --config examples/web-background.yaml
./bin/traffic-gen --config examples/control-sqli.yaml
./bin/traffic-gen --config examples/control-xss.yaml
```

The examples demonstrate [HTTP only](examples/http.yaml),
[a port scan only](examples/scan-only.yaml), and
[HTTP background with a port scan](examples/portscan.yaml),
[SQLi only](examples/sqli.yaml), and [HTTP background with XSS](examples/xss.yaml). Adjust their
addresses and ports for your test environment.

Either `background` or `scenario` may be omitted, but at least one must be
present. One scenario (`portscan`, `sqli`, or `xss`) is supported per run. The port expression
preserves order and duplicates and expands inclusive ranges such as `8000-8010`.

`--config` cannot be combined with HTTP flags, including `--duration` and
`--dry-run`. Set those values in YAML when using a file. `--help` and `--version` remain
available without loading the file. Without `--config`, the HTTP flags below
continue to work.

Defaults for omitted fields:

- Run: `duration: 30s`, `dry_run: false`.
- Background: `profile: simple` (also selected by an empty value), `mode: http1`, `rps: 100`, `workers: 10`, `request_timeout: 5s`; `target` is required.
- Port scan: `timeout: 1s`, `probes_per_second: 0`; `host` and `ports` are required.
- SQLi/XSS (`scenario.http`): `mode: http1`, `rps: 10`, `workers: 1`,
  `request_timeout: 5s`, `methods: [GET, POST]`, `parameter: q`; `target` is required.
  Omit `payloads` to use the built-in profile.

Explicit zero rates mean unlimited traffic; zero durations or worker counts
are invalid. Durations and port expressions must be strings, rates and worker
counts integers, and `dry_run` a boolean. Only one YAML document is accepted.
Nulls, aliases, merge keys, duplicate keys, and unknown fields are rejected.

HTTP `request_timeout` (in `background` or `scenario.http`) bounds each request, including reading its
response body. A timed-out attempt is counted as an error and the worker continues.
Zero disables this additional limit; negative values are rejected. The common
run context can always stop a request earlier.

The top-level duration limits the entire run. HTTP background and the scan
start independently; finishing the scan does not stop HTTP background. A
scan-only run exits as soon as its port list is exhausted or the common limit
is reached. `probes_per_second: 0` removes pacing; positive values pace
connection attempts, starting the first immediately. Slow connections can
reduce the achieved scan rate.

Dry-run skips all network activity. With any HTTP traffic source enabled it still
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

### Normal web background and control inputs

Use `background.profile: web` in YAML or `--profile web` with HTTP flags.
The existing `simple` profile remains the default. A background profile cannot
be set inside `scenario.http`; that block already selects an attack workload.

The web profile repeats eight requests: five GETs and three POST forms. They
cover health checks, search, documentation lookup, and feedback. Text includes
names with apostrophes, double quotes, comparison operators, ampersands,
percent/plus signs, and Unicode. This helps exercise false-positive handling
without putting executable injection expressions into the normal profile.

Paths `/health`, `/search`, `/docs`, and `/feedback` are appended below the
configured target path: target `http://host/app/?device=7` produces paths such
as `/app/search`. Existing query parameters are retained; a GET replaces its
selected field (`q` or `check`), whereas a POST sends `q` or `message` in a form
without replacing URL query fields. The text is encoded once during request
construction. Workers share a deterministic cycle, but arrival order can vary.
Background redirects follow the existing client policy.

This is synthetic traffic, not simulated browser sessions or a production
industrial-network distribution. The server must provide suitable endpoints
if successful application responses are required; a 404 still counts as a
received HTTP response. Feedback POSTs can change server state, so select
endpoints intended for experiments.

[Control corpus documentation](docs/validation-corpus.md) describes the separate
`control-sqli.yaml` and `control-xss.yaml` inputs, their OWASP sources, and how to
keep them separate from rule development. These files use the existing custom
payload mechanism. They are not a captured dataset or proof of detector quality.

### SQLi and XSS scenarios

Both scenario types use the same HTTP client and worker implementation as
background traffic, with independent targets, rates, workers, and metrics.
They run until the common duration expires or the caller cancels the run.
The built-in lists contain three illustrative payloads each; they are not a
comprehensive attack corpus or a guarantee of matching a particular IDS rule.

```yaml
duration: 10s
scenario:
  type: sqli
  http:
    target: http://127.0.0.1:8080/search?device=7
    methods: [GET, POST]
    parameter: q
    payloads:
      - "' OR '1'='1' -- "
      - "a&b + %"
```

A custom `payloads` list replaces the built-in list. Values are raw strings,
not pre-encoded URL fragments. Empty lists and empty strings are rejected;
quote numeric-looking values to keep them strings. Only uppercase `GET` and
`POST` are supported. Each payload visits every listed method before moving
to the next payload, then the cycle repeats. Workers share the cycle;
concurrent requests may reach the server in a different order.

GET sets `parameter` in the query, replacing any existing values of that key.
Other query parameters are preserved, though their URL encoding/order may
change. POST leaves the URL query intact and sends the field in an
`application/x-www-form-urlencoded` body. Special characters are encoded so
that form/query decoding recovers the original payload. The target path stays
fixed; scenarios do not generate random paths. Malformed URL queries are
rejected during scenario setup. Redirects are not followed.

These scenarios produce attempts visible to a traffic detector. An HTTP 200,
403, or 500 response does not establish whether an injection succeeded.
No browser executes XSS, and no database outcome is verified. SYN flood,
Slowloris, dataset conversion, and multiple simultaneous scenarios are outside
the current implementation.

## Build

Requires Go 1.26.2 or newer and `make`.

```sh
make build
./bin/traffic-gen --help
./bin/traffic-gen --version
```

## Usage

The default background profile sends GET requests to one target. The optional
`web` profile sends a fixed mix of GET requests and POST forms. Select the protocol explicitly:

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

An explicit target port must be between 1 and 65535; an omitted port uses
the scheme default. An empty explicit port (`http://host:/`) is rejected.

The selected mode is enforced. A scheme that does not match the mode is
rejected before the run. Protocol negotiation and connection failures
are counted as request errors.

Builds from `make build` and release archives include version, commit, and build
date in `--version` output. Plain `go build` uses `dev`, `none`, and `unknown`.
Release archives include the YAML examples under `examples/`.

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--version` | `false` | Print build information and exit without loading a config or sending traffic |
| `--config` | Unset | YAML file; cannot be combined with HTTP settings flags |
| `--target` | Required without `--config` | HTTP target URL |
| `--profile` | `simple` | Background profile: `simple` or `web`; empty also means `simple` |
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

With the default `simple` background profile, an explicit path, including `/`,
and query parameters are preserved.
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

Each enabled HTTP source prints separate metrics every two seconds and once
at the end. Background output retains `sent=...` and `final: ...`; scenario
output uses `sqli: ...` / `sqli final: ...` or `xss: ...` / `xss final: ...`.
Port scan results are printed after all tasks finish. During a run,
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
response status. The background HTTP client follows redirects, so its status counts describe
the final response rather than every redirect hop. SQLi/XSS scenarios do not
follow redirects; their counters describe the response from the selected endpoint.

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
