//go:build linux && amd64

package kernel

import (
	"errors"
	"os"
	"syscall"
)

// BusyError is a root lock or context lease another Bootwright command holds.
// Nothing is wrong with the store, so it is reported as context.busy rather
// than context.state: the same command succeeds once the holder finishes, and
// Bootwright never takes a lock over.
type BusyError struct{ Failure error }

func Busy(message string) error {
	return &BusyError{ContextBusy.Fail(message, "retry after the running Bootwright command finishes")}
}

func (e *BusyError) Error() string { return e.Failure.Error() }

func (e *BusyError) Unwrap() error { return e.Failure }

func Lock(dir *Directory) error {
	if err := dir.Verify(); err != nil {
		return err
	}
	if err := syscall.Flock(int(dir.File.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); errors.Is(err, syscall.EWOULDBLOCK) {
		return Busy("context state is held by another mutator")
	} else if err != nil {
		return state("context state is held by another mutator")
	}
	if err := dir.Verify(); err != nil {
		syscall.Flock(int(dir.File.Fd()), syscall.LOCK_UN)
		return err
	}
	return nil
}

func LockShared(dir *Directory) error {
	if err := dir.Verify(); err != nil {
		return err
	}
	if err := syscall.Flock(int(dir.File.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); errors.Is(err, syscall.EWOULDBLOCK) {
		return Busy("context storage is busy")
	} else if err != nil {
		return state("context storage is busy")
	}
	return nil
}

// UnlockAndClose releases a lock before closing its descriptor. The lock
// belongs to the open file description, which a child forked meanwhile shares
// until it execs, so closing the descriptor alone can leave it held.
func UnlockAndClose(file *os.File) {
	syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	file.Close()
}
