# traffic-gen

[![CI](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/wild-sergunys/traffic-gen/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wild-sergunys/traffic-gen)](go.mod)

HTTP/1.1 and HTTP/2 traffic generator for load testing and detector
validation. Single binary, no runtime dependencies.

## Why

There is no shortage of traffic generation tools. `packit` and
`Bit-Twist` can craft raw packets for IDS testing. `hey`, `vegeta`,
`wrk`, and `h2load` cover HTTP load testing. DDoS toolkits like
`FAST-DDoS` or `netstorm-pro` generate SYN floods and HTTP floods on
demand.

What is missing is a tool that combines all three in a single standalone
binary, with reproducible scenarios instead of one-off attacks.
`traffic-gen` is built around that gap:

- **One binary, no runtime dependencies.** Written in Go, compiles to
  a static executable. No Python environment, no libpcap headers to
  install, no framework to configure.
- **Scenario files, not one-off commands.** A single YAML file will
  describe a sequence of phases — port scan, SYN flood, HTTP flood,
  slowloris — and can be replayed against different detectors so the
  results can be compared.
- **Built for detector validation.** Metrics are shaped around what an
  IDS sees, not just what a load balancer sees.

## Status

Early development. The HTTP/1.1 generator works end to end: it drives
traffic against a single target at a configurable rate, pools
connections, counts sent/errors/bytes and average latency, treats
4xx/5xx responses as errors, and supports a dry-run mode. HTTP/2,
scenario files, and the port-scan and SYN-flood phases are not
implemented yet.

## Usage

    traffic-gen --target http://127.0.0.1:8080 --mode http1 --rps 100 --duration 30s

All flags: `--target`, `--mode`, `--rps` (0 = unlimited), `--duration`,
`--workers`, `--dry-run`. See `--help` for defaults.

An explicit path in `--target` is sent as-is; without one, every request
uses a random path so the stream does not look like a single client.
Servers that only serve known paths will answer the randomised requests
with 404, which counts as an error — pass an explicit path in that case.

## License

MIT — see [LICENSE](LICENSE).

This tool is meant for testing infrastructure you own or have written
permission to test. Running it against third-party systems without
authorization is illegal.