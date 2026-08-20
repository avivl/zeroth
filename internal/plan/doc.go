// Package plan is the plan lifecycle: draft, cross-exam, approve, apply.
//
// Consequential actions are proposed as a plan before they are applied.
// Cross-exam is a structured challenge of the draft; approve is the human
// gate; apply is the only path that mutates the world. Autonomy is earned
// tier by tier — it does not skip this package.
package plan
