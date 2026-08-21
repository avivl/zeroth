# ADR-Z-0006: Linear first, GitHub Issues second

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

Zeroth drives real work through a tracker port. Stage 1 is one operator and one tracker. Shipping GitHub Issues "while we are here" would be a second implementation of the same port, which stage 1 does not do unless an issue says otherwise.

## Decision drivers

- The project already runs on Linear (this decision register included).
- The daemon depends on `tracker.Provider`, not on a vendor SDK by name.
- GitHub Issues is the obvious second provider; naming it now avoids pretending the port is Linear-shaped.

## Considered options

- Linear as the stage-1 provider; GitHub Issues as the next one.
- GitHub Issues first (the code already lives on GitHub).
- A generic issue file or local markdown tracker.
- Two providers in stage 1.

## Decision

Linear is the first tracker provider (`internal/tracker/linear`). GitHub Issues is second: not in stage 1, and not as a parallel implementation "for completeness." The port stays vendor-neutral so the second provider can land without kernel changes.

## Consequences

- Stage-1 sessions, plans, and comments speak Linear's identifiers at the edge, behind the port.
- GitHub remains the host of the public git repo; it is not the stage-1 work tracker.
- A GitHub Issues implementation is a later subpackage plus a conformance row, with its own issue.

## Revisit triggers

- An issue explicitly schedules GitHub Issues (or another provider) as a stage-1 exception.
- Linear's API cannot represent the session or plan lifecycle without encoding kernel state in a comment thread.
- The operator's work for this project leaves Linear and does not return.
