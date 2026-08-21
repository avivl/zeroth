# ADR-Z-0009: Canonical plan hash and closed effect set

- Status: Accepted
- Date: 2026-08-21
- Linear: [42-25](https://linear.app/42-golems/issue/42-25/change-plan-model-effects-preconditions-hashes-expiry-cost-ceiling)

## Context

The plan is the product. Apply is only allowed to do what an approved plan lists. If the bundle the operator saw can be rewritten in place (row order, serialization, a "small" extra effect the builder could not type), approval is theatre.

The harness proposes effects. Some of those proposals will not fit a resource row. The tempting move is to let the run continue with a sidecar action. That is an escape hatch, and a plan model with an escape hatch is not a plan model.

## Decision drivers

- A revised plan must be a new identity so it needs a new approval by construction.
- Independent file writes may be reordered without changing meaning. Sequential operations on one target may not.
- Apply must be exactly the approved effect set, not "the same paths, more or less."
- Unexpressible effects must halt. The operator is asked. Nothing is applied on the side.

## Considered options

- Hash the JSON the store happens to write. Rejected: encoder whitespace, key order, and row order would mint new approvals for the same plan.
- Hash a set of rows with no order at all. Rejected: create-then-modify is not modify-then-create.
- Let unknown harness effects fall through to a tool call. Rejected: that is apply without a plan.

## Decision

`internal/plan` is the domain model (Z1-052). Each row carries operation, target, payload, the lease it will consume, a precondition observed at draft time, an idempotency key, and an expected postcondition. The plan as a whole carries a canonical SHA-256 over a versioned, length-prefixed encoding of those rows plus summary, expiry, cost ceiling, scope, and credential classes (never secrets).

Independent rows (different targets) are sorted by target before hashing, using a stable sort so same-target rows keep their relative order. Identity, status, cross-exam, and review comments are not hashed: approve gates this bundle, it does not rewrite it.

The builder turns harness proposed effects into rows. Unknown types, missing leases, missing preconditions, and non-workspace targets return `ErrUnexpressible` and stop the run.

Apply is permitted to execute exactly the rows of an approved, unexpired plan whose stored hash still matches. Anything else is a deny.

## Consequences

- Store round-trip must reproduce the rendered plan byte-for-byte. The operator-facing text is what was approved.
- Changing the hash encoding is a new version string (`zeroth-plan-v1`) and therefore a new hash. In-flight approvals cannot be silently reinterpreted.
- The kernel still authorizes on kind, scope, and target (`policy.Plan`). This package checks the full row, including payload and hashes.

## Revisit triggers

- A second effect kind that cannot be a workspace path or a memory key (for example a tracker comment) needs a new row type, not an escape hatch.
- The hash encoding is too weak (collision, missing field) or too strict (false revisions).
- Stage 2 shared plans across operators, which would need a signature over the hash, not a new hash algorithm.
