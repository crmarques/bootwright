//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

type ProcessExecutor struct{}

// GuardParent prevents an elevated command from surviving termination of its
// verified sudo parent, including the supervisor's final cancellation deadline.
// It is armed before root state access; process death releases held file locks.
// The caller must defer release on the same goroutine after all command work;
// the Linux guard belongs to this OS thread, whose lifetime must span that work.
func GuardParent(expectedPID int) (release func(), err error) {
	runtime.LockOSThread()
	if expectedPID <= 1 || os.Getppid() != expectedPID {
		runtime.UnlockOSThread()
		return nil, errors.New("sudo parent changed before invocation")
	}
	const setParentDeathSignal = 1
	release = func() {
		syscall.Syscall6(syscall.SYS_PRCTL, setParentDeathSignal, 0, 0, 0, 0, 0)
		runtime.UnlockOSThread()
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, setParentDeathSignal, uintptr(syscall.SIGKILL), 0, 0, 0, 0)
	if errno != 0 || os.Getppid() != expectedPID {
		release()
		return nil, errors.New("sudo parent lifetime cannot be guarded")
	}
	return release, nil
}

// Executable resolves the currently running binary, rejecting an exchanged
// pathname rather than executing a replacement through sudo.
func Executable() (string, error) {
	path, err := os.Executable()
	if err != nil || !filepath.IsAbs(path) {
		return "", errors.New("invocation executable is unavailable")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("invocation executable is unavailable")
	}
	info, err := os.Lstat(path)
	self, selfErr := os.Stat("/proc/self/exe")
	if err != nil || selfErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, self) {
		return "", errors.New("invocation executable changed")
	}
	return path, nil
}

// ReexecutionPath keeps the verified running executable pinned while sudo may
// wait for authentication. The supervisor stays alive until its child exits,
// so this process identity cannot be recycled during reexecution.
func ReexecutionPath() (string, error) {
	if _, err := Executable(); err != nil {
		return "", err
	}
	return "/proc/" + strconv.Itoa(os.Getpid()) + "/exe", nil
}

func (ProcessExecutor) Run(ctx context.Context, request Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	sudo, err := QualifiedSudo()
	if err != nil || sudo != request.Executable {
		return 1, errors.New("sudo executable cannot be verified")
	}
	args := append([]string(nil), request.Arguments...)
	for i, arg := range args {
		if arg == "--" {
			path, err := ReexecutionPath()
			if err != nil || i+1 >= len(args) || args[i+1] != path {
				return 1, errors.New("invocation executable cannot be verified")
			}
			break
		}
	}
	request.Executable, request.Arguments = sudo, args
	return runProcess(ctx, request, relayGrace)
}

// relayGrace is how long a command and its streams have to finish after the
// relayed signal before the command is killed or its streams are closed.
const relayGrace = 5 * time.Second

// errRelayed is what cancellation returns once it relayed the signal. It wraps
// os.ErrProcessDone, which keeps os/exec reporting the command's own status
// (Cmd.Cancel): the child chose that status after the relay, 0 included, while
// streams still open at the end of the grace remain an error.
var errRelayed = fmt.Errorf("signal relayed: %w", os.ErrProcessDone)

func runProcess(ctx context.Context, request Command, grace time.Duration) (int, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin, command.Stdout, command.Stderr = request.Input, request.Output, request.Error
	command.Cancel = func() error {
		if err := command.Process.Signal(cancellationSignal(ctx)); err != nil {
			return err
		}
		return errRelayed
	}
	command.WaitDelay = grace
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 1, err
}
