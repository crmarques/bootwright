//go:build !unix

package workflow

func processAlive(pid int) bool {
	return pid > 0
}
