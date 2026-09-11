package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func assertConfirmationRefusal(t *testing.T, err error) {
	t.Helper()
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.state" {
		t.Fatalf("confirmation refusal was not typed: %v", err)
	}
}

func TestConfirmationConstructorIsInertAndAnswerReadIsBounded(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "YeS\r\n", " \tY \n", strings.Repeat(" ", 62) + "y\n"} {
		t.Run(answer, func(t *testing.T) {
			input := strings.NewReader(answer + "unread\n")
			reads, checks, bytesRead := 0, 0, 0
			var out bytes.Buffer
			ctx := context.WithValue(context.Background(), dispatchContextKey{}, "confirmation")
			confirmation := NewConfirmation(func(got context.Context, p []byte) (int, error) {
				if got != ctx || len(p) != 1 {
					t.Fatal("incorrect confirmation read capability", got, len(p))
				}
				reads++
				n, err := input.Read(p)
				bytesRead += n
				return n, err
			}, &out, func() (bool, error) { checks++; return true, nil })
			if reads != 0 || checks != 0 || out.Len() != 0 {
				t.Fatal("constructor performed effects")
			}
			if err := confirmation.Confirm(ctx, "update", "example"); err != nil {
				t.Fatal(err)
			}
			if checks != 1 || bytesRead != len(answer) || bytesRead > 64 || !strings.Contains(out.String(), "update") || !strings.Contains(out.String(), "example") || input.Len() != len("unread\n") {
				t.Fatal("confirmation exceeded authority", checks, bytesRead, out.String(), input.Len())
			}
		})
	}
}

func TestConfirmationRejectsUnsafeDeclinedAndOversizedAnswers(t *testing.T) {
	for _, answer := range []string{"n\n", "no\n", "\n", "yes please\n", "yeſ\n", "ｙ\n", "y\x00\n", "y\x1b[0m\n", "yes", "", strings.Repeat(" ", 63) + "y\n", strings.Repeat("y", 4096) + "\n"} {
		t.Run(answer[:min(len(answer), 20)], func(t *testing.T) {
			input := strings.NewReader(answer)
			bytesRead := 0
			confirmation := NewConfirmation(func(_ context.Context, p []byte) (int, error) { n, err := input.Read(p); bytesRead += n; return n, err }, io.Discard, func() (bool, error) { return true, nil })
			assertConfirmationRefusal(t, confirmation.Confirm(context.Background(), "delete", "example"))
			if bytesRead > 64 {
				t.Fatal("answer bound exceeded", bytesRead)
			}
		})
	}
}

func TestConfirmationRefusesWithoutReadingWhenNotInteractiveOrPromptFails(t *testing.T) {
	for _, tc := range []struct {
		terminal    bool
		terminalErr error
		out         io.Writer
	}{
		{false, nil, io.Discard}, {true, errors.New("private terminal failure"), io.Discard},
		{true, nil, rejectingWriter{}}, {true, nil, rejectingWriter{short: true}},
	} {
		reads := 0
		confirmation := NewConfirmation(func(context.Context, []byte) (int, error) { reads++; return 0, errors.New("unexpected read") }, tc.out, func() (bool, error) { return tc.terminal, tc.terminalErr })
		err := confirmation.Confirm(context.Background(), "delete", "example")
		assertConfirmationRefusal(t, err)
		if reads != 0 || strings.Contains(err.Error(), "private terminal failure") {
			t.Fatal("unsafe confirmation effect or disclosure", reads, err)
		}
	}
	for _, confirmation := range []*Confirmation{nil, NewConfirmation(nil, nil, nil)} {
		assertConfirmationRefusal(t, confirmation.Confirm(context.Background(), "delete", "example"))
	}
}

func TestConfirmationRefusesReadFailuresAndIncompleteReads(t *testing.T) {
	for _, read := range []func(context.Context, []byte) (int, error){
		func(context.Context, []byte) (int, error) { return 0, nil },
		func(context.Context, []byte) (int, error) { return 0, errors.New("private input failure") },
		func(context.Context, []byte) (int, error) { return -1, nil },
		func(context.Context, []byte) (int, error) { return 2, nil },
	} {
		err := NewConfirmation(read, io.Discard, func() (bool, error) { return true, nil }).Confirm(context.Background(), "update", "example")
		assertConfirmationRefusal(t, err)
		if strings.Contains(err.Error(), "private input failure") {
			t.Fatal("read failure disclosed input", err)
		}
	}
}

func TestConfirmationCancellationAtEveryCallbackBoundary(t *testing.T) {
	for _, phase := range []string{"before", "terminal", "prompt", "read", "accepted newline"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checks, reads := 0, 0
			input := strings.NewReader("yes\n")
			var output bytes.Buffer
			writer := callbackWriter{write: func(p []byte) (int, error) {
				if phase == "prompt" {
					cancel()
				}
				return output.Write(p)
			}}
			confirmation := NewConfirmation(func(ctx context.Context, p []byte) (int, error) {
				reads++
				n, err := input.Read(p)
				if phase == "read" || phase == "accepted newline" && n == 1 && p[0] == '\n' {
					cancel()
				}
				return n, err
			}, writer, func() (bool, error) {
				checks++
				if phase == "terminal" {
					cancel()
				}
				return true, nil
			})
			if phase == "before" {
				cancel()
			}
			assertConfirmationRefusal(t, confirmation.Confirm(ctx, "update", "example"))
			if phase == "before" && (checks != 0 || reads != 0 || output.Len() != 0) {
				t.Fatal("canceled confirmation performed effects")
			}
			if phase == "terminal" && (reads != 0 || output.Len() != 0) {
				t.Fatal("terminal cancellation continued")
			}
			if phase == "prompt" && reads != 0 {
				t.Fatal("prompt cancellation read input")
			}
		})
	}
}

type callbackWriter struct{ write func([]byte) (int, error) }

func (w callbackWriter) Write(p []byte) (int, error) { return w.write(p) }

func TestConfirmationPromptEscapesUntrustedIdentityOnce(t *testing.T) {
	raw := "<example>\\path\n\x1b\xff"
	input := strings.NewReader("n\n")
	var output bytes.Buffer
	confirmation := NewConfirmation(func(_ context.Context, p []byte) (int, error) { return input.Read(p) }, &output, func() (bool, error) { return true, nil })
	assertConfirmationRefusal(t, confirmation.Confirm(context.Background(), raw, raw))
	want := "Confirm " + escapeDisplayLine(raw) + " for context " + escapeDisplayLine(raw) + "? [y/N] "
	if output.String() != want {
		t.Fatalf("unsafe prompt: %q", output.String())
	}
}
