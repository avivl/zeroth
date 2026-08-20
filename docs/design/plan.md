# Plan

Status: draft  
Companion docs: [PRD](../prd/zeroth.md), [architecture](architecture.md)

This is the project plan, not a sprint board. Stages are sequential. Stage 1 must be honest about what it is not.

## 1. Purpose

Ship a local control plane where agents work at machine speed and humans keep control of consequential actions.

## 2. Stages

### Stage 1 — local, single-player

- One operator, one machine.
- `zerothd` + `zeroth` + local web UI.
- SQLite, Docker sandbox, Claude Code harness, Linear tracker.
- Policy kernel, plan lifecycle, signed audit trail.
- **No deployment story.** If it does not run on the operator’s laptop, it does not ship in stage 1.

### Stage 2 — multiplayer

- Shared sessions, more than one human, a control plane that is not “this laptop.”
- Not designed here. Do not smuggle stage-2 requirements into stage-1 packages.

## 3. Skeleton first

The first deliverable is the repository shape: module path, package tree, MIT LICENSE, and an honest README that links this plan, the PRD, and the design doc. Empty packages with `doc.go` files are acceptable. `go build ./...` must succeed.

## 4. Kernel, then ports, then UI

Implement in that order. A UI that can apply without a kernel is a defect. A harness that can mutate the world without a plan is a defect.

## 5. Evals

`evals/` is where offline evaluations of plan quality, policy decisions, and harness behavior will live. They are not CI unit tests.

## 6. Project decisions

These are locked unless an ADR supersedes them.

1. **Name: Zeroth.** The governing constraint is numbered zero because it had to sit above rules that already existed.
2. **Human control is the kernel**, not a feature flag. Policy outranks the harness.
3. **MIT license.** See [ADR-Z-0002](../adr/Z-0002-mit-license.md).
4. **Public from day one.** A private “open source” project is a contradiction that gets harder to unwind later. The GitHub repository is public; we do not wait for a polish milestone to open it.
5. **Stage 1 is local and single-player.** No cloud deployment, no multiplayer.
6. **One implementation per port in stage 1.** Docker, Claude Code, Linear, SQLite. The interfaces exist so a second implementation can be added without rewriting the kernel.
7. **Plan-then-apply is mandatory** for consequential actions. Autonomy tiers change how much a session may do, not whether a plan exists.
8. **Trunk-based development.** Short-lived branches, squash merge onto `main`, `main` always green. A PR is required. CI is required. CODEOWNERS require a human on `internal/policy/`, `internal/plan/`, and `pkg/api/`, including agent-authored PRs. See `.github/workflows/ci.yml` and `.github/CODEOWNERS`.
9. **The SHA is the version.** Identify builds by git commit SHA. Do not add semver tags or a product version constant inside the repo.

## 7. Non-goals until stage 2

Hosting, teams, SSO, billing, a marketplace of harnesses, and any design that assumes more than one operator.
