# ADR-Z-0002: MIT License

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)
- Plan: [§6, decision 3](../design/plan.md)

## Context

Zeroth is public from day one ([plan §6, decision 4](../design/plan.md)). Visitors and downstream users need a license that is short, well understood, and compatible with the UI primitives we intend to use ([Beautiful UI](https://www.beautifului.dev/), MIT).

Copyleft would complicate linking a local control plane to Claude Code, Docker, Linear, and a React UI without buying corresponding benefits in stage 1.

## Decision drivers

- The repository is already public; the license must match that fact on day one.
- Stage 1 depends on MIT (or similarly permissive) components at the UI and tooling edges.
- A later relicensing is costlier than choosing a well-known permissive license now.

## Considered options

- MIT.
- Apache-2.0.
- GPL or other copyleft.
- Unlicensed / "source available" until a launch.

## Decision

The project is licensed under the MIT License. The canonical text is the `LICENSE` file at the repository root. Copyright is held by Aviv Laufer.

## Consequences

- Every source distribution includes the copyright and permission notice.
- Downstream use, modification, and sublicensing are allowed; we do not impose share-alike.
- A later change of license is possible but costly; MIT is the default we are willing to live with.

## Revisit triggers

- A dependency we cannot isolate requires a copyleft license that would apply to the daemon or kernel.
- The project moves under a foundation or CLA that mandates Apache-2.0 (or another OSI license) as the only option.
- We ship a component that must not be sublicensed (then that component is not this repository, or this ADR is superseded).
