//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type ProcessExecutor struct{}

// GuardParent prevents an elevated command from surviving termination of its
// verified sudo parent, including the kill a second operator signal makes the
// supervisor send to sudo; the supervisor itself sets no deadline on the
// command. It is armed before root state access; process death releases held
// file locks.
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
	return verifiedExecutable(path)
}

// verifiedExecutable proves path names the running binary. Only the
// unprivileged supervisor resolves it, before it elevates: the elevated child
// re-executes /proc/self/exe. That account is refused the resolution when it
// cannot search a directory on the path, as when it runs the executable from
// a working directory it inherited beneath one, so that refusal names the path
// and the local copy that avoids it.
func verifiedExecutable(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrPermission) {
		return "", unreadableExecutable(path)
	}
	if err != nil {
		return "", errors.New("invocation executable is unavailable")
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, fs.ErrPermission) {
		return "", unreadableExecutable(resolved)
	}
	self, selfErr := os.Stat("/proc/self/exe")
	if err != nil || selfErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, self) {
		return "", errors.New("invocation executable changed")
	}
	return resolved, nil
}

func unreadableExecutable(path string) error {
	return diagnostics.NewFailureWithRemediation("runtime.privilege",
		"the invoking account cannot resolve the Bootwright executable at "+path+" (permission denied)", "",
		"copy the executable to a local directory both the invoking account and root can read, such as /usr/local/bin, and run it from there")
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

// relayGrace is how long the policy probe or a refresh has to finish after
// the relayed signal before it is killed. For the elevated command it bounds
// only stream closure after sudo exits: that command bounds its own
// cancellation, and only a second operator signal kills it.
const relayGrace = 5 * time.Second

// errRelayed is what cancellation returns once it relayed the signal. It wraps
// os.ErrProcessDone, which keeps os/exec reporting the command's own status
// (Cmd.Cancel): the child chose that status after the relay, 0 included, while
// streams still open at the end of the grace remain an error.
var errRelayed = fmt.Errorf("signal relayed: %w", os.ErrProcessDone)

func runProcess(ctx context.Context, request Command, grace time.Duration) (int, error) {
	if request.Elevated {
		return runRelayed(ctx, request, grace)
	}
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
	return processStatus(command.Run())
}

// runRelayed runs the elevated command without a context, so os/exec sets it
// no deadline: WaitDelay starts only once Wait observes sudo's exit. The
// watcher relays the operator's signal once and kills sudo on escalation; a
// signal after Wait returns meets os.ErrProcessDone.
func runRelayed(ctx context.Context, request Command, grace time.Duration) (int, error) {
	command := exec.Command(request.Executable, request.Arguments...)
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin, command.Stdout, command.Stderr = request.Input, request.Output, request.Error
	command.WaitDelay = grace
	if err := command.Start(); err != nil {
		return processStatus(err)
	}
	waited, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			command.Process.Signal(cancellationSignal(ctx))
		case <-waited:
			return
		}
		select {
		case <-escalation(ctx):
			command.Process.Kill()
		case <-waited:
		}
	}()
	err := command.Wait()
	close(waited)
	<-watched
	return processStatus(err)
}

func processStatus(err error) (int, error) {
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
