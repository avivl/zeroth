// Package secretscan scans plans, diffs, and logs for secrets.
//
// Findings block apply. The scan is a gate on the plan lifecycle, not a
// best-effort linter the operator can skip.
package secretscan
