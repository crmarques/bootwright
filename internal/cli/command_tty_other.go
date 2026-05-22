//go:build !linux

package cli

import (
	"context"
	"io"
	"os/exec"
)

func runCommandWithControllingTTY(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
