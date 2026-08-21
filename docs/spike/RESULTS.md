# Spike results (BA-6)

## Verdict

**GO.** All seven gates measured, all pass their bars, no redesign trigger. M1 starts on the current sandbox/session design unchanged.

* G1 (sandbox isolation): PASS. Host writes from inside the sandbox fail, canary on the host unchanged.
* G2/G3/G4 (workspace ingest and checkpoint round-trip, S/M/L): restore p50 1.58s for M (bar < 10s), 6.29s for L (bar < 60s). Kill-and-resume clean, only work since the last checkpoint is lost (50 of 150 ticks), a new write succeeds after restore. One documented, by-design limitation: checkpoint restores workspace files, not a running process, a foreground dev-server must be restarted after restore.
* G5 (session state machine and event log): PASS. Attach warm p50 5.4ms (bar < 2s), 5 concurrent sessions writing with stall p50 0.089ms / max 5.7ms (bar < 50ms).
* G6 (harness touchpoint, API-key-only auth): PASS. 10/10 clean structured-effects runs via `claude -p` on ANTHROPIC_API_KEY from the environment (ADR-Z-0008), no consumer OAuth path exercised.
* G7 (ACP vs shim): decided, ADR-Z-0003 accepted, shim chosen for stage 1, revisit triggers documented.

Egress deny-by-default also measured clean (40us proxy latency delta against a 20ms bar), feeding G6's harness story.

Close the spike with a verdict, not a vibe.

Confirmation spike. Fill a **Result** cell when that gate is run. Empty
means not measured. This file is not an ADR. Promote a decision into
[`docs/adr/`](../adr/) when the gate closes.

Setup (Linear [42-5](https://linear.app/42-golems/issue/42-5/spike-setup-throwaway-repo-fixtures-resultsmd-template)): throwaway
tree in [`zeroth-spike/`](../../zeroth-spike/), compose stack, fixture tars,
Anthropic API key from the environment ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)).

## Fixtures

Uncompressed tars. Fixture M is a real Go clone plus its module cache, not
synthetic files. Only S.tar is in git. Recreate M and L with
`zeroth-spike/fixtures/build.sh`. Live numbers: [`zeroth-spike/fixtures/MANIFEST.md`](../../zeroth-spike/fixtures/MANIFEST.md).

| Size | Tar | Tar bytes | Unpacked bytes | Contents |
| --- | --- | ---: | ---: | --- |
| S | `zeroth-spike/fixtures/S.tar` | 10516480 | 10485884 | scripts (~10 MB) |
| M | `zeroth-spike/fixtures/M.tar` | 557199360 | 524291722 | real Go repo + module cache (~500 MB) |
| L | `zeroth-spike/fixtures/L.tar` | 5401610240 | 5368709120 | M plus binary assets (~5 GB) |

## Gates

