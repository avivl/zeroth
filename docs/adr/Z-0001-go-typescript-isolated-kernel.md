# ADR-Z-0001: Go and TypeScript, isolated policy kernel with typed IDs

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

Zeroth is a local control plane: a daemon, a CLI, and a web UI, with a policy kernel that must outrank every harness. The language split and the kernel's type rules are easy to defer until "we have behavior," and expensive to unwind once IDs, grants, and leases are strings in every package.

## Decision drivers

- The kernel must be reviewable, deny-by-default, and free of I/O.
- Distinct ID kinds must not be interchangeable at compile time.
- Stage 1 ships one local UI; that UI is not the kernel.
- One operator, one machine: pick languages the skeleton already uses, not a rewrite.

## Considered options

- Go for the daemon and kernel, TypeScript for `web/`.
- TypeScript everywhere (daemon in Node/Bun, shared types with the UI).
- Rust for the kernel, Go or TypeScript at the edges.
- Stringly-typed IDs (`string` / `int64` aliases) across packages.

## Decision

The daemon, CLI, and kernel are Go. The local UI is TypeScript (Vite + React). Generated API clients come from `pkg/api/openapi.yaml`; they are not hand-written.

`internal/policy` is isolated: no disk, network, environment, or store access. Callers pass facts in; policy returns a decision. Agents do not modify that package.

IDs are distinct named types (`ScopeID`, `GrantID`, `LeaseID`, `SessionID`, and the rest). They are not `string`, not `int64`, and not aliases of each other.

## Consequences

- Two languages and a generated contract at the UI boundary. That is cheaper than sharing a runtime with the kernel.
- Compile-time ID mix-ups fail in Go instead of in audit reconstruction.
- A second UI toolkit or a WASM kernel would be a new ADR, not a silent import.

## Revisit triggers

- The policy kernel must run in a context Go cannot occupy (for example a browser-only operator surface with no daemon).
- Named ID types are replaced by a generated scheme that still forbids mixing kinds.
- Stage 2 needs a shared kernel implementation across more than one language and the generated OpenAPI types are not enough.
