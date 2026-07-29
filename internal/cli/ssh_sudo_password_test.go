package cli

import (
	"strings"
	"testing"
)

func TestSSHUserForProvisionedWithoutAnAccountIsRefused(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "--ssh-user-for-provisioned", "version")
	if code == 0 {
		t.Fatalf("--ssh-user-for-provisioned without --ssh-user unexpectedly succeeded:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "--ssh-user-for-provisioned") {
		t.Fatalf("refusal did not name the flag, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSSHUserForProvisionedWithAnAccountIsAccepted(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "--ssh-user-for-provisioned", "--ssh-user", "carmj", "version")
	if code != 0 {
		t.Fatalf("--ssh-user-for-provisioned with --ssh-user failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestAskSudoPasswordRefusesAnEmptyAnswer(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLIWithInput(t, "\n", "--ssh-ask-sudo-password", "version")
	if code == 0 {
		t.Fatalf("an empty sudo password unexpectedly succeeded:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "--ssh-ask-sudo-password") {
		t.Fatalf("refusal did not name the flag, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestAskSudoPasswordPromptsOnStderrBeforeTheCommandRuns(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLIWithInput(t, "hunter2\n", "--ssh-ask-sudo-password", "version")
	if code != 0 {
		t.Fatalf("version with a prompted password failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, sshSudoPasswordPrompt) {
		t.Fatalf("stderr = %q, want the prompt; it must not go to stdout, which callers parse", stderr)
	}
	if strings.Contains(stdout+stderr, "hunter2") {
		t.Fatalf("the password was echoed back (stdout=%q stderr=%q)", stdout, stderr)
	}
}
