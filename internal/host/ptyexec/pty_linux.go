//go:build linux

package ptyexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// processGroupTerminationGrace bounds how long RunCommand waits after signalling
// the child's session/process group on cancellation before Go force-kills the
// leader. Matches the non-TTY ansible runner's grace.
const processGroupTerminationGrace = 5 * time.Second

func RunCommand(ctx context.Context, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	master, slave, err := openPseudoTerminal()
	if err != nil {
		return fmt.Errorf("allocate pseudo-terminal: %w", err)
	}
	defer master.Close()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	outputMu := &sync.Mutex{}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	// Setsid makes the child a session/process-group leader, so on cancellation
	// signal the whole group (negative PID) to reap ansible's ssh/python
	// children rather than only the leader; WaitDelay escalates to a force-kill
	// if the group does not exit in time. Without this the detached session
	// would survive shutdown and orphan to PID 1.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = processGroupTerminationGrace

	ttyOutput := synchronizedWriter(stdout, outputMu)
	if ttyOutput == nil {
		ttyOutput = synchronizedWriter(stderr, outputMu)
		if ttyOutput == nil {
			ttyOutput = io.Discard
		}
	}
	drained := make(chan struct{})
	go func() {
		_, copyErr := io.Copy(ttyOutput, master)
		if copyErr != nil && !errors.Is(copyErr, os.ErrClosed) && !errors.Is(copyErr, syscall.EIO) {
			fmt.Fprintf(ttyOutput, "warning: read pseudo-terminal output: %v\n", copyErr)
		}
		close(drained)
	}()

	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		_ = master.Close()
		<-drained
		return err
	}
	_ = slave.Close()
	err = cmd.Wait()
	_ = master.Close()
	<-drained
	return err
}

func openPseudoTerminal() (master *os.File, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = master.Close()
		}
	}()

	slavePath, err := pseudoTerminalSlavePath(master)
	if err != nil {
		return nil, nil, err
	}
	if err := unlockPseudoTerminal(master); err != nil {
		return nil, nil, err
	}
	slave, err = os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	return master, slave, nil
}

func pseudoTerminalSlavePath(master *os.File) (string, error) {
	var n uint32
	if err := ioctl(master, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		return "", err
	}
	return "/dev/pts/" + strconv.Itoa(int(n)), nil
}

func unlockPseudoTerminal(master *os.File) error {
	var unlocked int32
	return ioctl(master, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlocked)))
}

func ioctl(file *os.File, request uint, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(request), arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func synchronizedWriter(w io.Writer, mu *sync.Mutex) io.Writer {
	if w == nil {
		return nil
	}
	return lockedWriter{w: w, mu: mu}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
