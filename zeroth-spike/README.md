
![Zeroth](zeroth-app-icon.svg )
# zeroth-spike

Throwaway BA-6 confirmation spike. Not a product. No UI, no auth, no Linear.

The sandbox and session interfaces in this directory are meant to survive
into M1 and M2 even if these implementations do not. Do not promote this
tree into `internal/`. Findings go in [RESULTS.md](../docs/spike/RESULTS.md)
or an ADR.

## Stack

```bash
cp .env.example .env   # set ANTHROPIC_API_KEY (ADR-Z-0008, API key only)
docker compose up --build
curl -fsS http://127.0.0.1:8421/health
curl -fsS http://127.0.0.1:8421/auth      # {"api_key_configured": true|false, "method":"api_key"}
curl -fsS http://127.0.0.1:8421/fixtures  # S/M/L tar sizes
```

The process listens on loopback port 8421. It never logs or returns the API
key. Consumer OAuth is out of scope.

## Session log (Linear 42-6)

SQLite is the source of truth. WebSocket is a live tail of that log.
Attach is replay last N, a `caught_up` frame, then live tokens.

```bash
go run ./cmd/spike serve -db /tmp/spike.db
go run ./cmd/spike run                         # prints session id; fake agent
go run ./cmd/spike attach <id>                 # replay then live tail
go run ./cmd/spike bg <id>                     # demote; agent keeps running
go run ./cmd/spike run -agent claude           # one real `claude -p` if installed
go run ./cmd/spike bench                       # G1 attach + G6 write stall
```

`spike run` is headless: it starts a session on the server and exits.
`spike attach` talks to that server from another process.

## Tests

```bash
go test -race ./...
```

CI runs that on push. Docker tests skip if the daemon is not available.
Fixture M and L are not built in CI (hundreds of MB and 5 GB). Recreate
them with `./fixtures/build.sh`.

## Checkpoint gates (Linear 42-7)

The docker driver uses an overlay workspace bind-mounted at `/workspace`,
plus `ExportTar`, `ImportTar`, `Exec`, and `Kill`. A checkpoint is that
tar plus the session transcript, not a frozen process.

```bash
# S is in git. Build M and L first for the full matrix.
./fixtures/build.sh
go run ./cmd/gate -fixtures ./fixtures -runs 10 -build-sec 300
```

`cmd/gate` prints the hydration matrix, kill-and-resume lost-work count,
and daemon-on-restore notes for [RESULTS.md](../docs/spike/RESULTS.md).
Optional env: `SPIKE_SANDBOX_ROOT` (workspace parent), `SPIKE_SANDBOX_SUDO=1`
(kernel overlayfs via `sudo -n mount`), `SPIKE_SANDBOX_IMAGE`.

## Structured effects and egress (Linear 42-8)

G4: a system prompt that forbids writes and asks for JSON effects
(`op`, `target`, `diff` or `payload`). Ten runs of a 3-file change.
G5: deny-by-default HTTP/HTTPS CONNECT proxy whose allowlist is derived
from active leases. Docker stays `--network none` when the list is empty.

```bash
go run ./cmd/effects -runs 10
go run ./cmd/egress
```

The prompt is `harness.ProposeEffectsPrompt`. It is the adapter, not a
one-off. Re-run with `ANTHROPIC_API_KEY` set. `claude -p` is preferred
when the binary is on PATH; otherwise the Messages API is the stand-in.

## Layout

| Path | Role |
| --- | --- |
| `sandbox` | `Driver` port: `Name`, `Start` / `Exec` / `ExportTar` / `ImportTar` / `Kill` / `Stop`. Docker uses an overlay workspace. Empty egress leases keep `--network none`. |
| `session` | Distinct `ID` type, state machine, append-only event log |
| `eventlog` | Append-only SQLite WAL log; one row per session event |
| `supervisor` | Goroutine plus fake ticker agent, or a subprocess (`claude -p` / cmd) |
| `bench` | G1 attach latency and G6 SQLite write-stall measurement |
| `harness` | API-key touchpoint, G4 effects prompt/parser, Claude Code / Messages runner |
| `fixtures` | S/M/L workspace tars |
| `cmd/spike` | `serve` (default), `run`, `attach`, `bg`, `bench` |
| `cmd/gate` | G2/G3 checkpoint measurements |
| `cmd/effects` | G4 structured-effects measurement |
| `cmd/egress` | G5 allow/deny and proxy latency measurement |
