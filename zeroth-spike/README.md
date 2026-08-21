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

## Layout

| Path | Role |
| --- | --- |
| `sandbox` | `Driver` port: `Name`, `Start` / `Exec` / `ExportTar` / `ImportTar` / `Kill` / `Stop`. Docker uses an overlay workspace. Memory unpacks tars without isolation. |
| `session` | Distinct `ID` type, state machine, append-only event log |
| `harness` | One touchpoint: `ANTHROPIC_API_KEY` present or not |
| `fixtures` | S/M/L workspace tars |
| `cmd/spike` | The compose process |
| `cmd/gate` | G2/G3 checkpoint measurements |


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
