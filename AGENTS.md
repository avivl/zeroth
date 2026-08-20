# Agent guide

Canonical instructions for coding agents working in this repository.

## What this is

Zeroth is a **local, single-player** control plane for AI agents. Human control of consequential actions outranks everything else. Stage 1 has **no deployment story**. Multiplayer is stage 2 — do not smuggle it into stage-1 packages.

Read [README.md](README.md), [docs/prd/zeroth.md](docs/prd/zeroth.md), [docs/design/architecture.md](docs/design/architecture.md), and [docs/design/plan.md](docs/design/plan.md) before changing shape.

## Layout

| Path | Role |
| --- | --- |
| `cmd/zerothd` | Daemon |
| `cmd/zeroth` | CLI + headless entry point |
| `internal/policy` | Kernel: scopes, grants, leases |
| `internal/session` | State machine + event log |
| `internal/plan` | Draft, cross-exam, approve, apply |
| `internal/sandbox` | `Driver` + `docker/` + `conformance_test.go` |
| `internal/harness` | `Driver` + `claudecode/` + `conformance_test.go` |
| `internal/tracker` | `Provider` + `linear/` + `conformance_test.go` |
| `internal/store` | `Store` + `sqlite/` + `conformance_test.go` |
| `internal/memory` `audit` `signer` `lease` `secretscan` | Kernel-adjacent packages |
| `pkg/api` | OpenAPI spec; generate stubs and the TS client from it |
| `web/` | Vite + React |
| `docs/adr` `docs/prd` `docs/design` `docs/spike` | Specs |
| `evals/` | Offline evaluations, not unit tests |

`internal/` is not importable from other modules. That is deliberate.

## Commands

```bash
go build ./...
go test ./...
go vet ./...
task ci    # if Task is installed
```

Go 1.22. Do not add dependencies unless the package that needs them is being implemented.

## Rules

- Policy outranks the harness. Never add a path that applies a consequential action without the plan lifecycle.
- Ports are interfaces. Implementations live in subpackages and must pass `conformance_test.go`.
- Generate `pkg/api` clients from `openapi.yaml`. Do not hand-write them.
- Stage 1 is SQLite, local daemon, one operator. No cloud resources, no multi-tenant schema.
- Empty `doc.go` packages are acceptable until that package has behavior. Do not invent APIs to look busy.
- ADRs are `docs/adr/Z-NNNN-slug.md`. License is MIT (ADR-Z-0002). Repo is public (ADR-Z-0001).
