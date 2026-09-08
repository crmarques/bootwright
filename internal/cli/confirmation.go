package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Confirmation uses lazy, invocation-owned capabilities. The read callback must
// observe cancellation even while awaiting input and must not read ahead.
type Confirmation struct {
	read       func(context.Context, []byte) (int, error)
	out        io.Writer
	isTerminal func() (bool, error)
}

var _ contexts.Confirmer = (*Confirmation)(nil)

// NewConfirmation performs no input, output or terminal detection.
func NewConfirmation(read func(context.Context, []byte) (int, error), out io.Writer, isTerminal func() (bool, error)) *Confirmation {
	return &Confirmation{read: read, out: out, isTerminal: isTerminal}
}

func (c *Confirmation) Confirm(ctx context.Context, action, name string) error {
	if ctx.Err() != nil {
		return contexts.StateError("context confirmation was canceled")
	}
	if c == nil || c.read == nil || c.out == nil || c.isTerminal == nil {
		return contexts.StateError("context confirmation is not configured")
	}
	interactive, err := c.isTerminal()
	if ctx.Err() != nil {
		return contexts.StateError("context confirmation was canceled")
	}
	if err != nil || !interactive {
		return contexts.StateError("context confirmation requires interactive input; use --yes after reviewing the selected transition")
	}
	prompt := fmt.Sprintf("Confirm %s for context %s? [y/N] ", escapeDisplayLine(action), escapeDisplayLine(name))
	if n, err := io.WriteString(c.out, prompt); err != nil || n != len(prompt) {
		return contexts.StateError("context confirmation prompt could not be written")
	}
	// Read one byte at a time to avoid consuming input after the answer. The
	// 64-byte ceiling includes the LF terminating the single answer.
	var answer [64]byte
	for i := range answer {
		if ctx.Err() != nil {
			return contexts.StateError("context confirmation was canceled")
		}
		n, err := c.read(ctx, answer[i:i+1])
		if ctx.Err() != nil {
			return contexts.StateError("context confirmation was canceled")
		}
		if err != nil || n != 1 {
			return contexts.StateError("context confirmation answer could not be read")
		}
		if answer[i] == '\n' {
			value := strings.ToLower(strings.TrimSpace(string(answer[:i])))
			if value == "y" || value == "yes" {
				return nil
			}
			return contexts.StateError("context confirmation was declined")
		}
	}
	return contexts.StateError("context confirmation answer exceeds the 64-byte limit")
}
