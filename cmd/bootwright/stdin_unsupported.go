//go:build !linux || !amd64

package main

import (
	"context"
	"errors"
	"io"
	"os"
)

func stdinTerminal() (bool, error) { return false, nil }

func terminalFile(io.Writer) (*os.File, bool) { return nil, false }

func readStdin(ctx context.Context, _ []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("interactive confirmation requires Linux on amd64")
}
