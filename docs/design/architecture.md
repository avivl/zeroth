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
- **session** — state machine plus event log for one human-supervised run.
- **plan** — draft, cross-exam, approve, apply. Consequential mutation happens only on apply.
- **lease** — runtime mint/renew/expire for policy leases.
- **signer / audit** — attributable, append-only trail.
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

Stage 1 is local and single-player, so the store is SQLite and the daemon binds locally. There is no remote control plane.

## Surfaces

- `cmd/zerothd` — long-running process.
- `cmd/zeroth` — CLI and headless entry point (same kernel, no GUI required).
- `pkg/api` — OpenAPI spec; Go stubs and the TypeScript client are generated from it.
- `web/` — Vite + React. Intended to use [Beautiful UI](https://www.beautifului.dev/) primitives (MIT).

## Trust boundary

The harness is untrusted relative to the kernel. The sandbox is the isolation boundary for agent work. Policy decides what a session may do; the harness does not. Audit records are signed so a successful agent cannot quietly rewrite history.

## Out of scope here

Deployment topology, multi-tenant isolation, and multiplayer session sharing. Those belong to stage 2 and are not designed in this document.
