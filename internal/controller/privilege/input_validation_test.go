package privilege

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	listArguments     = []string{"-n", "-u", "#0", "-ll"}
	validateArguments = []string{"-u", "#0", "-v"}
)

// policyScript answers the sudo listing and the interactive validation the way
// sudo does for a rule that needs a password: -n -ll exits 1 until a credential
// is cached, and -v caches one after its prompt. Each answer returns the exit
// status and the listing written on success; every call's arguments are
// recorded in order, the elevated child's as "child".
type policyScript struct {
	list     func(call int) (int, []byte)
	validate func(ctx context.Context) int
	calls    [][]string
	child    *Command
	lists    int
}

func (s *policyScript) executor() executorFunc {
	return executorFunc(func(ctx context.Context, c Command) (int, error) {
		switch {
		case c.Elevated:
			s.calls = append(s.calls, []string{"child"})
			child := c
			s.child = &child
			return 0, nil
		case slices.Equal(c.Arguments, listArguments):
			s.calls = append(s.calls, c.Arguments)
			s.lists++
			code, report := s.list(s.lists)
			c.Output.Write(report)
			return code, nil
		case slices.Equal(c.Arguments, validateArguments):
			s.calls = append(s.calls, c.Arguments)
			return s.validate(ctx), nil
		}
		s.calls = append(s.calls, c.Arguments)
		return 0, nil
	})
}

func (s *policyScript) validations() int {
	count := 0
	for _, call := range s.calls {
		if slices.Equal(call, validateArguments) {
			count++
		}
	}
	return count
}

func waitForCancel(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

func stdinOptions(script *policyScript, interactive bool, input io.Reader) SudoOptions {
	return SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: script.executor(), Delay: delayFunc(waitForCancel),
		NonInteractive: !interactive, SecretStdin: "bmc", Input: input, Error: io.Discard}
}

var secretSet = []string{"secret", "set", "--name", "bmc", "--value-stdin"}

func refused(code int) func(int) (int, []byte) {
	return func(int) (int, []byte) { return code, nil }
}

func TestAStandardInputSecretIsReadOnlyAfterTheListingIsRead(t *testing.T) {
	script := &policyScript{
		list: func(call int) (int, []byte) {
			if call == 1 {
				return 1, nil
			}
			return 0, listing("env_reset", "", "")
		},
		validate: func(context.Context) int { return 0 },
	}
	input := strings.NewReader("value")
	options := stdinOptions(script, true, input)
	code, err := NewSupervisor(options).Run(context.Background(), secretSet)
	if code != 0 || err != nil {
		t.Fatalf("Run = %d, %v", code, err)
	}
	want := [][]string{listArguments, validateArguments, listArguments, {"child"}}
	if !slices.EqualFunc(script.calls, want, slices.Equal[[]string]) {
		t.Fatalf("calls = %q, want %q", script.calls, want)
	}
	if script.child == nil || script.child.Input != options.Input {
		t.Fatal("the elevated child did not receive the standard-input Secret")
	}
}

func TestAStandardInputSecretRefusesWhenSudoWillNotListItsPolicy(t *testing.T) {
	script := &policyScript{list: refused(1), validate: func(context.Context) int { t.Error("a noninteractive invocation validated"); return 0 }}
	code, err := NewSupervisor(stdinOptions(script, false, unreadableInput{t})).Run(context.Background(), secretSet)
	var unproven *InputUnproven
	if code != 1 || !errors.As(err, &unproven) || unproven.Secret != "bmc" || unproven.Validated {
		t.Fatalf("Run = %d, %v", code, err)
	}
	if script.child != nil || script.validations() != 0 {
		t.Fatalf("calls = %q", script.calls)
	}
}

func TestAStandardInputSecretRefusesWhenValidationLeavesTheListingUnreadable(t *testing.T) {
	for _, test := range []struct {
		name     string
		validate int
		lists    int
	}{
		{"validated", 0, 2},
		{"validation refused", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := &policyScript{list: refused(1), validate: func(context.Context) int { return test.validate }}
			code, err := NewSupervisor(stdinOptions(script, true, unreadableInput{t})).Run(context.Background(), secretSet)
			var unproven *InputUnproven
			if code != 1 || !errors.As(err, &unproven) || !unproven.Validated {
				t.Fatalf("Run = %d, %v", code, err)
			}
			if script.child != nil || script.validations() != 1 || script.lists != test.lists {
				t.Fatalf("calls = %q", script.calls)
			}
		})
	}
}

