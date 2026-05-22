package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
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

func TestPrepareBecomePasswordFileRejectsEmptyPassword(t *testing.T) {
	_, _, err := prepareBecomePasswordFile(strings.NewReader("\n"), io.Discard)
	if err == nil {
		t.Fatal("empty password should fail")
	}
}
