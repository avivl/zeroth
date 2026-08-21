// Package spike is the throwaway BA-6 confirmation-spike process.
//
// It exists to stand up sandbox and session interfaces, fixture
// workspaces, and a compose stack before any gate is measured. The
// interfaces here are meant to survive into M1 and M2 even if these
// implementations do not. Do not promote this directory into a
// long-lived subsystem.
package spike
