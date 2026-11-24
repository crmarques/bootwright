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
	t.Parallel()
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
		if code != test.want || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "  error  runtime.privilege\n    ") {
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
	t.Parallel()
	exec := []string{"machine", "exec", "--name", "rhel-01", "uptime"}
	rsh := []string{"machine", "rsh", "--name", "rhel-01"}
	stop := []string{"machine", "stop", "--name", "rhel-01"}
	setup := []string{"install"}
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
		if reported := strings.HasPrefix(stderr.String(), "  error  "+test.code+"\n"); reported != (test.code != "") {
			t.Fatalf("%s: stderr %q, want the %q refusal", test.name, stderr.String(), test.code)
		}
		if test.elevated && elevated[0].Session != (test.args[1] != "stop") {
			t.Fatalf("%s: the elevator relaunched %v with Session %t", test.name, elevated[0].Arguments, elevated[0].Session)
		}
	}
}

func TestExplicitSSHPasswordUsesTheSudoInputGuard(t *testing.T) {
	args := []string{"machine", "rsh", "--name", "node", "--ssh-user", "ops", "--ssh-ask-password"}
	var elevated []privilege.Invocation
	var stdout, stderr bytes.Buffer
	boundary := boundaryUnderTest(nil, privilege.Outcome{}, &elevated)
	if status := boundary.run(context.Background(), args, &stdout, &stderr); status != 0 ||
		len(elevated) != 1 || elevated[0].SecretStdin != privilege.SSHPasswordInput {
		t.Fatalf("password input did not reach the sudo guard: status %d, %v, %s", status, elevated, &stderr)
	}
	boundary.euid = func() int { return 0 }
	boundary.lookupEnv = func(name string) (string, bool) {
		if name == "SUDO_COMMAND" {
			return "/usr/bin/bootwright machine rsh", true
		}
		return "", false
	}
	stderr.Reset()
	if status := boundary.run(context.Background(), args, &stdout, &stderr); status != 255 ||
		len(elevated) != 1 || !strings.Contains(stderr.String(), "input logging policy was not verified") {
		t.Fatalf("manual sudo did not refuse the password prompt: status %d, %s", status, &stderr)
	}
}

// Every reveal reaches the sudo output-logging guard, and a reveal from a root
// session sudo started, whose policy Bootwright cannot list, refuses before
// any custody read; a command that reveals nothing is not affected.
func TestARevealUsesTheSudoOutputGuard(t *testing.T) {
	for _, test := range []struct {
		args   []string
		reveal bool
	}{
		{[]string{"secret", "show", "--name", "bmc", "--part", "password"}, true},
		{[]string{"cluster", "kubeconfig", "--name", "ocp"}, true},
		{[]string{"cluster", "keyring", "--name", "storage", "--client", "vm-disks"}, true},
		{[]string{"cluster", "info", "--name", "ocp", "--secrets"}, true},
		{[]string{"cluster", "info", "--name", "storage", "--object-user", "backup"}, true},
		{[]string{"cluster", "info", "--name", "ocp"}, false},
		{[]string{"secret", "list"}, false},
	} {
		var elevated []privilege.Invocation
		var stdout, stderr bytes.Buffer
		boundary := boundaryUnderTest(nil, privilege.Outcome{}, &elevated)
		if status := boundary.run(context.Background(), test.args, &stdout, &stderr); status != 0 || len(elevated) != 1 || elevated[0].SecretOutput != test.reveal {
			t.Fatalf("%v: status %d, elevated %+v, %s", test.args, status, elevated, &stderr)
		}
		if !test.reveal {
			continue
		}
		boundary.euid = func() int { return 0 }
		boundary.lookupEnv = func(name string) (string, bool) {
			if name == "SUDO_COMMAND" {
				return "/usr/bin/bootwright " + strings.Join(test.args, " "), true
			}
			return "", false
		}
		stdout.Reset()
		stderr.Reset()
		if status := boundary.run(context.Background(), test.args, &stdout, &stderr); status != 1 || len(elevated) != 1 || stdout.Len() != 0 ||
			!strings.Contains(stderr.String(), "secret.output") || !strings.Contains(stderr.String(), "root login session sudo did not start") {
			t.Fatalf("%v: a sudo-started root reveal = status %d, stdout %q, stderr %s", test.args, status, &stdout, &stderr)
		}
	}
}

