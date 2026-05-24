//go:build linux

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandWithControllingTTYProvidesControllingTTY(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommandWithControllingTTY(context.Background(), nil, &stdout, &stderr, []string{
		"sh",
		"-c",
		"test -t 0 && test -t 1 && test -t 2 && exec 3</dev/tty && echo tty-present",
	}, nil)
	if err != nil {
		t.Fatalf("runCommandWithControllingTTY failed: %v; stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tty-present") {
		t.Fatalf("expected command to see a tty on stdin; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunCommandWithControllingTTYProvidesTTYToNestedPipeChild(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommandWithControllingTTY(context.Background(), nil, &stdout, &stderr, []string{
		"python3",
		"-c",
		"import subprocess, sys; p = subprocess.run(['sh', '-c', 'test -t 0 || true; exec 3</dev/tty && echo nested-tty'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True); sys.stdout.write(p.stdout); sys.stderr.write(p.stderr); sys.exit(p.returncode)",
	}, nil)
	if err != nil {
		t.Fatalf("runCommandWithControllingTTY nested child failed: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "nested-tty") {
		t.Fatalf("expected nested child to see controlling tty; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestControllerCLISudoCommandGetsControllingTTY(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	fakeSudo := filepath.Join(fakeBin, "sudo")
	if err := os.WriteFile(fakeSudo, []byte(`#!/bin/sh
if ! test -t 0; then
  printf '%s\n' 'sudo: sorry, you must have a tty to run sudo' >&2
  exit 1
fi
if ! exec 3</dev/tty; then
  printf '%s\n' 'sudo: sorry, you must have a tty to run sudo' >&2
  exit 1
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    -u|-p)
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      break
      ;;
  esac
done
exec "$@"
`), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := append(os.Environ(),
		"PATH="+os.Getenv("PATH"),
	)
	err := runCommandWithControllingTTY(
		context.Background(),
		nil,
		&stdout,
		&stderr,
		controllerCLIInstallCommand([]string{"/bin/true"}, false, ""),
		env,
	)
	if err != nil {
		t.Fatalf("controller CLI sudo command failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
