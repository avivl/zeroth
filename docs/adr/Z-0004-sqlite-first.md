# ADR-Z-0004: SQLite first, Postgres at stage 2, one store interface

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

The daemon needs durable state for sessions, plans, leases, and the audit trail. Stage 1 is one operator on one machine. A hosted database would smuggle a deployment story into a local control plane.

## Decision drivers

- Local, single-player: no server to operate, no network round-trip for the store.
- One `store.Store` port so stage 2 can add a backend without rewriting the kernel.
- Deny-by-default policy and append-only audit must not depend on a cloud database.

## Considered options

- SQLite now, Postgres (or equivalent) when stage 2 needs a shared control plane.
- Postgres from day one, locally via Docker.
- An embedded key-value store instead of SQL.
- Files on disk with no store port.

## Decision

Stage 1 persistence is SQLite (`internal/store/sqlite`). Callers depend on `store.Store`, never on the engine by name. Postgres is a stage-2 concern. There is no second store implementation in stage 1.

## Consequences

- CI and the operator laptop run the same engine.
- SQL that is SQLite-only will hurt at stage 2; the port and conformance tests are the place to keep the surface honest.
- Multi-tenant schema, replicas, and hosted Postgres do not belong in stage 1.

## Revisit triggers

- Stage 2 work starts and a second `Store` implementation is in scope (Postgres is the expected one).
- SQLite cannot hold the append-only audit log or concurrent local sessions for a single operator.
- The store must live on a filesystem SQLite cannot use safely, and the alternative is still local (not a cloud).
