![Zeroth](zeroth-app-icon.svg )
# Zeroth

![Coverage](./docs/coverage.svg)

Agents work at machine speed. Humans keep control of consequential actions.

Zeroth is a control plane for AI agents. Autonomy is earned tier by tier, plans are proposed before they are applied, and every action is signed and auditable. The name is from Asimov: the Three Laws were incomplete, so a Zeroth Law was added above them — not another rule in the list, but the constraint that outranks the rest. Everything else in this repository is a First Law. **Human control is the Zeroth.**

## Why "Zeroth"?

In Asimov's robot stories, the Three Laws were eventually found to be incomplete. A Zeroth Law was added above them, not as another rule in the list but as the one that outranks the rest and constrains what the others are permitted to do. Zeroth takes its name from that idea. Agents here are fast, autonomous, and increasingly capable, and none of that is allowed to override the governing constraint: a human stays in control of consequential actions. Autonomy is earned tier by tier, plans are proposed before they are applied, and every action is signed and auditable. Everything else in this project is a First Law. Human control is the Zeroth.

## What this is

Stage 1 is **local and single-player**. You run a daemon (`zerothd`) and a CLI (`zeroth`) on your own machine. The kernel is policy (scopes, grants, leases). The workflow is plan-then-apply: draft, cross-exam, approve, apply. Sandbox, harness, tracker, and store are ports with one implementation each (Docker, Claude Code, Linear, SQLite). The UI is Vite + React. The repo is public and MIT licensed from day one.

The Docker sandbox holds a **copy** of your git checkout (the overlay). The Claude Code harness is a **host subprocess** of `zerothd` whose working directory is that overlay, not a `docker exec` inside the container ([ADR-Z-0010](docs/adr/Z-0010-harness-host-subprocess.md)). Relative writes land in the copy. The process can still see host paths outside it. Plan-then-apply is what stops the agent from mutating the world. In-sandbox harness execution is not a stage-1 property.

## What this is not yet

- **Not a hosted product.** There is no deployment story in stage 1. No cloud, no shared cluster, no “deploy Zeroth for the team.”
- **Not multiplayer.** Sharing sessions, orgs, and remote control planes is stage 2.
- **Not done.** This repository is a skeleton. Packages compile; most of them are empty on purpose. The shape is the deliverable.

If you arrived looking for a SaaS agent runtime you can roll out to a team, you are early. The three documents below are the source of truth for what lands when.

## Docs

- [AGENTS.md](AGENTS.md): harness-facing memory (canonical for coding agents)
- [Product requirements](docs/prd/zeroth.md)
- [Architecture](docs/design/architecture.md)
- [Plan](docs/design/plan.md): §6 is the project decision log (decision 4: public from day one)
- [Decision register](docs/prd/zeroth.md#decision-register): index of [ADRs](docs/adr/)

Also: [ADR-Z-0002](docs/adr/Z-0002-mit-license.md) chooses MIT, [spikes](docs/spike/), [evals](evals/).

## Layout

```
zeroth/
├── cmd/zerothd/     daemon
├── cmd/zeroth/      CLI + headless entry point
├── internal/        kernel + ports (not importable from outside the module)
├── pkg/api/         OpenAPI spec; generated stubs and TS client under gen/
├── web/             Vite + React (pnpm workspace)
├── docs/            adr, prd, design, spike
├── zeroth-spike/    BA-6 confirmation spike (throwaway)
└── evals/           offline evaluations
```

## Develop


### Connecting Linear (assign-to-Zeroth)

Zeroth can watch a Linear workspace and start a run when an issue is
assigned to its agent identity, or delegated to it via Linear's native
agent-delegation (the same "self-assign and delegate" flow used with
Cursor in this repo's own backlog).

Configure it with `--linear-api-key` / `ZEROTH_LINEAR_API_KEY`,
`--linear-agent-user` / `ZEROTH_LINEAR_AGENT_USER`, and
`--linear-auth-style` / `ZEROTH_LINEAR_AUTH_STYLE` (`personal` or `oauth`,
default `personal`).

See[`docs/linear-setup.md`](docs/linear-setup.md) for how to
set up a dedicated "Zeroth" identity in Linear, the full configuration
reference, and a step-by-step walkthrough from assigning an issue to a
verified, merged PR.



You need Go 1.27, Node, Docker, and [Task](https://taskfile.dev). A newcomer needs four commands:

```bash
task up    # zerothd with SQLite and the docker sandbox driver
task web   # Vite dev server for web/
task test  # go test -race ./...
task lint  # go vet, staticcheck, and the web lint
```

`task --list` shows the rest (`conformance`, `generate`, `generate:check`, `secretscan`, and the local `ci` stand-in).

The UI under `web/` is a pnpm workspace package. `task web` and `task lint` install JS deps via Corepack. Requires Go 1.27 (see `go.mod`). `zerothd` binds `127.0.0.1:8420` by default (`ZEROTH_ADDR` or `--addr`). Config is Cobra flags over Viper (env, optional `zeroth.yaml` / `--config`, then defaults). Cross-exam uses an independent OpenAI reviewer (`--reviewer-model`, `--reviewer-api-key` / `OPENAI_API_KEY`). Logs are Zap (`--log-level`, `--log-encoding`). `zeroth version` prints the build SHA.

PRs run GitHub Actions (`.github/workflows/ci.yml`): race tests with a coverage profile, octocov (PR comment plus a fail if coverage drops versus main), conformance, `go vet`, staticcheck, a secret scan over the diff, the `web/` build and tests, and a check that `pkg/api/gen` matches `task generate`. `web/`-only PRs skip Go. `internal/`-only PRs skip web. Changes under `pkg/api/` run everything, because that tree is the contract. Merge to main commits `docs/coverage.svg`. The commit SHA is the version. There is no semver in this repository.

`zeroth run <task>` starts a headless session. `zeroth attach <run-id>` replays recent events and live-tails (type to steer; Ctrl-C detaches). `zeroth bg <run-id>` demotes a run. `zeroth runs` lists. `zeroth retract <run-id> --reason "..."` closes any pull request that run opened, comments the reason on the Linear issue, and leaves the issue ready for a fresh assignment. `zeroth verify <run-id>` checks the signed audit chain against the SQLite file with no daemon (`--db-path` / `ZEROTH_DB_PATH`). Live events are `GET /runs/{id}/events` over WebSocket.

## License

[MIT](LICENSE) © 2026 Aviv Laufer

## License

MIT. See [LICENSE](./LICENSE) for the full text.
