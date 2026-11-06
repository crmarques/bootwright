//go:build linux && amd64

package sshlocal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHClientRefusesAnUnqualifiedExecutableBeforeLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(path, []byte("untrusted executable"), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := (Launcher{Client: path}).executable(); err == nil {
		t.Fatal("an invoking-account-owned writable SSH client was accepted")
	}
}