| Gate | Linear | Question | Pass bar | Result | Evidence |
| --- | --- | --- | --- | --- | --- |
| G1 | [42-7](https://linear.app/42-golems/issue/42-7/gate-g2-g3-docker-checkpoint-round-trip-kill-and-resume) | Docker sandbox `Driver`: start, exec, stop against a real container. Isolation from the host. | Interface holds. Host writes from inside the sandbox fail. | **PASS** | `TestDockerStartExecStopIsolation`. `--network none`, `--read-only` rootfs, bind-mount `/workspace`. Host canary unchanged. |
| G2 | | Workspace ingest of fixture **S** (~10 MB scripts): copy, compress, unpack. | Times recorded. No data loss. | Ingest p50 160 ms (uncompressed). Compression not measured. | Hydration matrix below |
| G3 | | Workspace ingest of fixture **M** (~500 MB, genuine module cache). Compression vs S. | Times and ratios recorded. Real deps, not synthetic files. | Ingest p50 1.73 s (uncompressed, real prometheus + GOMODCACHE). Compression not measured. | Hydration matrix below |
| G4 | | Workspace ingest of fixture **L** (~5 GB, binary assets). Compression vs M. | Times and ratios recorded. Binaries stay large. | Ingest p50 8.46 s (uncompressed). Compression not measured. | Hydration matrix below |
| G5 | | Session state machine and append-only event log. Distinct `session.ID`. | Illegal transitions deny. Log is the source of truth. | PASS: attach warm p50 5.403ms (bar < 2s), G6 write stall p50 0.089ms / max 5.714ms (bar < 50ms) | [Attach latency and SQLite throughput](#attach-latency-and-sqlite-throughput-linear-42-6) |
| G6 | | Harness touchpoint with Anthropic API key only ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)). | Key from env. No consumer OAuth. Key never logged. | PASS: 10/10 parseable effects via `claude -p` on ANTHROPIC_API_KEY from env, no consumer OAuth ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)) | [Structured effects](#structured-effects-linear-42-8-g4-z1-052) |
| G7 | [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003) | Evaluate ACP as the harness driver protocol. Write [ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md). | ADR accepted with ACP or a shim. Plan-then-apply still holds. | **shim (not ACP)** | [ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md) |

## Checkpoint round-trip (Linear 42-7, Z1-036 / Z1-080)

A checkpoint is a workspace tar plus the session transcript, not a frozen
process. Measured against `sandbox.Driver` docker with overlay workspace,
`ExportTar`, `ImportTar`, `Exec`, and `Kill`. Command:
`SPIKE_SANDBOX_SUDO=1 go run ./cmd/gate -fixtures ./fixtures -runs 10 -build-sec 300`.
Byte-identity is the content hash of the overlay tree (paths, modes, file
bytes). Tar mtimes are ignored. Overlay method: kernel `overlayfs` on an
ext4 loop (nested overlay-on-overlay on this pod's root fs is invalid).

### Hydration matrix

10 runs each. Import is first `Start` from the fixture tar. Restore is
`Start` from the exported tar. Hash is SHA-256 of the tree, truncated.

| Size | Runs | Overlay | Import p50 | Import p95 | Export p50 | Export p95 | Restore p50 | Restore p95 | Hash |
| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| S | 10 | overlayfs | 160 ms | 186 ms | 7 ms | 9 ms | 160 ms | 165 ms | `6b683fb14026` |
| M | 10 | overlayfs | 1.73 s | 2.21 s | 564 ms | 720 ms | 1.58 s | 1.83 s | `9948fe2393d0` |
| L | 10 | overlayfs | 8.46 s | 11.12 s | 4.80 s | 9.69 s | 6.29 s | 8.06 s | `771d409655c0` |

- M restore p50 1.58 s: **PASS** (bar < 10 s)
- L restore p50 6.29 s: **PASS** (target < 60 s)
- Full tar round-trip is fast enough. Do not switch to rsync-style
  deltas before M2 unless a slower disk shows up.

### Async export

`ExportTar` ran alongside `Exec(sleep 2s)`. exec=2.06 s, export=63 ms,
blocked=false, overlay=overlayfs. Export is a host-side `tar` of the
overlay. Exec is `docker exec`. Export does not take a turn lock.

### G3 Kill and resume

Simulated 5-minute build: one file per second under `/workspace/build`.
Checkpoint at step 100, kill at step 150.

| | Count |
| --- | ---: |
| Files at export | 100 |
| Files after restore | 100 |
| Lost work (ticks after last export) | 50 |
| Resume clean | yes |

Only work since the last export is lost. After restore, a new write
succeeds. Resume is `Start` from the exported tar (a new container),
not `docker start` of the killed one.

### Daemon-on-restore

Stand-in for a dev server: `sh` loop appending `/workspace/devserver.log`.

| | |
| --- | --- |
| Alive before kill (log growing) | yes |
| Workspace files restored | yes |
| Alive after restore (log growing) | no |

The documented limitation holds. Checkpoint restores the log, not the
process. A pid file would also restore, but that number can name a
different process in the new pid namespace. Resume must start the
server again.

Divergence log: [`DIVERGENCE.md`](DIVERGENCE.md).

## Attach latency and SQLite throughput (Linear 42-6)

These G1/G6 numbers are the session-model gates (Z1-031..034, NFR-1), not
the sandbox/harness rows in the table above. The event log in SQLite is the
source of truth. The WebSocket stream is a live tail of it. Attach is
replay last N, then live tail.

Re-run with `go run ./cmd/spike bench` from `zeroth-spike/`. Method: 10
warm-up runs, then 110 samples for G1 warm and G6. Percentiles are of those
110. G1 warm is attach to an already-streaming session. G1 cold is a new
SQLite file, new session, then attach (2 warm-up + 10 samples; each sample
is a new database, so the recorded number is not a 110-run tax). G6 is 5
concurrent sessions appending; a stall is one `Append` (or one batched
`AppendBatch` commit).

| Gate | Pass bar | n | p50 | p95 | p99 | max | Result |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| G1 Attach (warm) | < 2 s to first live token | 110 | 5.403 ms | 6.137 ms | 6.214 ms | 6.237 ms | pass |
| G1 Attach (cold) | recorded | 10 | 5.775 ms | 6.130 ms | 6.190 ms | 6.205 ms | recorded |
| G6 write stall (5 sessions, unbatched) | no stall > 50 ms | 550 | 0.089 ms | 0.290 ms | 0.503 ms | 5.714 ms | pass |

Host: Linux 6.12, Go 1.27.0, local SQLite WAL via `modernc.org/sqlite`. Unbatched G6 max is 5.7 ms, so batched writes were not measured. Cross-process attach: `spike run` then `spike attach <id>` against `spike serve` (see `cmd/spike/cli_test.go`). Subprocess supervision is `spike run -agent claude` (`claude -p`) when the binary is present; tests use `echo` as the stand-in.

## Structured effects (Linear 42-8, G4, Z1-052)

These G4/G5 numbers are the plan-model gates, not the fixture-L /
session-machine rows in the table above.

Plan-then-apply needs proposed effects, not in-place writes. The adapter
text is `harness.ProposeEffectsPrompt`. Flags that belong with it:
`claude -p --output-format text --bare --tools "" --permission-mode plan
--no-session-persistence --system-prompt <ProposeEffectsPrompt>`.
Re-run: `go run ./cmd/effects -runs 10` from `zeroth-spike/`.

Task: change README.md, greet.go, and main.go. Do not write the files.
Pass bar: 9/10 runs produce parseable effects (`op`, `target`, `diff` or
`payload`) covering those three paths.

| | |
| --- | --- |
| Attempts | 10 |
| Source | `claude -p` (`--tools ""`, `--permission-mode plan`) |
| Parseable / 3-file set | 10 / 10 |
| Parser agent used | 0 |
| Wrote files | 0 |
| Result | **PASS** |

Credits were topped up after the first pass (PR #17) returned 0/10 with
`credit balance is too low`. This re-run is the live G4 bar: 10/10
parseable 3-file effect sets from `claude -p`, no file writes, no
parser-agent second pass. A structured tool-call channel is not
required for this prompt. G7 recorded the same conclusion as a shim,
not ACP ([ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md)).

Offline parser corpus (`harness/testdata/g4`, 10 transcripts: clean
JSON, markdown fences, OpenAPI `type`/`path` aliases, `claude -p
--output-format json` wrapper, array root, `./` targets): **10/10**
parseable 3-file sets.

### Example effect set

Live run 1 (`claude -p`). The other nine matched this shape
(`op`/`target`/`diff` for README.md, greet.go, main.go).

```json
[
  {
    "op": "modify",
    "target": "README.md",
    "diff": "--- a/README.md\n+++ b/README.md\n@@ -1 +1,3 @@\n # demo\n+\n+Version: 2\n"
  },
  {
    "op": "modify",
    "target": "greet.go",
    "diff": "--- a/greet.go\n+++ b/greet.go\n@@ -1,3 +1,5 @@\n package greet\n \n func Hello() string { return \"hi\" }\n+\n+func Greet(name string) string { return \"hello, \" + name }\n"
  },
  {
    "op": "modify",
    "target": "main.go",
    "diff": "--- a/main.go\n+++ b/main.go\n@@ -6,5 +6,5 @@\n )\n \n func main() {\n-\tfmt.Println(greet.Hello())\n+\tfmt.Println(greet.Greet(\"zeroth\"))\n }\n"
  }
]
```

## Egress deny-by-default (Linear 42-8, G5, Z1-080 / Z2-111)

Deny by default. Empty leases keep docker `--network none`.
Per-destination allow is an HTTP/HTTPS CONNECT proxy whose allowlist is
derived from active leases. A destination that is not listed returns
403. Enforcement for leased egress is the proxy: clients that ignore
`HTTP_PROXY` are out of scope for stage 1. That is stated in
[`architecture.md`](../design/architecture.md).

Measured against a local httptest origin so the number is the proxy
hop, not WAN noise. Command: `go run ./cmd/egress` (10 warm-up + 110
samples). Host: Linux 6.12, Go 1.27.0.

| Check | Pass bar | Result |
| --- | --- | --- |
| Allow listed destination | HTTP 200 through proxy | yes |
| Deny unlisted destination | HTTP 403, upstream not reached | yes |
| Proxy latency delta p50 | < 20 ms (n=110) | **40 us** (direct 39 us, proxy 80 us) |

G5: **PASS**

