//go:build linux

package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/host/ptyexec"
)

func runCommandWithControllingTTY(ctx context.Context, _ io.Reader, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	return ptyexec.RunCommand(ctx, stdout, stderr, args, env)
}
