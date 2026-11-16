package cli

import "errors"

// ErrInterrupted is the cancellation cause supplied by an invocation-owned
// operating-system interrupt adapter. Ordinary caller cancellation is distinct.
var ErrInterrupted = errors.New("operation interrupted")

// Commands that reach the privileged context store re-execute as root. The two
// flag-dependent exceptions are resolved by ClassifyInvocation.
func privilegedOperation(path string) bool { return specFor(path).privileged }

func specFor(path string) commandSpec {
	for _, spec := range commandCatalog() {
		if spec.path == path {
			return spec
		}
	}
	return commandSpec{}
}
