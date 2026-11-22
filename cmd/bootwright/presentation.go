package main

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets/material"
)

var errStructuredSecretTerminal = errors.New("structured secret input requires a pipe or file")

// This adapter accepts pipes and refuses hidden interactive reads.
type structuredSecretTerminal struct{ source material.TerminalInput }

func (t structuredSecretTerminal) Interactive() (bool, error) {
	interactive, err := t.source.Interactive()
	if err != nil {
		return false, err
	}
	if interactive {
		return false, errStructuredSecretTerminal
	}
	return false, nil
}

func (structuredSecretTerminal) ReadHidden(context.Context, string, []byte) (int, error) {
	return 0, errStructuredSecretTerminal
}

type enrollmentPromptTerminal struct {
	material.TerminalInput
	withPrompt func(func())
}

func (t enrollmentPromptTerminal) ReadHidden(ctx context.Context, prompt string, buffer []byte) (n int, err error) {
	t.withPrompt(func() {
		n, err = t.TerminalInput.ReadHidden(ctx, prompt, buffer)
	})
	return n, err
}
