package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
)

// Command describes one local process launch through the Runner seam.
type Command struct {
	Name string
	Args []string
	Dir  string
	// Env is the exact process environment; nil inherits the parent's. Use
	// AppendEnv to extend the inherited environment.
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Runner is the single local-process seam. Production code outside
// internal/host and internal/converge/ansible launches processes through it
// so tests can substitute a fake; the boundary is guard-tested in
// internal/repo/checks.
type Runner interface {
	// Run starts the command and waits for it, honoring the Command's stdio.
	Run(ctx context.Context, cmd Command) error
	// Output runs the command and returns its stdout; stderr goes to
	// cmd.Stderr when set and is otherwise captured into the returned error.
	Output(ctx context.Context, cmd Command) ([]byte, error)
}

// OSRunner launches commands with os/exec.
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

// AppendEnv extends the inherited environment for Command.Env.
func AppendEnv(extra ...string) []string {
	return append(os.Environ(), extra...)
}

// ExitCode extracts a process exit code from a Runner error.
func ExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// ExitStderr returns the captured stderr of a failed Output call.
func ExitStderr(err error) []byte {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Stderr
	}
	return nil
}

// IsNotFound reports whether the command's binary was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// LookPath resolves a binary on PATH.
func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
