//go:build unix

package execution

import (
	"os/exec"
	"syscall"
)

func signaledExitCode(err *exec.ExitError) (int, bool) {
	status, ok := err.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return 0, false
	}
	return 128 + int(status.Signal()), true
}
