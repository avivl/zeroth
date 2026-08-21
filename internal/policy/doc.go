// Package policy is the kernel. It answers exactly one question: may this
// principal, in this scope, holding these leases, perform this effect?
//
// No I/O, no database, no network, no knowledge of harnesses. Everything
// else in Zeroth calls into this package; this package calls into nothing.
// That asymmetry is deliberate: it is what makes the kernel auditable and
// testable in isolation, and what keeps every other package honest about
// where authorization decisions actually happen.
//
// Deny by default. Every entry point returns a Decision carrying a reason
// string, allow or deny, because the reason is what lands in the audit log.
// There is no code path in this package that grants access silently.
package policy
