# Zeroth

Agents work at machine speed. Humans keep control of consequential actions.

Zeroth is a control plane for AI agents. Autonomy is earned tier by tier, plans are proposed before they are applied, and every action is signed and auditable. The name is from Asimov: the Three Laws were incomplete, so a Zeroth Law was added above them — not another rule in the list, but the constraint that outranks the rest. Everything else in this repository is a First Law. **Human control is the Zeroth.**

## What this is

Stage 1 is **local and single-player**. You run a daemon (`zerothd`) and a CLI (`zeroth`) on your own machine. The kernel is policy (scopes, grants, leases). The workflow is plan-then-apply: draft, cross-exam, approve, apply. Sandbox, harness, tracker, and store are ports with one implementation each (Docker, Claude Code, Linear, SQLite). The UI is Vite + React. The repo is public and MIT licensed from day one.

## What this is not yet

- **Not a hosted product.** There is no deployment story in stage 1. No cloud, no shared cluster, no “deploy Zeroth for the team.”
- **Not multiplayer.** Sharing sessions, orgs, and remote control planes is stage 2.
- **Not done.** This repository is a skeleton. Packages compile; most of them are empty on purpose. The shape is the deliverable.

If you arrived looking for a SaaS agent runtime you can roll out to a team, you are early. The three documents below are the source of truth for what lands when.

## Docs

- [AGENTS.md](AGENTS.md): harness-facing memory (canonical for coding agents)
- [Product requirements](docs/prd/zeroth.md)
- [Architecture](docs/design/architecture.md)
- [Plan](docs/design/plan.md) — §6 is the decision log (decision 4: public from day one)

Also: [ADRs](docs/adr/) ([ADR-Z-0002](docs/adr/Z-0002-mit-license.md) chooses MIT), [spikes](docs/spike/), [evals](evals/).

## Layout

```
zeroth/
├── cmd/zerothd/     daemon
├── cmd/zeroth/      CLI + headless entry point
├── internal/        kernel + ports (not importable from outside the module)
├── pkg/api/         OpenAPI spec; generated stubs and TS client will live here
├── web/             Vite + React
├── docs/            adr, prd, design, spike
└── evals/           offline evaluations
```

## Develop

```bash
go build ./...
go test -race ./...
```

If you have [Task](https://taskfile.dev):

```bash
task build
task test
task ci
```

Requires Go 1.27 (see `go.mod`). The UI under `web/` is a Vite + React scaffold and is not required for `go build`.

## License

[MIT](LICENSE) © 2026 Aviv Laufer
