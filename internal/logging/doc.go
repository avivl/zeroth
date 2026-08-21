// Package logging constructs Zap loggers for the daemon and CLI.
//
// There is no package-level logger. Callers create a logger and pass it to
// the code that needs it. internal/policy must not import this package or
// go.uber.org/zap: the kernel is I/O-free (ADR-Z-0001). If a policy decision
// ever needs a log line, the caller logs it after the decision returns.
package logging
