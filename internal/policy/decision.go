package policy

import "fmt"

// Decision is the kernel's answer to an authorization question. Reason is
// never empty on either branch: an allow names the lease that covered the
// effect, a deny names why nothing did. Both land in the audit log, because
// "why was this allowed" matters as much as "why was this denied."
type Decision struct {
	Allowed bool
	Reason  string
}

func allow(format string, args ...any) Decision {
	return Decision{Allowed: true, Reason: fmt.Sprintf(format, args...)}
}

func deny(format string, args ...any) Decision {
	return Decision{Allowed: false, Reason: fmt.Sprintf(format, args...)}
}
