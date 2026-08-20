// Package policy is the kernel: scopes, grants, and leases.
//
// Scopes name what an agent may touch. Grants assign those scopes to a
// principal. Leases time-bound a grant so permission expires without a
// separate revoke. Nothing else in the process is allowed to outrank this
// package; human control of consequential actions is enforced here.
package policy
