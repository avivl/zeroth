// Package lease issues and expires runtime leases.
//
// Policy defines what a lease is allowed to be (scopes, grants, duration).
// This package is the runtime that mints, renews, and expires those leases
// so a grant cannot outlive its intended window.
package lease
