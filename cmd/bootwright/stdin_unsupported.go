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

func terminalColumns(io.Writer) func() int { return nil }

func readStdin(ctx context.Context, _ []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("interactive confirmation requires Linux on amd64")
}

type secretTerminal struct{}

func newSecretTerminal(*os.File, io.Writer) secretTerminal { return secretTerminal{} }

func (secretTerminal) Interactive() (bool, error) { return false, nil }

func (secretTerminal) ReadHidden(ctx context.Context, _ string, _ []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("a terminal prompt requires Linux on amd64")
}
