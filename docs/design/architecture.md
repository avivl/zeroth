# Architecture

Status: draft (stage 1)  
Companion docs: [PRD](../prd/zeroth.md), [plan](plan.md)

Zeroth is a local control plane. The daemon holds the kernel. Everything that talks to the outside world does so through a port.

```
                 ┌──────────────┐     ┌──────────────┐
                 │  web (Vite)  │     │ cmd/zeroth   │
                 └──────┬───────┘     └──────┬───────┘
                        │  pkg/api           │
                        └─────────┬──────────┘
                                  ▼
                           cmd/zerothd
                                  │
        ┌──────────── policy · session · plan · lease ────────────┐
        │  memory    audit ← signer    secretscan                 │
        └────────────┬──────────┬──────────┬──────────┬───────────┘
                     ▼          ▼          ▼          ▼
                 sandbox     harness    tracker     store
                  docker    claudecode   linear     sqlite
```

## Kernel

- **policy** — scopes, grants, leases. Outranks every other package.
- **session** — state machine plus event log for one human-supervised run. Lifecycle is `pending → running → awaiting-approval → applying → done | failed`. Attachment (`attached` / `background`) is orthogonal. The event log is the source of truth; status is a replay of it. Attach is replay-then-live-tail of that log, so promotion and demotion only add or remove listeners. Illegal transitions are errors. A supervisor goroutine per live session serializes mutations. A demoted session carries a completion contract (finish, comment on the issue, ping only on blockers). Persistence of the log is the store port; this package talks to a `Log`.
- **plan** — draft, cross-exam, approve, apply. Consequential mutation happens only on apply.
- **lease** — runtime mint/renew/expire for policy leases.
- **signer / audit**: attributable, append-only trail. Signatures are secp256k1 Schnorr, Nostr-compatible ([ADR-Z-0007](../adr/Z-0007-secp256k1-schnorr.md)).
- **secretscan** — a gate on apply, not a linter the operator can skip.
- **memory** — session and agent memory, store-backed in stage 1.

## Ports

Each port is an interface in `internal/<name>` with one implementation in a subpackage and a `conformance_test.go` that the implementation must pass. The daemon depends on the interface.

| Port | Interface | Stage-1 implementation |
| --- | --- | --- |
| sandbox | `Driver` | `docker` |
| harness | `Driver` | `claudecode` |
| tracker | `Provider` | `linear` |
| store | `Store` | `sqlite` |

The store port covers sessions, events, plans, approvals, memory entries and proposals, audit records, leases, the checkpoint index, and agents. SQLite is one file in WAL mode. The path is configurable (`zerothd --db-path` / `ZEROTH_DB_PATH`). Schema changes go through Up and Down migrations; a migration without a Down is not done. The event log is append-only and is the source of truth for a session. `internal/store/conformance_test.go` is the contract a later Postgres driver must pass unchanged except for adding its table row (ADR-Z-0004, NFR-4).

The sandbox port is `Driver`: Spawn, Exec, ExportTar, ImportTar, AllowEgress, Kill, and Stop. Spawn starts with egress denied. A checkpoint is a workspace tar (content hash of paths, modes, and bytes, not tar mtimes), not a frozen process. Kill drops in-flight PIDs; the overlay remains until Stop. `internal/sandbox/conformance_test.go` is the contract a later backend must pass unchanged except for adding its table row (Z1-080, NFR-4). Docker is the stage-1 implementation ([ADR-Z-0005](../adr/Z-0005-docker-sandbox.md)).

Stage 1 is local and single-player, so the store is SQLite and the daemon binds locally (`127.0.0.1:8420` by default, overridable with `ZEROTH_ADDR` or `zerothd --addr`). There is no remote control plane.

## Cross-cutting

- **Logging:** Zap via `internal/logging`. No package-level global. `internal/policy` does not log; callers pass facts in and may log the decision.
- **CLI and config:** Cobra command trees for `cmd/zeroth` and `cmd/zerothd`. Viper loads `zerothd` startup config (flags > `ZEROTH_*` env > yaml file > defaults).
- **Resilience:** Failsafe-go via `internal/resilience`. See [resilience.md](resilience.md). Drivers that talk to docker, a subprocess, or a tracker API reuse that pattern.

## Surfaces

- `cmd/zerothd` — long-running process (Cobra root, Viper config, Zap logs).
- `cmd/zeroth` — CLI and headless entry point (Cobra: `version`, stubs for `run` / `attach` / `bg`).
- `pkg/api` — OpenAPI spec (`openapi.yaml`); Go stubs and the TypeScript client are generated from it. Stage 1 surface: runs (steer, background, foreground, stop, events WebSocket, on-demand checkpoints), plans (list, approve, request-changes, branch, apply), agents (including read-only leases), approvals, memory (including proposals), audit verify, and checkpoint restore (forks a new run). Apply is `POST /plans/{id}/apply` after approve. Cross-exam is automatic and appears on the plan resource, not as an operator endpoint. A flow that cannot be expressed here does not ship on the web UI or the CLI.
- `web/` — Vite + React. Intended to use [Beautiful UI](https://www.beautifului.dev/) primitives (MIT). The generated TypeScript client is HTTP; live run events use a thin WebSocket wrapper around the generated `RunEvent` type.

## Trust boundary

The harness is untrusted relative to the kernel. The sandbox is the isolation boundary for agent work. Policy decides what a session may do; the harness does not. Audit records are signed so a successful agent cannot quietly rewrite history.

Sandbox egress is deny by default. With no lease, the docker driver uses `--network none`. Per-destination allow is an HTTP/HTTPS CONNECT proxy whose allowlist is derived from active leases. A destination that is not listed is denied. The proxy is the enforcement point for leased egress: clients that ignore `HTTP_PROXY` are out of scope for stage 1. Closing that gap is a later network-namespace filter, not a second allowlist. The BA-6 spike measured this in [RESULTS.md](../spike/RESULTS.md) (Linear 42-8).

## Out of scope here

Deployment topology, multi-tenant isolation, and multiplayer session sharing. Those belong to stage 2 and are not designed in this document.
