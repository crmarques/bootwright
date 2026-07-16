//go:build !unix

package execution

import "os"

func terminateProcess(process *os.Process) error {
	return process.Kill()
}
