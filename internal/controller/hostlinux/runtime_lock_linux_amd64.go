//go:build linux && amd64

package hostlinux

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

func (f *heldFilesystem) runtimeLock(ctx context.Context, name string) (*os.File, error) {
	file, _, err := f.openPathPolicy(ctx, name, true, false)
	if err != nil {
		return nil, inspectionFailure(ctx, "controller.unsupported", "the qualified native package lock is missing or unsafe")
	}
	// An OFD read lock conflicts with RPM's native POSIX write lock, while
	// keeping each concurrent inspection's lifetime independent. Closing an
	// unrelated descriptor cannot release another inspection's protection.
	lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lock); err != nil {
		file.Close()
		if err == unix.EACCES || err == unix.EAGAIN {
			return nil, inspectionFailure(ctx, "controller.conflict", "a native package transaction prevents coherent runtime inspection")
		}
		return nil, inspectionFailure(ctx, "controller.unsupported", "the native package lock cannot provide read-only inspection protection")
	}
	return file, nil
}
