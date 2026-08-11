//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly || solaris

package workflow

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockRunLeaseFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func unlockRunLeaseFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_UN)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
