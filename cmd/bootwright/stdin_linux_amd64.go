//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// These capabilities are called only after context mutation safeguards decide
// that ordinary confirmation is needed. Polling keeps cancellation synchronous.
func stdinTerminal() (bool, error) {
	var terminal syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&terminal)))
	if errno == syscall.ENOTTY {
		return false, nil
	}
	if errno != 0 {
		return false, errno
	}
	return true, nil
}

func readStdin(ctx context.Context, buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	fd := int(os.Stdin.Fd())
	descriptor := struct {
		FD       int32
		Events   int16
		Returned int16
	}{FD: int32(fd), Events: 1}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		descriptor.Returned = 0
		count, _, errno := syscall.Syscall(syscall.SYS_POLL, uintptr(unsafe.Pointer(&descriptor)), 1, 100)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return 0, errno
		}
		if count == 0 {
			continue
		}
		if descriptor.Returned&1 == 0 {
			return 0, errors.New("standard input is unavailable")
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, err := readReadyInput(fd, buffer)
		if err == syscall.EINTR || err == syscall.EAGAIN {
			continue
		}
		if n == 0 && err == nil {
			return 0, io.EOF
		}
		return n, err
	}
}

// A terminal can flush a ready byte when Ctrl-C arrives. A nonblocking read
// closes that race; restore the invocation's original descriptor flags before
// returning. Bootwright has exactly one reader during ordinary confirmation.
func readReadyInput(fd int, buffer []byte) (int, error) {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFL, 0)
	if errno != 0 {
		return 0, errno
	}
	changed := flags&syscall.O_NONBLOCK == 0
	if changed {
		if _, _, errno = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFL, flags|syscall.O_NONBLOCK); errno != 0 {
			return 0, errno
		}
	}
	n, err := syscall.Read(fd, buffer)
	if changed {
		for {
			_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFL, flags)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				return 0, errors.New("standard input flags could not be restored")
			}
			break
		}
	}
	return n, err
}
