# ADR-Z-0001: Public from day one

- Status: Accepted
- Date: 2026-08-20
- Plan: [§6, decision 4](../design/plan.md)

## Context

It is tempting to keep a new project private until it “looks like a product.” That habit produces a private repository that claims to be open source, then a painful rewrite of history, CLA, and assumption when it finally opens.

## Decision

The Zeroth repository is public from day one. We do not wait for a polish milestone, a launch blog post, or stage 2.

## Consequences

- Docs and README must be honest about stage 1 (local, single-player, no deployment story). An honest public README is cheaper than a private myth.
- Secrets, credentials, and unpublished vendor deals do not belong in this repo.
- External visitors will see a skeleton. That is acceptable; wasting their time with implied completeness is not.
