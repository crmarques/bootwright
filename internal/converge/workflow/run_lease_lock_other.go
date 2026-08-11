//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris

package workflow

import (
	"errors"
	"os"
)

func lockRunLeaseFile(*os.File) error {
	return errors.New("mutating run lease serialization is unsupported on this platform")
}

func unlockRunLeaseFile(*os.File) error {
	return nil
}