// A root session with a sudo ancestor that is not the Bootwright supervisor is
// sudo-started even when its environment names no SUDO_COMMAND, as under
// sudo su -: it refuses a reveal, a standard-input Secret or media URL, and an
// SSH password prompt before anything is read, each with its own remedy, the
// first three also naming a root login sudo did not start; a
// root login sudo did not start is not refused.
func TestASudoAncestorRefusesUnprovedInputAndOutput(t *testing.T) {
	for _, test := range []struct {
		args         []string
		code, status int
		want         string
	}{
		{[]string{"secret", "show", "--name", "bmc", "--part", "password"}, 0, 1, "secret.output"},
		{[]string{"secret", "set", "--name", "bmc", "--value-stdin"}, 0, 1, "--value-file <path>"},
		{[]string{"secret", "set", "--name", "bmc", "--username", "admin", "--password-stdin"}, 0, 1, "--password-file <path>"},
		{[]string{"catalog", "media", "add", "--name", "rhel.iso", "--from-url-file", "-", "--sha256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, 0, 1, "--from-url-file <path>"},
		{[]string{"machine", "rsh", "--name", "node", "--ssh-user", "ops", "--ssh-ask-password"}, 0, 255, "--ssh-id-file"},
	} {
		for _, ancestor := range []bool{true, false} {
			var elevated []privilege.Invocation
			var stdout, stderr bytes.Buffer
			boundary := boundaryUnderTest(errors.New("accounts were resolved"), privilege.Outcome{}, &elevated)
			boundary.euid = func() int { return 0 }
			boundary.lookupEnv = func(string) (string, bool) { return "", false }
			boundary.directRoot = func() bool { return !ancestor }
			status := boundary.run(context.Background(), test.args, &stdout, &stderr)
			refused := strings.Contains(stderr.String(), test.want) && (test.status == 255 || strings.Contains(stderr.String(), "root login session sudo did not start"))
			if ancestor && (status != test.status || !refused || len(elevated) != 0) || !ancestor && refused {
				t.Fatalf("%v (sudo ancestor %v): status %d, stderr %s", test.args, ancestor, status, &stderr)
			}
		}
	}
}

func TestDelegatedEntryRefusesAdministrativeCommandsAndUnprivilegedCallers(t *testing.T) {
	for _, test := range []struct {
		name        string
		args        []string
		uid, status int
	}{
		{"policy install", []string{"policy", "install", "--context", "lab", "--file", "policy.yaml"}, 0, 1},
		{"read-only command", []string{"version"}, 0, 1},
		{"unprivileged session", []string{"machine", "exec", "--name", "node", "uptime"}, 1001, 255},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			boundary := privilegeBoundary{euid: func() int { return test.uid }}
			args := append([]string{delegatedSessionEntry}, test.args...)
			if status := boundary.run(t.Context(), args, &stdout, &stderr); status != test.status ||
				stdout.Len() != 0 || !strings.Contains(stderr.String(), "error  access.denied") {
				t.Fatalf("exit %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
			}
		})
	}
}

// A root invocation sudo started refuses a foreign variable before it resolves
// an account or runs anything, whether or not its command needs root.
func TestASudoStartedRootInvocationRefusesAForeignVariableFirst(t *testing.T) {
	t.Parallel()
	foreign := &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "this root invocation was given variable GODEBUG"}
	for _, args := range [][]string{{"machine", "stop", "--name", "rhel-01"}, {"version"}, {"--help"}, {"completion", "bash"}, {strings.Repeat("x", 16385)}} {
		var elevated []privilege.Invocation
		var stdout, stderr bytes.Buffer
		boundary := boundaryUnderTest(errors.New("accounts were resolved"), privilege.Outcome{}, &elevated)
		boundary.euid = func() int { return 0 }
		checked := 0
		boundary.sudoEnviron = func() *diagnostics.Diagnostic { checked++; return foreign }
		code := boundary.run(context.Background(), args, &stdout, &stderr)
		if code != 1 || checked != 1 || len(elevated) != 0 || !strings.Contains(stderr.String(), "variable GODEBUG") {
			t.Fatalf("%v: exit %d, checked %d, elevated %d, stderr %q", args, code, checked, len(elevated), stderr.String())
		}
	}
}
