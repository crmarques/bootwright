//go:build !unix

package execution

import "os/exec"

func signaledExitCode(_ *exec.ExitError) (int, bool) {
	return 0, false
}
