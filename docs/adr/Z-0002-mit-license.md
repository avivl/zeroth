# ADR-Z-0002: MIT License

- Status: Accepted
- Date: 2026-08-20
- Plan: [§6, decision 3](../design/plan.md)

## Context

Zeroth is public from day one ([ADR-Z-0001](Z-0001-public-from-day-one.md)). Visitors and downstream users need a license that is short, well understood, and compatible with the UI primitives we intend to use ([Beautiful UI](https://www.beautifului.dev/), MIT).

Copyleft would complicate linking a local control plane to Claude Code, Docker, Linear, and a React UI without buying corresponding benefits in stage 1.

## Decision

The project is licensed under the MIT License. The canonical text is the `LICENSE` file at the repository root. Copyright is held by Aviv Laufer.

## Consequences

- Every source distribution includes the copyright and permission notice.
- Downstream use, modification, and sublicensing are allowed; we do not impose share-alike.
- A later change of license is possible but costly; MIT is the default we are willing to live with.
