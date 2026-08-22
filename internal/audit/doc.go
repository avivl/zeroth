// Package audit is the append-only trail of signed events.
//
// Every consequential action is recorded here. The log is meant to be
// verifiable, not merely informational: records are signed (see package
// signer) and chained by prev_hash so an auditor who does not trust
// Zeroth can still check the file. Config changes, including autonomy
// tier, are signed the same way as apply.
package audit
