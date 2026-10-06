package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
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

// The prompt names the context that would trust the key, and its refusal names
// the command that records it in that context.
func TestTheHostKeyPromptNamesTheContext(t *testing.T) {
	var out bytes.Buffer
	confirmation := NewConfirmation(func(_ context.Context, p []byte) (int, error) { p[0] = '\n'; return 1, nil },
		&out, func() (bool, error) { return true, nil })
	err := confirmation.ConfirmHostKey(context.Background(), "lab", "rhel-01", "198.51.100.11", "ssh-ed25519", "SHA256:aaa")
	if out.String() != "Trust ssh-ed25519 SHA256:aaa for Machine rhel-01 at 198.51.100.11 in context lab? [y/N] " {
		t.Fatalf("prompt = %q", out.String())
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "trust.identity" {
		t.Fatalf("a declined prompt = %+v (%v)", reported, err)
	}
	unattended := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected read") },
		io.Discard, func() (bool, error) { return false, nil })
	err = unattended.ConfirmHostKey(context.Background(), "lab", "rhel-01", "198.51.100.11", "ssh-ed25519", "SHA256:aaa")
	if reported := diagnostics.Of(err); len(reported) != 1 ||
		reported[0].Remediation != "record it with bootwright machine trust --context lab --machines rhel-01" {
		t.Fatalf("refusal = %+v", reported)
	}
}

