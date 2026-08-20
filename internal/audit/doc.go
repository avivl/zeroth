// Package audit is the append-only trail of signed events.
//
// Every consequential action is recorded here. The log is meant to be
// verifiable, not merely informational: records are signed (see package
// signer) and are not rewritten in place.
package audit
