package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestAskSudoPasswordPromptNamesTheAccountItAnswersFor(t *testing.T) {
	setTestHomeAndRoot(t)
	withoutControllingTTY(t)
	stdout, stderr, code := runCLIWithInput(t, "hunter2\n", "--ssh-user", "operator", "--ssh-ask-sudo-password", "version")
	if code != 0 {
		t.Fatalf("version with a prompted password failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, `SSH sudo password for "operator": `) {
		t.Fatalf("stderr = %q, want a prompt naming the account, so a caller answering two sudo prompts can tell them apart", stderr)
	}
}

func TestSSHSudoPasswordReusesTheAnswerTheCallerAlreadyGaveForTheirOwnAccount(t *testing.T) {
	seedInheritedCallerSudoPassword(t, "hunter2")
	useCallerAccount(t, "carmj")
	askSSHSudoPassword(t)

	var prompt bytes.Buffer
	password, err := resolveSSHSudoPassword(strings.NewReader(""), &prompt, "carmj")
	if err != nil {
		t.Fatalf("resolveSSHSudoPassword: %v", err)
	}
	if password != "hunter2" {
		t.Fatalf("password = %q, want the answer the local root gate already collected", password)
	}
	if strings.Contains(prompt.String(), sshSudoPasswordPrompt) {
		t.Fatalf("prompt = %q, want no second prompt for a password the caller already typed this run", prompt.String())
	}
	if !strings.Contains(prompt.String(), `sudo as "carmj"`) {
		t.Fatalf("prompt = %q, want the reuse to be stated; a password sent to remote sudo is never silent", prompt.String())
	}
	if strings.Contains(prompt.String(), "hunter2") {
		t.Fatalf("the reused password was echoed back: %q", prompt.String())
	}
}

func TestSSHSudoPasswordStillPromptsForAnAccountThatIsNotTheCaller(t *testing.T) {
	seedInheritedCallerSudoPassword(t, "hunter2")
	useCallerAccount(t, "carmj")
	askSSHSudoPassword(t)

	var prompt bytes.Buffer
	password, err := resolveSSHSudoPassword(strings.NewReader("s3cret\n"), &prompt, "operator")
	if err != nil {
		t.Fatalf("resolveSSHSudoPassword: %v", err)
	}
	if password != "s3cret" {
		t.Fatalf("password = %q, want the prompted answer; the caller's own password belongs to another account", password)
	}
	if !strings.Contains(prompt.String(), `SSH sudo password for "operator": `) {
		t.Fatalf("prompt = %q, want a prompt for the account that is not the caller", prompt.String())
	}
}

func TestSSHSudoPasswordStillPromptsWhenNoAccountIsNamed(t *testing.T) {
	seedInheritedCallerSudoPassword(t, "hunter2")
	useCallerAccount(t, "carmj")
	askSSHSudoPassword(t)

	var prompt bytes.Buffer
	password, err := resolveSSHSudoPassword(strings.NewReader("s3cret\n"), &prompt, "")
	if err != nil {
		t.Fatalf("resolveSSHSudoPassword: %v", err)
	}
	if password != "s3cret" {
		t.Fatalf("password = %q, want the prompted answer; without --ssh-user the declared login is unknown", password)
	}
	if !strings.Contains(prompt.String(), sshSudoPasswordPrompt) {
		t.Fatalf("prompt = %q, want the unqualified prompt", prompt.String())
	}
}

func TestSSHSudoPasswordPromptsWhenTheLocalRootGateNeverAsked(t *testing.T) {
	useCallerAccount(t, "carmj")
	askSSHSudoPassword(t)

	var prompt bytes.Buffer
	password, err := resolveSSHSudoPassword(strings.NewReader("s3cret\n"), &prompt, "carmj")
	if err != nil {
		t.Fatalf("resolveSSHSudoPassword: %v", err)
	}
	if password != "s3cret" {
		t.Fatalf("password = %q, want the prompted answer; passwordless local sudo collects nothing to reuse", password)
	}
}

func TestArgsAskSSHSudoPassword(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"destroy", "--ssh-user", "carmj", "--ssh-ask-sudo-password", "--yes"}, true},
		{[]string{"--ssh-ask-sudo-password", "preflight", "all"}, true},
		{[]string{"preflight", "all", "--ssh-ask-sudo-password=true"}, true},
		{[]string{"preflight", "all", "--ssh-ask-sudo-password=false"}, false},
		{[]string{"preflight", "all"}, false},
		{[]string{"container-cluster", "oc", "--name", "hub", "--", "get", "--ssh-ask-sudo-password"}, false},
	}
	for _, tc := range tests {
		if got := argsAskSSHSudoPassword(tc.args); got != tc.want {
			t.Fatalf("argsAskSSHSudoPassword(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestArgsNeedCallerSudoPasswordCoversCommandsThatNeverUseBecome(t *testing.T) {
	if argsNeedCallerSudoPassword([]string{"preflight", "all"}) {
		t.Fatal("preflight collects no become password, so the caller's answer must not be written to a file it never reads")
	}
	if !argsNeedCallerSudoPassword([]string{"preflight", "all", "--ssh-ask-sudo-password"}) {
		t.Fatal("--ssh-ask-sudo-password must carry the caller's answer into the child, or the child prompts for it a second time")
	}
	if !argsNeedCallerSudoPassword([]string{"apply", "--yes"}) {
		t.Fatal("apply still needs the inherited become password")
	}
}

func seedInheritedCallerSudoPassword(t *testing.T, password string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "become-pass")
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(localRootSudoAuthEnv, localSudoAuthPrompted)
	t.Setenv(localRootBecomePasswordFileEnv, path)
}

func useCallerAccount(t *testing.T, name string) {
	t.Helper()
	previous := callerAccountName
	callerAccountName = func() string { return name }
	t.Cleanup(func() { callerAccountName = previous })
}

func askSSHSudoPassword(t *testing.T) {
	t.Helper()
	previous := sshAskSudoPassword
	sshAskSudoPassword = true
	t.Cleanup(func() { sshAskSudoPassword = previous })
	withoutControllingTTY(t)
}

func withoutControllingTTY(t *testing.T) {
	t.Helper()
	previous := openControllingTTY
	openControllingTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	t.Cleanup(func() { openControllingTTY = previous })
}
