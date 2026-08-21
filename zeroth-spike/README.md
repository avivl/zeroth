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
| `session` | Distinct `ID` type, state machine, append-only event log |
| `harness` | One touchpoint: `ANTHROPIC_API_KEY` present or not |
| `fixtures` | S/M/L workspace tars |
| `cmd/spike` | The compose process |
