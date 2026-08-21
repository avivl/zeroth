# ADR-Z-0005: Docker sandbox driver is the reference implementation

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

Agent work must not run unbounded on the operator's host. The sandbox is the isolation boundary; the kernel decides what a session may do, and the sandbox is where that work actually executes.

## Decision drivers

- Stage 1 needs one real isolation boundary, not a catalog of runtimes.
- Docker is the isolation tool an operator already has on a development machine.
- The daemon depends on `sandbox.Driver`, so a later runtime can replace Docker without touching policy.

## Considered options

- Docker as the stage-1 `Driver` (reference implementation).
- A process-level sandbox on the host (no container).
- A stronger micro-VM or gVisor boundary in stage 1.
- No sandbox; rely on policy and the harness.

## Decision

Docker is the stage-1 sandbox driver (`internal/sandbox/docker`). It is the reference implementation of `sandbox.Driver`. There is no second sandbox implementation in stage 1. Changes to the port land with the conformance suite in the same change.

## Consequences

- Operators need Docker. That is a stage-1 dependency, not a hosted fleet.
- A missing or broken Docker install is a sandbox failure, not a reason to run on the host.
- Firecracker, gVisor, WASM, or a second container runtime would be a new subpackage plus a conformance row, and only when an issue says so.

## Revisit triggers

- Docker cannot be the isolation boundary on the operator's machine (rootless constraints, cgroup limits, or a platform where Docker is not the available runtime).
- Conformance requires isolation Docker does not provide (for example a hardware-enforced guest boundary).
- A stage-1 issue explicitly adds a second `Driver` implementation.
