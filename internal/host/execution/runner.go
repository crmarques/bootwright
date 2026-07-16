package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type Command struct {
	Name           string
	Args           []string
	Dir            string
	Env            []string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	GracefulCancel bool
	WaitDelay      time.Duration
}

type Runner interface {
	Run(ctx context.Context, cmd Command) error
	Output(ctx context.Context, cmd Command) ([]byte, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, cmd Command) error {
	return build(ctx, cmd).Run()
}

func (OSRunner) Output(ctx context.Context, cmd Command) ([]byte, error) {
	return build(ctx, cmd).Output()
}

func build(ctx context.Context, cmd Command) *exec.Cmd {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	c.Env = cmd.Env
	c.Stdin = cmd.Stdin
	c.Stdout = cmd.Stdout
	c.Stderr = cmd.Stderr
	if cmd.GracefulCancel {
		c.Cancel = func() error {
			return terminateProcess(c.Process)
		}
		c.WaitDelay = cmd.WaitDelay
	}
	return c
}

func AppendEnv(extra ...string) []string {
	return append(os.Environ(), extra...)
}

func ExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code, true
		}
		if code, ok := signaledExitCode(exitErr); ok {
			return code, true
		}
		return 1, true
	}
	return 0, false
}

func ExitStderr(err error) []byte {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Stderr
	}
	return nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