// Each consumer's confirmation names what it acts on and, inside a context,
// that context; a refusal keeps the consumer's own code, says why in its
// message and puts the command it confirms, repeated with --yes, in its remedy.
func TestEachConfirmationNamesItsObjectAndContextAndKeepsItsConsumersCode(t *testing.T) {
	secret := func(name string) *diagnostics.ObjectIdentity {
		return &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Secret", Name: name}
	}
	for _, tc := range []struct {
		action, object, context, prompt, code, label, remedy string
		args                                                 []string
		identity                                             *diagnostics.ObjectIdentity
	}{
		{action: "setup", object: "this host", prompt: "Confirm controller setup on this host? [y/N] ", args: []string{"setup"},
			code: "controller.setup", label: "setup", remedy: "review the plan, then repeat bootwright setup with --yes"},
		{action: "media replace", object: "rhel.iso", prompt: "Confirm replace of stored media rhel.iso? [y/N] ",
			args: []string{"media", "add", "--name", "rhel.iso", "--from-file", "/srv/rhel.iso"},
			code: "media.store", label: "media", remedy: "review it, then repeat bootwright media add --from-file /srv/rhel.iso --name rhel.iso with --yes"},
		{action: "media delete", object: "rhel.iso", prompt: "Confirm delete of stored media rhel.iso? [y/N] ", args: []string{"media", "delete", "--name", "rhel.iso"},
			code: "media.store", label: "media", remedy: "review it, then repeat bootwright media delete --name rhel.iso with --yes"},
		{action: "apply", object: "lab", prompt: "Confirm apply for context lab? [y/N] ", args: []string{"apply", "--context", "lab"},
			code: "lifecycle.state", label: "apply", remedy: "review the plan, then repeat bootwright apply --context lab with --yes"},
		{action: "destroy", object: "lab", prompt: "Confirm destroy for context lab? [y/N] ", args: []string{"destroy"},
			code: "lifecycle.state", label: "destroy", remedy: "review the plan, then repeat bootwright destroy --context lab with --yes"},
		{action: "trust", object: "lab", prompt: "Confirm trust for context lab? [y/N] ", args: []string{"machine", "trust", "--context", "lab"},
			code: "trust.identity", label: "trust", remedy: "review the plan, then repeat bootwright machine trust --context lab with --yes"},
		{action: "rotate secret encryption", object: "lab", prompt: "Confirm rotation of the secret encryption key of context lab? [y/N] ",
			args: []string{"secret", "encryption", "rotate"},
			code: "secret.store.conflict", label: "key rotation", remedy: "review it, then repeat bootwright secret encryption rotate --context lab with --yes"},
		{action: "update", object: "lab", prompt: "Confirm update for context lab? [y/N] ", args: []string{"context", "update", "--name", "lab", "--input-dir", "infra"},
			code: "context.state", label: "context", remedy: "review it, then repeat bootwright context update --input-dir infra --name lab with --yes"},
		{action: "replace secret", object: "token", context: "lab", prompt: "Confirm replacement of secret token in context lab? [y/N] ",
			args: []string{"secret", "set", "--name", "token", "--value-file", "token.txt"},
			code: "secret.store.conflict", label: "secret replacement", identity: secret("token"),
			remedy: "review it with bootwright secret check --context lab, then repeat bootwright secret set --context lab --name token --value-file token.txt with --yes"},
		{action: "delete secret", object: "token", context: "lab", prompt: "Confirm deletion of secret token in context lab? [y/N] ",
			args: []string{"secret", "delete", "--name", "token"},
			code: "secret.store.conflict", label: "secret deletion", identity: secret("token"),
			remedy: "review it with bootwright secret check --context lab, then repeat bootwright secret delete --context lab --name token with --yes"},
		{action: "stop machine", object: "rhel-01", context: "lab", prompt: "Confirm stop of machine rhel-01 in context lab? [y/N] ",
			args: []string{"machine", "stop", "--name", "rhel-01"},
			code: "machine.power", label: "power", remedy: "review it, then repeat bootwright machine stop --context lab --name rhel-01 with --yes"},
		{action: "restart machine", object: "rhel-01", context: "lab", prompt: "Confirm restart of machine rhel-01 in context lab? [y/N] ",
			args: []string{"machine", "restart", "--name", "rhel-01"},
			code: "machine.power", label: "power", remedy: "review it, then repeat bootwright machine restart --context lab --name rhel-01 with --yes"},
		{action: "force stop machine", object: "rhel-01", context: "lab", prompt: "Confirm force stop of machine rhel-01 in context lab? [y/N] ",
			args: []string{"machine", "stop", "--name", "rhel-01", "--force"},
			code: "machine.power", label: "power", remedy: "review it, then repeat bootwright machine stop --context lab --force --name rhel-01 with --yes"},
		{action: "force restart machine", object: "rhel-01", context: "lab", prompt: "Confirm force restart of machine rhel-01 in context lab? [y/N] ",
			args: []string{"machine", "restart", "--force", "--name", "rhel-01"},
			code: "machine.power", label: "power", remedy: "review it, then repeat bootwright machine restart --context lab --force --name rhel-01 with --yes"},
	} {
		for _, answer := range []struct {
			name, prompt, reason string
			terminal             bool
			read                 func(*strings.Reader, []byte) (int, error)
		}{
			{name: "declined", prompt: tc.prompt, reason: "was declined; nothing changed", terminal: true,
				read: func(input *strings.Reader, p []byte) (int, error) { return input.Read(p) }},
			{name: "not a terminal", reason: "requires an interactive terminal",
				read: func(*strings.Reader, []byte) (int, error) { return 0, errors.New("unexpected read") }},
			{name: "unreadable", prompt: tc.prompt, reason: "answer could not be read", terminal: true,
				read: func(*strings.Reader, []byte) (int, error) { return 0, errors.New("private input failure") }},
		} {
			t.Run(tc.action+"/"+answer.name, func(t *testing.T) {
				input := strings.NewReader("n\n")
				var out bytes.Buffer
				confirmation := NewConfirmation(func(_ context.Context, p []byte) (int, error) { return answer.read(input, p) },
					&out, func() (bool, error) { return answer.terminal, nil }).Repeating(tc.args)
				var err error
				if tc.context == "" {
					err = confirmation.Confirm(context.Background(), tc.action, tc.object)
				} else {
					err = confirmation.ConfirmIn(context.Background(), tc.action, tc.object, tc.context)
				}
				if out.String() != answer.prompt {
					t.Fatalf("prompt = %q, want %q", out.String(), answer.prompt)
				}
				want := []diagnostics.Diagnostic{{Severity: "error", Code: tc.code, Message: tc.label + " confirmation " + answer.reason,
					Object: tc.identity, Remediation: tc.remedy}}
				if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
					t.Fatalf("refusal = %+v, want %+v", reported, want)
				}
				if !strings.Contains(tc.remedy, " --yes") || tc.context != "" && !strings.Contains(tc.remedy, "--context "+tc.context) {
					t.Fatalf("the remedy %q does not repeat the command in its context with --yes", tc.remedy)
				}
			})
		}
	}
}

