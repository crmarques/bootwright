package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
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

// ConfirmHostKey asks the operator to accept one server identity that nothing
// has proved yet. It shows the fingerprint they compare out of band and the
// context that would trust it, and a declined or unanswerable prompt records
// nothing at all.
func (c *Confirmation) ConfirmHostKey(ctx context.Context, contextName, name, address, keyType, fingerprint string) error {
	refuse := func(reason string) error {
		return diagnostics.NewFailureWithRemediation("trust.identity",
			"host key confirmation "+reason, "",
			"record it with bootwright machine trust --context "+contextName+" --machines "+name)
	}
	if c == nil || c.read == nil || c.out == nil || c.isTerminal == nil {
		return refuse("is not configured")
	}
	if ctx.Err() != nil {
		return refuse("was canceled")
	}
	interactive, err := c.isTerminal()
	if err != nil || !interactive {
		return refuse("requires interactive input")
	}
	prompt := fmt.Sprintf("Trust %s %s for Machine %s at %s in context %s? [y/N] ",
		escapeDisplayLine(keyType), escapeDisplayLine(fingerprint),
		escapeDisplayLine(name), escapeDisplayLine(address), escapeDisplayLine(contextName))
	if err := c.ask(ctx, prompt); err != nil {
		return refuse(strings.TrimPrefix(err.Error(), "confirmation "))
	}
	return nil
}

func (c *Confirmation) Confirm(ctx context.Context, action, name string) error {
	media, mediaVerb := strings.CutPrefix(action, "media ")
	machine, machineVerb := strings.CutSuffix(action, " machine")
	failure := func(reason string) error {
		switch {
		case action == "setup":
			return diagnostics.NewFailure("controller.setup", "setup confirmation "+reason, "")
		case mediaVerb:
			return diagnostics.NewFailure("media.store", "media confirmation "+reason, "")
		case machineVerb:
			return diagnostics.NewFailure("machine.power", "power confirmation "+reason, "")
		}
		return contexts.StateError("context confirmation " + reason)
	}
	if ctx.Err() != nil {
		return failure("was canceled")
	}
	if c == nil || c.read == nil || c.out == nil || c.isTerminal == nil {
		return failure("is not configured")
	}
	interactive, err := c.isTerminal()
	if ctx.Err() != nil {
		return failure("was canceled")
	}
	if err != nil || !interactive {
		return failure("requires interactive input; use --yes after reviewing the selected transition")
	}
	prompt := fmt.Sprintf("Confirm %s for context %s? [y/N] ", escapeDisplayLine(action), escapeDisplayLine(name))
	switch {
	case action == "setup":
		prompt = fmt.Sprintf("Confirm controller setup on %s? [y/N] ", escapeDisplayLine(name))
	case mediaVerb:
		prompt = fmt.Sprintf("Confirm %s of stored media %s? [y/N] ", escapeDisplayLine(media), escapeDisplayLine(name))
	case machineVerb:
		prompt = fmt.Sprintf("Confirm %s of machine %s? [y/N] ", escapeDisplayLine(machine), escapeDisplayLine(name))
	}
	if err := c.ask(ctx, prompt); err != nil {
		return failure(strings.TrimPrefix(err.Error(), "confirmation "))
	}
	return nil
}

// ask writes one prompt and reads a single answer. It reads one byte at a time
// to avoid consuming input after the answer, and its 64-byte ceiling includes
// the LF that terminates it.
func (c *Confirmation) ask(ctx context.Context, prompt string) error {
	if n, err := io.WriteString(c.out, prompt); err != nil || n != len(prompt) {
		return errors.New("confirmation prompt could not be written")
	}
	var answer [64]byte
	for i := range answer {
		if ctx.Err() != nil {
			return errors.New("confirmation was canceled")
		}
		n, err := c.read(ctx, answer[i:i+1])
		if ctx.Err() != nil {
			return errors.New("confirmation was canceled")
		}
		if err != nil || n != 1 {
			return errors.New("confirmation answer could not be read")
		}
		if answer[i] == '\n' {
			value := strings.ToLower(strings.TrimSpace(string(answer[:i])))
			if value == "y" || value == "yes" {
				return nil
			}
			return errors.New("confirmation was declined")
		}
	}
	return errors.New("confirmation answer exceeds the 64-byte limit")
}
