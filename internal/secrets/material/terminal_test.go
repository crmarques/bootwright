package material

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

// fakeTerminal is a terminal behind standard input that types line.
type fakeTerminal struct {
	interactive bool
	failure     error
	line        string
	prompts     []string
}

func (f *fakeTerminal) Interactive() (bool, error) { return f.interactive, f.failure }

func (f *fakeTerminal) ReadHidden(_ context.Context, prompt string, buffer []byte) (int, error) {
	f.prompts = append(f.prompts, prompt)
	return copy(buffer, f.line), nil
}

// countingInput counts reads of standard input, which serves value and EOF.
type countingInput struct {
	value string
	reads int
}

func (c *countingInput) Read(_ context.Context, buffer []byte) (int, error) {
	c.reads++
	if c.reads > 1 {
		return 0, io.EOF
	}
	return copy(buffer, c.value), nil
}

// At a terminal, a token or password is typed after a prompt naming its Secret
// and context with echo off, while an opaque or Docker configuration value
// refuses before anything is read; a pipe or file is read to its end (D73).
func TestTerminalStdinPromptsForTokensAndPasswordsAndRefusesOpaque(t *testing.T) {
	for _, test := range []struct {
		kind, prompt string
		input        secrets.Input
		part         secrets.Part
	}{
		{"token", "Token for Secret fixture in context lab: ", secrets.Input{ValueStdin: true}, secrets.ValuePart},
		{"usernamePassword", "Password for Secret fixture in context lab: ", secrets.Input{Username: "operator", PasswordStdin: true}, secrets.PasswordPart},
	} {
		t.Run(test.kind, func(t *testing.T) {
			terminal := &fakeTerminal{interactive: true, line: "typed-secret\n"}
			input := &countingInput{}
			test.input.ContextName = "lab"
			value, err := New(input, Options{Terminal: terminal}).Acquire(context.Background(), secrets.Declaration{Name: "fixture", Type: test.kind, Source: "contextStore"}, test.input)
			if err != nil {
				t.Fatalf("Acquire: %+v", diagnostics.Of(err))
			}
			defer value.Clear()
			if got := requiredPart(t, value, test.part); string(got) != "typed-secret" || len(terminal.prompts) != 1 || terminal.prompts[0] != test.prompt || input.reads != 0 {
				t.Fatalf("typed %q after prompts %q with %d reads of the input", got, terminal.prompts, input.reads)
			}
			terminal.line = "\n"
			_, err = New(input, Options{Terminal: terminal}).Acquire(context.Background(), secrets.Declaration{Name: "fixture", Type: test.kind, Source: "contextStore"}, test.input)
			assertFailureCode(t, err, "secret.input")
		})
	}
	for _, kind := range []string{"opaque", "dockerConfigJson"} {
		t.Run(kind, func(t *testing.T) {
			terminal := &fakeTerminal{interactive: true, line: "typed-secret\n"}
			input := &countingInput{}
			_, err := New(input, Options{Terminal: terminal}).Acquire(context.Background(), secrets.Declaration{Name: "fixture", Type: kind, Source: "contextStore"}, secrets.Input{ContextName: "lab", ValueStdin: true})
			found := diagnostics.Of(err)
			if !diagnostics.IsUsage(err) || len(found) != 1 || found[0].Code != "secret.input" || found[0].Object == nil || found[0].Object.Name != "fixture" ||
				found[0].Remediation != "pipe the value into bootwright secret set --context lab --name fixture --value-stdin, or use --value-file <path>" ||
				len(terminal.prompts) != 0 || input.reads != 0 {
				t.Fatalf("a %s value at a terminal: %+v after %d prompts and %d reads", kind, found, len(terminal.prompts), input.reads)
			}
		})
	}
	t.Run("not a terminal", func(t *testing.T) {
		terminal := &fakeTerminal{}
		input := &countingInput{value: "piped-secret\n"}
		value, err := New(input, Options{Terminal: terminal}).Acquire(context.Background(), secrets.Declaration{Name: "fixture", Type: "opaque", Source: "contextStore"}, secrets.Input{ValueStdin: true})
		if err != nil {
			t.Fatalf("Acquire: %+v", diagnostics.Of(err))
		}
		defer value.Clear()
		if got := requiredPart(t, value, secrets.ValuePart); string(got) != "piped-secret\n" || len(terminal.prompts) != 0 {
			t.Fatalf("piped %q after %d prompts", got, len(terminal.prompts))
		}
	})
	t.Run("uninspectable", func(t *testing.T) {
		terminal := &fakeTerminal{failure: errors.New("synthetic ioctl failure")}
		input := &countingInput{value: "piped-secret\n"}
		_, err := New(input, Options{Terminal: terminal}).Acquire(context.Background(), secrets.Declaration{Name: "fixture", Type: "token", Source: "contextStore"}, secrets.Input{ValueStdin: true})
		assertFailureCode(t, err, "secret.input")
		if diagnosticMessage(err) != "standard input could not be inspected" || input.reads != 0 {
			t.Fatalf("message = %q after %d reads", diagnosticMessage(err), input.reads)
		}
	})
}
