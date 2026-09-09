package cli

import "errors"

// ErrInterrupted is the cancellation cause supplied by an invocation-owned
// operating-system interrupt adapter. Ordinary caller cancellation is distinct.
var ErrInterrupted = errors.New("operation interrupted")

// Only implemented application paths may acquire the operation cancellation
// capability. Informational and invalid invocations finish before this boundary.
func implementedOperation(path string) bool {
	switch path {
	case "context init", "context update", "context use", "context list", "context current", "context delete", "validate", "render effective",
		"secret set", "secret generate", "secret check", "secret list", "secret show", "secret delete",
		"secret encryption init", "secret encryption status", "secret encryption rotate":
		return true
	default:
		return false
	}
}
