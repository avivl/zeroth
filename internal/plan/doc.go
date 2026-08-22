// Package plan is the plan lifecycle: draft, cross-exam, approve, apply.
//
// Consequential actions are proposed as a plan before they are applied.
// Cross-exam is a structured challenge of the draft by a reviewer that
// does not share the producer's context (Z1-019). Approve is the human
// gate; apply is the only path that mutates the world. Autonomy is earned
// tier by tier: it does not skip this package.
//
// A plan is a typed set of resource rows plus the constraints it was
// drafted under (Z1-052). Each row names an operation, a target, a
// payload, the lease it will consume, a precondition observed at draft
// time, an idempotency key, and an expected postcondition. The plan as a
// whole carries a canonical hash, an expiry, a cost ceiling, and the
// scope and credential classes it was drafted under. A revised plan is a
// new hash, so it needs a new approval by construction.
//
// The builder turns harness proposed effects into those rows. An effect
// that cannot be expressed as a row stops the run. There is no fallback
// to direct action: a plan model with an escape hatch is not a plan model.
//
// Apply is deliberately boring (Linear 42-26). It rechecks every
// precondition and fails closed on drift, verifies that the approval
// covers exactly this plan hash, takes a checkpoint, acquires leases,
// executes rows in order (each idempotent by key), signs what it applied,
// and releases leases. A mid-apply failure leaves applied rows applied
// and records the exact boundary. Recovery drafts a new plan from
// observed postconditions rather than replaying the rest blindly.
package plan
