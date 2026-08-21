// Package resilience is the Failsafe-go reference pattern for wrapping
// unreliable external calls (docker, subprocess supervision, tracker API).
//
// Compose retry (outer) around timeout (inner) so each attempt has a deadline
// and transient failures are retried. Add a circuit breaker between them when
// the same remote is called repeatedly. Do not invent a second retry loop in a
// driver. See docs/design/resilience.md.
package resilience
