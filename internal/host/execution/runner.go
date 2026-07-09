package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
)

type Command struct {
	Name   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
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
	return c
}

func AppendEnv(extra ...string) []string {
	return append(os.Environ(), extra...)
}

func ExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
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
