# CLI attach-warm latency versus spike G1

Linear [42-38](https://linear.app/42-golems/issue/42-38/compare-cli-attach-latency-against-the-actual-spike-g1-number-not-the),
follow-up to [42-24](https://linear.app/42-golems/issue/42-24/cli-zeroth-run-attach-bg-plus-websocket-streaming) / PR #30.
Requirement: Z1-044.

The design-doc NFR bar is "first live token in under 2s." The BA-6 spike
measured G1 attach-warm at p50 **5.403ms** / p95 6.137ms / p99 6.214ms over
110 runs ([RESULTS.md](../spike/RESULTS.md), Linear [42-6](https://linear.app/42-golems/issue/42-6/gate-g1-g6-session-event-log-attach-latency-sqlite-throughput)).
`TestAttachLatencyWarm` in `internal/server` still enforces the 2s ceiling as
a coarse hang detector. This note is the durable record that a real
`zeroth attach` was measured the same way the spike was (warm-up, then a
real sample size) and compared to that 5.403ms p50, not only to 2s.

## Method

Same plan as `go run ./cmd/spike bench` for G1 warm:

- 10 warm-up attaches, discarded
- 110 timed samples
- Percentiles are of those 110, linear interpolation as in `zeroth-spike/bench`
- Token interval 5ms
- Replay last 20 events (`--last 20`)
- Warm: attach to an already-streaming session
- Clock: start of `zeroth attach` (Cobra `ExecuteContext`) to the first live
  token (event seq greater than the pre-attach snapshot, type `log`, message
  prefix `token-`)

The CLI path is the product command, not a raw WebSocket dial: `GET /runs/{id}`,
`POST /runs/{id}/foreground`, then `GET /runs/{id}/events` over WebSocket
(replay, then live tail). The daemon is `internal/server` on loopback
`httptest`, SQLite WAL. Process exec of the `zeroth` binary is out of scope:
the extra cost called out in 42-38 is the HTTP/WebSocket hop the spike's
in-process bench did not have.

Re-run (uninstrumented; this is the number to compare to the spike):

```bash
go test ./cmd/zeroth -run TestCLIAttachLatencyWarm -v -count=1
```

Race-instrumented CI uses a 2 warm-up + 8 sample smoke (same size as
`zeroth-spike/bench.TestG1G6Smoke`) because 110 samples under `-race` take
about 85s and inflate p50 into hundreds of milliseconds. Force the full
sample under race with `ZEROTH_ATTACH_BENCH=1`.

## Result

| | |
| --- | --- |
| UTC | 2026-08-21T22:02:27Z |
| Host | Linux 6.12.94+, x86_64, Go 1.27.0 |
| Command | `go test ./cmd/zeroth -run TestCLIAttachLatencyWarm -v -count=1` |
| Race | no |
| Git | this branch (Linear 42-38) |
| Result | **PASS** (1.68s) |

```
=== RUN   TestCLIAttachLatencyWarm
    attach_latency_test.go: cli attach warm 1/120 last=2.149885ms
    attach_latency_test.go: cli attach warm 20/120 last=2.995629ms
    attach_latency_test.go: cli attach warm 40/120 last=3.628204ms
    attach_latency_test.go: cli attach warm 60/120 last=5.456869ms
    attach_latency_test.go: cli attach warm 80/120 last=8.397198ms
    attach_latency_test.go: cli attach warm 100/120 last=12.562837ms
    attach_latency_test.go: cli attach warm 120/120 last=19.685559ms
    attach_latency_test.go: CLI attach warm n=110 p50=6.240178ms p95=19.372483ms p99=24.395789ms max=32.290484ms min=2.435902ms
    attach_latency_test.go: spike G1 warm p50=5.403ms; CLI/spike p50 ratio=1.15x
--- PASS: TestCLIAttachLatencyWarm (1.68s)
```

| Path | n | p50 | p95 | p99 | max | vs spike p50 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Spike G1 attach (warm, in-process WS) | 110 | 5.403 ms | 6.137 ms | 6.214 ms | 6.237 ms | 1x |
| CLI `zeroth attach` (warm) | 110 | 6.240 ms | 19.372 ms | 24.396 ms | 32.290 ms | **1.15x** |

p50 is **1.15x** the spike (6.240ms / 5.403ms). That is the expected
direction: the CLI adds one HTTP GET and one POST foreground before the
WebSocket the spike dialed directly. On loopback those hops are
sub-millisecond, so the 5ms token interval still dominates the median, as it
did in the spike. This is not a two-to-three-order regression against G1.

p95 is 3.16x the spike (19.372ms / 6.137ms). Later samples in the 120-attach
series slow down (2.1ms at sample 1, 19.7ms at sample 120) as the session
event log grows and each attach constructs a Zap logger and prints the
replay window. The tail is extra CLI work plus SQLite range-read cost, still
tens of milliseconds, not the 500ms-1.5s band that the 2s design bar would
have hidden.

The 2s NFR ceiling still holds (max 32.290ms). Uninstrumented CI-equivalent
gates on this run: p50 < 50ms, p95 < 100ms.
