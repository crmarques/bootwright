//go:build !linux

package cli

import "os"

func readPasswordNoEcho(_ *os.File) (string, bool, error) {
	return "", false, nil
}
