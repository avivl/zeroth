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

CI runs that on push. Fixture M and L are not built in CI (hundreds of MB
and 5 GB). Recreate them with `./fixtures/build.sh`.

## Layout

| Path | Role |
| --- | --- |
| `sandbox` | `Driver` port: `Name`, `Start` / `Exec` / `Stop`. Memory unpacks tars. Docker is named and stubbed. |
| `session` | Distinct `ID` type and state machine |
| `eventlog` | Append-only SQLite WAL log; one row per session event |
| `supervisor` | Goroutine plus fake ticker agent, or a subprocess (`claude -p` / cmd) |
| `bench` | G1 attach latency and G6 SQLite write-stall measurement |
| `harness` | One touchpoint: `ANTHROPIC_API_KEY` present or not |
| `fixtures` | S/M/L workspace tars |
| `cmd/spike` | `serve` (default), `run`, `attach`, `bg`, `bench` |