func TestAStandardInputSecretRefusesAnOverflowingListing(t *testing.T) {
	script := &policyScript{
		list: func(int) (int, []byte) {
			return 0, append(listing("env_reset", "", ""), bytes.Repeat([]byte("#"), 64*1024)...)
		},
		validate: func(context.Context) int { return 1 },
	}
	code, err := NewSupervisor(stdinOptions(script, false, unreadableInput{t})).Run(context.Background(), secretSet)
	var unproven *InputUnproven
	if code != 1 || !errors.As(err, &unproven) || script.child != nil {
		t.Fatalf("Run = %d, %v; calls = %q", code, err, script.calls)
	}
}

func TestAValidatedListingThatLogsInputStillRefuses(t *testing.T) {
	script := &policyScript{
		list: func(call int) (int, []byte) {
			if call == 1 {
				return 1, nil
			}
			return 0, listing("env_reset, log_stdin", "", "")
		},
		validate: func(context.Context) int { return 0 },
	}
	code, err := NewSupervisor(stdinOptions(script, true, unreadableInput{t})).Run(context.Background(), secretSet)
	var logged *InputLogged
	if code != 1 || !errors.As(err, &logged) || logged.Option != "log_stdin" || script.child != nil {
		t.Fatalf("Run = %d, %v; calls = %q", code, err, script.calls)
	}
}

func TestAnInvocationWithoutAStandardInputSecretNeverValidates(t *testing.T) {
	script := &policyScript{list: refused(1), validate: func(context.Context) int { return 0 }}
	options := stdinOptions(script, true, strings.NewReader(""))
	options.SecretStdin = ""
	code, err := NewSupervisor(options).Run(context.Background(), []string{"status"})
	if code != 0 || err != nil || script.child == nil || script.validations() != 0 {
		t.Fatalf("Run = %d, %v; calls = %q", code, err, script.calls)
	}
}

func TestAnInterruptedValidationStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	script := &policyScript{list: refused(1), validate: func(context.Context) int { cancel(); return 1 }}
	code, err := NewSupervisor(stdinOptions(script, true, unreadableInput{t})).Run(ctx, secretSet)
	if code != 1 || !errors.Is(err, context.Canceled) || script.child != nil || script.lists != 1 {
		t.Fatalf("Run = %d, %v; calls = %q", code, err, script.calls)
	}
}

func TestElevationRefusesAnUnprovenInputLoggingPolicy(t *testing.T) {
	for _, test := range []struct {
		name          string
		json          bool
		terminal      bool
		wantsValidate bool
	}{
		{"json", true, false, true},
		{"interactive", false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := &policyScript{list: refused(1), validate: func(context.Context) int { return 0 }}
			elevator := scriptedElevator(scriptedChild{}, nil)
			elevator.Executor = script.executor()
			var stdout, stderr bytes.Buffer
			outcome := elevator.Run(context.Background(), Invocation{JSON: test.json, InputTerminal: test.terminal, Arguments: secretSet,
				Input: unreadableInput{t}, Output: &stdout, Error: &stderr, SecretStdin: "bmc"})
			d := outcome.Diagnostic
			if outcome.ExitCode != 1 || d == nil || d.Code != "secret.input" || !strings.Contains(d.Message, "Secret bmc") || !strings.Contains(d.Message, "nothing was read") ||
				!strings.Contains(d.Remediation, "--value-file <path>") || !strings.Contains(d.Remediation, "--password-file <path>") ||
				strings.Contains(d.Remediation, "sudo -v") != test.wantsValidate || d.Object == nil || d.Object.Kind != "Secret" || d.Object.Name != "bmc" {
				t.Fatalf("outcome = %d %+v", outcome.ExitCode, d)
			}
			if script.child != nil || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("calls %q, stdout %q, stderr %q", script.calls, stdout.String(), stderr.String())
			}
			if wantValidations := map[bool]int{true: 1, false: 0}[test.terminal]; script.validations() != wantValidations {
				t.Fatalf("calls = %q", script.calls)
			}
		})
	}
}
