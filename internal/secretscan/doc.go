// Package secretscan scans plans, diffs, logs, and sandbox exports for
// secrets.
//
// Findings block apply and block ExportTar. The scan is a gate, not a
// best-effort linter the operator can skip. Matched secret values are
// never included in a Finding.
package secretscan