// Following a refusal's remedy runs exactly the command the operator ran, with
// --yes: every selection, authorization, source and choice it made stays, so
// the repeated command never does more than the plan they reviewed. A value
// output never repeats is named by a placeholder, a value a shell would split
// is quoted, and a confirmer that was not given the invocation names the same
// command rather than a shorter one.
func TestARefusedConfirmationRepeatsTheExactInvocation(t *testing.T) {
	for _, tc := range []struct {
		name, action, object, context string
		args                          []string
		remedy                        string
	}{
		{name: "an apply's stage selection and authorization", action: "apply", object: "lab",
			args:   []string{"apply", "--stage", "infra-components", "--authorize", "data-loss"},
			remedy: "review the plan, then repeat bootwright apply --context lab --authorize data-loss --stage infra-components with --yes"},
		{name: "a destroy's authorization", action: "destroy", object: "lab",
			args:   []string{"--context", "lab", "destroy", "--authorize", "data-loss"},
			remedy: "review the plan, then repeat bootwright destroy --context lab --authorize data-loss with --yes"},
		{name: "a trust selection and replacement", action: "trust", object: "lab",
			args:   []string{"machine", "trust", "--machines", "rhel-01", "--replace", "rhel-01", "--output", "json"},
			remedy: "review the plan, then repeat bootwright machine trust --context lab --machines rhel-01 --output json --replace rhel-01 with --yes"},
		{name: "a setup that retires old bundles", action: "setup", object: "this host",
			args:   []string{"setup", "--purge-old-bundles"},
			remedy: "review the plan, then repeat bootwright setup --purge-old-bundles with --yes"},
		{name: "a media download keeps its digest and hides its URL", action: "media replace", object: "rhel.iso",
			args:   []string{"media", "add", "--name", "rhel.iso", "--from-url", "https://mirror.example.test/rhel.iso?token=abc", "--sha256", strings.Repeat("a", 64)},
			remedy: "review it, then repeat bootwright media add --from-url <from-url> --name rhel.iso --sha256 " + strings.Repeat("a", 64) + " with --yes"},
		{name: "a media path a shell would split", action: "media replace", object: "rhel.iso",
			args:   []string{"media", "add", "--name", "rhel.iso", "--from-file", "/srv/it's here.iso"},
			remedy: `review it, then repeat bootwright media add --from-file '/srv/it'\''s here.iso' --name rhel.iso with --yes`},
		{name: "a secret's username stays undisclosed", action: "replace secret", object: "login", context: "lab",
			args:   []string{"secret", "set", "--name", "login", "--username", "alice", "--password-file", "password.txt"},
			remedy: "review it with bootwright secret check --context lab, then repeat bootwright secret set --context lab --name login --password-file password.txt --username <username> with --yes"},
		{name: "a context deletion's acknowledgements", action: "delete", object: "lab",
			args:   []string{"context", "delete", "--name", "lab", "--purge", "--allow-orphans"},
			remedy: "review it, then repeat bootwright context delete --allow-orphans --name lab --purge with --yes"},
		{name: "no invocation", action: "apply", object: "lab",
			remedy: "review the plan, then repeat the same command with --yes"},
		{name: "an invocation that names no command", action: "apply", object: "lab", args: []string{"no-such-command"},
			remedy: "review the plan, then repeat the same command with --yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			confirmation := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected read") },
				io.Discard, func() (bool, error) { return false, nil }).Repeating(tc.args)
			var err error
			if tc.context == "" {
				err = confirmation.Confirm(context.Background(), tc.action, tc.object)
			} else {
				err = confirmation.ConfirmIn(context.Background(), tc.action, tc.object, tc.context)
			}
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Remediation != tc.remedy {
				t.Fatalf("refusal = %+v, want the remedy %q", reported, tc.remedy)
			}
		})
	}
}

// A canceled confirmation names no remedy: nothing about the command was
// refused, and repeating it with --yes is not what the operator asked for.
func TestACanceledConfirmationKeepsItsCodeAndNamesNoRemedy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewConfirmation(func(context.Context, []byte) (int, error) { return 0, errors.New("unexpected read") }, io.Discard,
		func() (bool, error) { return true, nil }).ConfirmIn(ctx, "stop machine", "rhel-01", "lab")
	want := []diagnostics.Diagnostic{{Severity: "error", Code: "machine.power", Message: "power confirmation was canceled"}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("cancellation = %+v", reported)
	}
}
