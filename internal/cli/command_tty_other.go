//go:build !linux

package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/host/execution"
)

func runCommandWithControllingTTY(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	return execution.OSRunner{}.Run(ctx, execution.Command{
		Name:   args[0],
		Args:   args[1:],
		Env:    env,
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}
