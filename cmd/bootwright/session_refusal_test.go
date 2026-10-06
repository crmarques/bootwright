package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A refusal at the privilege boundary before an SSH session opens exits 255,
// the client's own failure status, so a status from 0 to 254 is always the
// remote command's. Every other command keeps 1, and an interrupt keeps 130.
func TestABoundaryRefusalOfASessionExitsTwoFiftyFive(t *testing.T) {
	refusal := diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "invoking account cannot be verified"}
	for _, test := range []struct {
		args       []string
		code, want int
		session    bool
	}{
		{args: []string{"machine", "exec", "--name", "rhel-01", "uptime"}, code: 1, want: 255, session: true},
		{args: []string{"machine", "rsh", "--name", "rhel-01"}, code: 1, want: 255, session: true},
		{args: []string{"machine", "exec", "--name", "rhel-01", "uptime"}, code: 130, want: 130, session: true},
		{args: []string{"machine", "stop", "--name", "rhel-01"}, code: 1, want: 1},
	} {
		classification := cli.ClassifyInvocation(test.args)
		if !classification.RequiresRoot || sessionCommand(classification) != test.session {
			t.Fatalf("%v classifies as %+v", test.args, classification)
		}
		var stdout, stderr bytes.Buffer
		code := classification.Diagnostic(&stdout, &stderr, refusal, refusedStatus(classification, test.code))
		if code != test.want || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "[FAIL] runtime.privilege: ") {
			t.Fatalf("%v refused with %d = %d, stdout %q, stderr %q; want %d", test.args, test.code, code, stdout.String(), stderr.String(), test.want)
		}
	}
}

type boundaryAccounts struct{ err error }

func (a boundaryAccounts) Resolve(context.Context) (privilege.Account, error) {
	return privilege.Account{}, a.err
}

// boundaryUnderTest crosses the privilege boundary as a non-root operator
// whose environment holds an unqualified proxy route, and records what the
// elevator was asked to relaunch.
func boundaryUnderTest(accounts error, outcome privilege.Outcome, elevated *[]privilege.Invocation) privilegeBoundary {
	environment := map[string]string{"HTTP_PROXY": "http://proxy.example:3128"}
	return privilegeBoundary{
		lookupEnv: func(name string) (string, bool) { value, ok := environment[name]; return value, ok },
		accounts:  boundaryAccounts{err: accounts},
		guard:     func(int) (func(), error) { return func() {}, nil },
		euid:      func() int { return 1000 },
		elevate: func(_ context.Context, invocation privilege.Invocation) privilege.Outcome {
			*elevated = append(*elevated, invocation)
			return outcome
		},
	}
}

// Every non-root operator crosses into root through this boundary, so a
// session's refusals before the remote command runs exit 255, whether the
// account, the elevator or sudo refused, and only a session's elevation lets
// the child's status stand over an interrupt. Every other command keeps 1.
func TestTheBoundaryGivesASessionItsOwnStatus(t *testing.T) {
	exec := []string{"machine", "exec", "--name", "rhel-01", "uptime"}
	rsh := []string{"machine", "rsh", "--name", "rhel-01"}
	stop := []string{"machine", "stop", "--name", "rhel-01"}
	setup := []string{"setup"}
	unverified := errors.New("no account")
	sudoRefused := privilege.Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "sudo is unavailable; run Bootwright as root"}}
	interrupted := privilege.Outcome{ExitCode: 130, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.interrupted", Message: "operation interrupted"}}
	for _, test := range []struct {
		name     string
		args     []string
		accounts error
		outcome  privilege.Outcome
		want     int
		elevated bool
		code     string
	}{
		{name: "exec account refused", args: exec, accounts: unverified, want: 255, code: "runtime.privilege"},
		{name: "rsh account refused", args: rsh, accounts: unverified, want: 255, code: "runtime.privilege"},
		{name: "stop account refused", args: stop, accounts: unverified, want: 1, code: "runtime.privilege"},
		{name: "exec elevator refused", args: exec, outcome: sudoRefused, want: 255, elevated: true, code: "runtime.privilege"},
		{name: "rsh elevator refused", args: rsh, outcome: sudoRefused, want: 255, elevated: true, code: "runtime.privilege"},
		{name: "stop elevator refused", args: stop, outcome: sudoRefused, want: 1, elevated: true, code: "runtime.privilege"},
		{name: "exec interrupted", args: exec, outcome: interrupted, want: 130, elevated: true, code: "runtime.interrupted"},
		{name: "exec remote status", args: exec, outcome: privilege.Outcome{ExitCode: 1}, want: 1, elevated: true},
		{name: "setup route refused", args: setup, want: 1, code: "controller.unsupported"},
	} {
		var elevated []privilege.Invocation
		var stdout, stderr bytes.Buffer
		code := boundaryUnderTest(test.accounts, test.outcome, &elevated).run(context.Background(), test.args, &stdout, &stderr)
		if code != test.want || stdout.Len() != 0 || (len(elevated) == 1) != test.elevated {
			t.Fatalf("%s: exit %d, elevated %d times, stdout %q, stderr %q; want %d", test.name, code, len(elevated), stdout.String(), stderr.String(), test.want)
		}
		if reported := strings.HasPrefix(stderr.String(), "[FAIL] "+test.code+": "); reported != (test.code != "") {
			t.Fatalf("%s: stderr %q, want the %q refusal", test.name, stderr.String(), test.code)
		}
		if test.elevated && elevated[0].Session != (test.args[1] != "stop") {
			t.Fatalf("%s: the elevator relaunched %v with Session %t", test.name, elevated[0].Arguments, elevated[0].Session)
		}
	}
}
