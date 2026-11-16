package nativeexec

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

type Outcome int

const (
	StartFailed Outcome = iota
	Exited
	Signalled
	Cancelled
	Deadline
	Lingered
)

type Command struct {
	Path        string
	Args        []string
	Env         []string
	Dir         string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	WaitDelay   time.Duration
	Interrupt   syscall.Signal
	Foreground  bool
	Credential  *syscall.Credential
	ParentDeath syscall.Signal
}

type Result struct {
	Outcome  Outcome
	ExitCode int
	Signal   syscall.Signal
	Err      error
}

func (r Result) Succeeded() bool { return r.Outcome == Exited && r.ExitCode == 0 && r.Err == nil }

const DefaultWaitDelay = time.Second

var errRelative = errors.New("the executable path is not absolute")

func Run(ctx context.Context, command Command) Result {
	if !filepath.IsAbs(command.Path) {
		return Result{Outcome: StartFailed, ExitCode: -1, Err: errRelative}
	}
	process := exec.CommandContext(ctx, command.Path, command.Args...)
	process.Env = append([]string{}, command.Env...)
	process.Dir = cmp.Or(command.Dir, "/")
	process.Stdin, process.Stdout, process.Stderr = command.Stdin, command.Stdout, command.Stderr
	process.WaitDelay = cmp.Or(command.WaitDelay, DefaultWaitDelay)
	process.SysProcAttr = attributes(command)
	group, interrupt := !command.Foreground, cmp.Or(command.Interrupt, syscall.SIGKILL)
	process.Cancel = func() error { return signal(process.Process, interrupt, group) }
	err := supervise(process, command.ParentDeath != 0)
	return classify(ctx, process, err, group)
}

func supervise(process *exec.Cmd, locked bool) error {
	if !locked {
		return process.Run()
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- process.Run()
	}()
	return <-done
}

func signal(process *os.Process, interrupt syscall.Signal, group bool) error {
	if !group {
		return process.Signal(interrupt)
	}
	if err := syscall.Kill(-process.Pid, interrupt); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

func classify(ctx context.Context, process *exec.Cmd, err error, group bool) Result {
	state := process.ProcessState
	ended := Result{ExitCode: -1}
	if state != nil {
		ended.ExitCode = state.ExitCode()
		if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			ended.Signal = status.Signal()
		}
	}
	if cause := ctx.Err(); cause != nil && (state == nil || !state.Success()) {
		ended.Outcome, ended.Err = Cancelled, cause
		if errors.Is(cause, context.DeadlineExceeded) {
			ended.Outcome = Deadline
		}
		return ended
	}
	if state == nil {
		return Result{Outcome: StartFailed, ExitCode: -1, Err: err}
	}
	if group && !state.Success() {
		_ = syscall.Kill(-state.Pid(), syscall.SIGKILL)
	}
	if ended.Signal != 0 {
		ended.Outcome, ended.Err = Signalled, err
		return ended
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		if group {
			_ = syscall.Kill(-state.Pid(), syscall.SIGKILL)
		}
		return Result{Outcome: Lingered, ExitCode: state.ExitCode(), Err: err}
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		err = nil
	}
	return Result{Outcome: Exited, ExitCode: state.ExitCode(), Err: err}
}
