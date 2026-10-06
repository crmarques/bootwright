package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Confirmation uses lazy, invocation-owned capabilities. The read callback must
// observe cancellation even while awaiting input and must not read ahead.
type Confirmation struct {
	read       func(context.Context, []byte) (int, error)
	out        io.Writer
	isTerminal func() (bool, error)
	invocation []string
}

var _ contexts.Confirmer = (*Confirmation)(nil)

// NewConfirmation performs no input, output or terminal detection.
func NewConfirmation(read func(context.Context, []byte) (int, error), out io.Writer, isTerminal func() (bool, error)) *Confirmation {
	return &Confirmation{read: read, out: out, isTerminal: isTerminal}
}

// Repeating gives the confirmer the arguments of the invocation it confirms
// for, so a refusal's remedy repeats exactly that command with --yes rather
// than a shorter one that could do more than the plan the operator reviewed.
func (c *Confirmation) Repeating(args []string) *Confirmation {
	if c == nil {
		return nil
	}
	repeating := *c
	repeating.invocation = slices.Clone(args)
	return &repeating
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

// confirmation is how one consumer asks before an effect and how it refuses
// when the answer is not yes: the code that consumer refuses under, the exact
// prompt, the words its message opens with, what the operator reviews before
// repeating the command with --yes, and the context a context-backed command
// acts in, which the repeated command names.
type confirmation struct {
	code, prompt, label, review, scope string
	object                             *diagnostics.ObjectIdentity
}

// Confirm asks before an effect on this host or on a context as a whole. The
// exact action each production consumer sends selects its entry, and every
// other action is the context store's own.
func (c *Confirmation) Confirm(ctx context.Context, action, name string) error {
	return c.confirm(ctx, confirmationOf(action, name))
}

// ConfirmIn asks before an effect on one object of a context, so its prompt
// names the object and the context it acts in.
func (c *Confirmation) ConfirmIn(ctx context.Context, action, object, contextName string) error {
	return c.confirm(ctx, confirmationIn(action, object, contextName))
}

func confirmationOf(action, name string) confirmation {
	shown := escapeDisplayLine(name)
	switch action {
	case "setup":
		return confirmation{code: "controller.setup", label: "setup", review: "the plan",
			prompt: "Confirm controller setup on " + shown + "? [y/N] "}
	case "media replace", "media delete":
		verb := strings.TrimPrefix(action, "media ")
		return confirmation{code: "media.store", label: "media", review: "it",
			prompt: "Confirm " + verb + " of stored media " + shown + "? [y/N] "}
	case "apply", "destroy":
		return confirmation{code: "lifecycle.state", label: action, review: "the plan", scope: name,
			prompt: "Confirm " + action + " for context " + shown + "? [y/N] "}
	case "trust":
		return confirmation{code: "trust.identity", label: "trust", review: "the plan", scope: name,
			prompt: "Confirm trust for context " + shown + "? [y/N] "}
	case "rotate secret encryption":
		return confirmation{code: "secret.store.conflict", label: "key rotation", review: "it", scope: name,
			prompt: "Confirm rotation of the secret encryption key of context " + shown + "? [y/N] "}
	}
	return confirmation{code: "context.state", label: "context", review: "it",
		prompt: "Confirm " + escapeDisplayLine(action) + " for context " + shown + "? [y/N] "}
}

func confirmationIn(action, object, contextName string) confirmation {
	shown := escapeDisplayLine(object) + " in context " + escapeDisplayLine(contextName)
	switch action {
	case "replace secret", "delete secret":
		noun := "replacement"
		if action == "delete secret" {
			noun = "deletion"
		}
		var identity *diagnostics.ObjectIdentity
		if api.ValidLexical("name", object) {
			identity = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Secret), Name: object}
		}
		return confirmation{code: "secret.store.conflict", label: "secret " + noun, object: identity,
			review: "it with bootwright secret check --context " + contextName, scope: contextName,
			prompt: "Confirm " + noun + " of secret " + shown + "? [y/N] "}
	case "stop machine", "restart machine", "force stop machine", "force restart machine":
		verb := strings.TrimSuffix(action, " machine")
		return confirmation{code: "machine.power", label: "power", review: "it", scope: contextName,
			prompt: "Confirm " + verb + " of machine " + shown + "? [y/N] "}
	}
	return confirmation{code: "context.state", label: "context", review: "it", scope: contextName,
		prompt: "Confirm " + escapeDisplayLine(action) + " of " + shown + "? [y/N] "}
}

// remedy repeats the invocation with --yes, in the context it acts in. A
// confirmer that was not given the invocation names the same command, never a
// shorter one.
func (c *Confirmation) remedy(asked confirmation) string {
	command := "the same command"
	if c != nil {
		if repeated := repeatedCommand(c.invocation, asked.scope); repeated != "" {
			command = repeated
		}
	}
	return "review " + asked.review + ", then repeat " + command + " with --yes"
}

// confirm asks one confirmation. Every refusal but a cancellation names the
// command that repeats it with --yes, so the remedy rather than the message
// carries the flag.
func (c *Confirmation) confirm(ctx context.Context, asked confirmation) error {
	refuse := func(reason string) error {
		reported := diagnostics.Diagnostic{Severity: "error", Code: asked.code, Message: asked.label + " confirmation " + reason, Object: asked.object}
		if reason != canceled {
			reported.Remediation = c.remedy(asked)
		}
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{reported}}
	}
	if ctx.Err() != nil {
		return refuse(canceled)
	}
	if c == nil || c.read == nil || c.out == nil || c.isTerminal == nil {
		return refuse("is not configured")
	}
	interactive, err := c.isTerminal()
	if ctx.Err() != nil {
		return refuse(canceled)
	}
	if err != nil || !interactive {
		return refuse("requires an interactive terminal")
	}
	if err := c.ask(ctx, asked.prompt); err != nil {
		reason := strings.TrimPrefix(err.Error(), "confirmation ")
		if reason == declined {
			reason += "; nothing changed"
		}
		return refuse(reason)
	}
	return nil
}

const (
	canceled = "was canceled"
	declined = "was declined"
)

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
			return errors.New("confirmation " + canceled)
		}
		n, err := c.read(ctx, answer[i:i+1])
		if ctx.Err() != nil {
			return errors.New("confirmation " + canceled)
		}
		if err != nil || n != 1 {
			return errors.New("confirmation answer could not be read")
		}
		if answer[i] == '\n' {
			value := strings.ToLower(strings.TrimSpace(string(answer[:i])))
			if value == "y" || value == "yes" {
				return nil
			}
			return errors.New("confirmation " + declined)
		}
	}
	return errors.New("confirmation answer exceeds the 64-byte limit")
}
