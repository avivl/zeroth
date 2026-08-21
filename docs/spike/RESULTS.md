# Spike results (BA-6)

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
| G5 | | Session state machine and append-only event log. Distinct `session.ID`. | Illegal transitions deny. Log is the source of truth. | | |
| G6 | | Harness touchpoint with Anthropic API key only ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)). | Key from env. No consumer OAuth. Key never logged. | | |
| G7 | [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003) | Evaluate ACP as the harness driver protocol. Write [ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md). | ADR accepted with ACP or a shim. Plan-then-apply still holds. | | |

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
