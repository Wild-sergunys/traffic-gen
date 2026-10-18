# HTTP/1.1 and HTTP/2 throughput

## Workload

Measured on 2026-10-18 with Go 1.26.2 on Linux/amd64 and an Intel Core
i7-11800H (8 cores, 16 logical CPUs).

Both modes use HTTPS over loopback, GET `/`, and a fixed 1,024-byte response
with status 200. The client trusts the test server certificate. The client
and server run in the same process with `GOMAXPROCS=4`.

Each benchmark operation runs the full generator for 10 seconds, with
4 or 32 workers and `RPS=0`. Server and client setup is excluded from timing.
There is no connection warmup: initial TCP/TLS connections are included.
Each sample creates a fresh server and client; there are five samples per case.

## Reproduce

From the repository root:

```bash
GOTOOLCHAIN=go1.26.2 go test ./internal/generator \
  -run '^$' -bench '^BenchmarkRun$' \
  -benchtime=1x -count=5 -cpu=4 -timeout=5m
```

`-benchtime=1x` selects one complete generator run per sample. The command
takes approximately 200 seconds. Run without `-race` for performance figures;
race detection adds overhead.

## Results

| Mode | Workers | Median successful requests/s | Observed range, requests/s | Errors/run, range |
|---|---:|---:|---:|---:|
| HTTP/1.1 | 4 | 81,254 | 74,715–85,738 | 1–3 |
| HTTP/2 | 4 | 26,075 | 25,629–26,462 | 3–4 |
| HTTP/1.1 | 32 | 82,507 | 79,553–86,798 | 8–18 |
| HTTP/2 | 32 | 52,396 | 51,958–52,730 | 27–32 |

See [the original output](http-protocols-2026-10-18.txt) for all samples.

`req/s` is `(Sent - Errors) / measured elapsed seconds`. `errors/run` is the
number of failed attempts per complete generator run. Errors can include
requests interrupted at the duration deadline; their individual causes
were not collected, so they cannot all be attributed to cancellation.
`ns/op` describes one complete run, not the latency of one HTTP request.

## Interpretation and limitations

HTTP/1.1 achieved higher throughput in this workload. Increasing workers
from 4 to 32 substantially increased HTTP/2 throughput, while HTTP/1.1
changed little. This does not establish that either protocol is generally
faster: network latency, response size, concurrency, and server behavior
can change the comparison.

The measurements include the generator worker pool and metrics, as well
as the local server's CPU cost. They do not isolate generator capacity,
measure packet rate, or predict IDS throughput. Samples for each case run
consecutively; thermal changes and background load can affect the results.
The medians and ranges are descriptive, not a statistical significance test.
The benchmark rejects runs with no successful requests but does not classify
individual request errors. That check was added after the recorded samples;
all recorded samples have successful requests.

No h2c throughput measurements are included. HTTP/2 and h2c protocol behavior
is covered separately by integration tests.
