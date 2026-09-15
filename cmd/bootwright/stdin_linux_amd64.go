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

// These capabilities are acquired lazily after command safeguards authorize
// confirmation or explicit secret input. Polling keeps cancellation synchronous.
func stdinTerminal() (bool, error) {
	return terminalDescriptor(os.Stdin.Fd())
}

func terminalDescriptor(fd uintptr) (bool, error) {
	var terminal syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&terminal)))
	if errno == syscall.ENOTTY {
		return false, nil
	}
	if errno != 0 {
		return false, errno
	}
	return true, nil
}

// A terminal that reports no size is treated as the classic 80 columns, which
// keeps a redrawn row within the narrowest width an operator is likely to use.
const defaultTerminalColumns = 80

// terminalColumns reports how wide the terminal behind writer is, measured
// again on every call so a resize takes effect. A writer that is not a
// terminal has no width and receives appended output instead.
func terminalColumns(writer io.Writer) func() int {
	file, ok := terminalFile(writer)
	if !ok {
		return nil
	}
	return func() int { return terminalWidth(file.Fd()) }
}

func terminalWidth(fd uintptr) int {
	var window struct{ Rows, Columns, XPixels, YPixels uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&window)))
	if errno != 0 || window.Columns == 0 {
		return defaultTerminalColumns
	}
	return int(window.Columns)
}

// terminalFile reports a stream an elevated child can inherit as the same
// terminal descriptor; every other writer is copied through the supervisor.
func terminalFile(writer io.Writer) (*os.File, bool) {
	file, ok := writer.(*os.File)
	if !ok {
		return nil, false
	}
	terminal, err := terminalDescriptor(file.Fd())
	if err != nil || !terminal {
		return nil, false
	}
	return file, true
}

func readStdin(ctx context.Context, buffer []byte) (int, error) {
	return readInputFD(ctx, int(os.Stdin.Fd()), buffer)
}

func readInputFD(ctx context.Context, fd int, buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if n, err := requestBackgroundTerminal(fd, buffer); n != 0 || err != nil {
		return n, err
	}
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
		if descriptor.Returned&(1|16) == 0 {
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

// A process group behind its controlling terminal receives nothing until job
// control grants the terminal, and poll never asks for it. One real read from
// the background raises SIGTTIN, so a foreground sudo or a shell's fg can hand
// the terminal over before polling starts; an orphaned group reads EIO instead.
func requestBackgroundTerminal(fd int, buffer []byte) (int, error) {
	var foreground int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&foreground))); errno != 0 || int(foreground) == syscall.Getpgrp() {
		return 0, nil
	}
	n, err := readReadyInput(fd, buffer)
	if err == syscall.EINTR || err == syscall.EAGAIN {
		return 0, nil
	}
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
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
