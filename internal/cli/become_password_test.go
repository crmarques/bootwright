package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/host/become"
)

func TestPrepareBecomePasswordFileWritesRestrictedFile(t *testing.T) {
	var prompt bytes.Buffer
	path, cleanup, err := prepareBecomePasswordFile(strings.NewReader("secret\n"), &prompt)
	if err != nil {
		t.Fatalf("prepareBecomePasswordFile: %v", err)
	}
	defer cleanup()
	if prompt.String() != "BECOME password: " {
		t.Fatalf("prompt = %q, want BECOME password prompt", prompt.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read password file: %v", err)
	}
	if string(data) != "secret\n" {
		t.Fatalf("password file content = %q, want redacted one-line secret", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat password file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("password file mode = %03o, want 600", got)
	}
	cleanup()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup did not remove password file, stat err=%v", err)
	}
}

func TestPrepareBecomePasswordFileRejectsEmptyPassword(t *testing.T) {
	_, _, err := prepareBecomePasswordFile(strings.NewReader("\n"), io.Discard)
	if err == nil {
		t.Fatal("empty password should fail")
	}
}

func TestReadSudoPasswordEOFExplainsRootRequirement(t *testing.T) {
	old := openControllingTTY
	t.Cleanup(func() { openControllingTTY = old })
	openControllingTTY = func() (*os.File, error) { return nil, errors.New("no tty") }

	_, err := readSudoPassword(strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("EOF stdin should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "needs root to access /var/lib/bootwright") || !strings.Contains(msg, "passwordless sudo") {
		t.Fatalf("error = %q, want it to name the root requirement and passwordless sudo", msg)
	}
	if got := commandErrorRemediation(msg); !strings.Contains(got, "passwordless sudo") {
		t.Fatalf("remediation = %q, want a sudo/root hint", got)
	}
}

func TestPrepareBecomeCredentialReusesInheritedPasswordFile(t *testing.T) {
	path, cleanup, err := become.WritePasswordFile("secret")
	if err != nil {
		t.Fatalf("writeBecomePasswordFile: %v", err)
	}
	defer cleanup()
	t.Setenv(localRootSudoAuthEnv, localSudoAuthPrompted)
	t.Setenv(localRootBecomePasswordFileEnv, path)

	var prompt bytes.Buffer
	credential, credentialCleanup, err := prepareBecomeCredential(strings.NewReader("should-not-read\n"), &prompt, true, true, true)
	if err != nil {
		t.Fatalf("prepareBecomeCredential: %v", err)
	}
	defer credentialCleanup()
	if prompt.String() != "" {
		t.Fatalf("prompt = %q, want no prompt", prompt.String())
	}
	if credential.Password != "secret" {
		t.Fatalf("password = %q, want inherited secret", credential.Password)
	}
	if credential.PasswordFile != path {
		t.Fatalf("password file = %q, want %q", credential.PasswordFile, path)
	}
	if credential.Prompted {
		t.Fatal("inherited password file should not be marked prompted")
	}
}

func TestPrepareBecomeCredentialPromptsOnceForPasswordAndFile(t *testing.T) {
	var prompt bytes.Buffer
	credential, cleanup, err := prepareBecomeCredential(strings.NewReader("secret\n"), &prompt, true, true, true)
	if err != nil {
		t.Fatalf("prepareBecomeCredential: %v", err)
	}
	defer cleanup()
	if prompt.String() != "BECOME password: " {
		t.Fatalf("prompt = %q, want BECOME password prompt", prompt.String())
	}
	if credential.Password != "secret" {
		t.Fatalf("password = %q, want secret", credential.Password)
	}
	if credential.PasswordFile == "" {
		t.Fatal("password file is empty")
	}
	if !credential.Prompted {
		t.Fatal("fresh password should be marked prompted")
	}
}
