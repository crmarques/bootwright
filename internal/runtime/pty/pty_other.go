//go:build !linux

package ptyexec

import (
	"context"
	"io"
	"os/exec"
)

func RunCommand(ctx context.Context, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
