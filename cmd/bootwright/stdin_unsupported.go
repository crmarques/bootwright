//go:build !linux || !amd64

package main

import (
	"context"
	"errors"
)

func stdinTerminal() (bool, error) { return false, nil }

func readStdin(ctx context.Context, _ []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("interactive confirmation requires Linux on amd64")
}
