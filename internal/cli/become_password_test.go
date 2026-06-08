package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestWriteBecomePasswordFileUsesRestrictedDirectory(t *testing.T) {
	path, cleanup, err := writeBecomePasswordFile("secret")
	if err != nil {
		t.Fatalf("writeBecomePasswordFile: %v", err)
	}
	defer cleanup()
	dir := filepath.Dir(path)
	if dir == os.TempDir() {
		t.Fatalf("password file placed directly in %s; want a dedicated 0700 per-run directory", os.TempDir())
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat password directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("password directory mode = %03o, want 700", got)
	}
	cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup did not remove password directory, stat err=%v", err)
	}
}

func TestPrepareBecomePasswordFileRejectsEmptyPassword(t *testing.T) {
	_, _, err := prepareBecomePasswordFile(strings.NewReader("\n"), io.Discard)
	if err == nil {
		t.Fatal("empty password should fail")
	}
}

func TestPrepareBecomeCredentialReusesInheritedPasswordFile(t *testing.T) {
	path, cleanup, err := writeBecomePasswordFile("secret")
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
