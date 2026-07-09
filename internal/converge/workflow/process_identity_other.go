//go:build !linux

package workflow

func processStartToken(pid int) (string, bool) {
	return "", false
}
