//go:build linux && amd64

package venvlocal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

// TestALiveBuildOnThisHost builds a real venv from the embedded lock on this
// host's /usr/bin/python3.X and launches through the guard. It needs network
// on a cold cache and runs only when asked:
//
//	BOOTWRIGHT_VENV_LIVE=1 [BOOTWRIGHT_VENV_WHEELS=<dir>] ./scripts/go test -run TestALiveBuildOnThisHost -v ./internal/controller/venvlocal/
//
// BOOTWRIGHT_VENV_WHEELS keeps the wheel cache across runs.
func TestALiveBuildOnThisHost(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_VENV_LIVE") != "1" {
		t.Skip("set BOOTWRIGHT_VENV_LIVE=1 to build a real venv on this host")
	}
	lock, err := runtimelock.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	runtimes := filepath.Join(root, "runtimes")
	wheels := cmpOr(os.Getenv("BOOTWRIGHT_VENV_WHEELS"), filepath.Join(root, "wheels"))
	for _, directory := range []string{runtimes, wheels} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	host := newFakeHost(t)
	adapter := New(Config{Lock: lock, Runtimes: runtimes, Wheels: wheels})
	publishers.Store(adapter, &renameStore{host: host, runtimes: runtimes})
	me := uint32(os.Getuid())
	adapter.trusted = func(uid uint32) bool { return uid == 0 || uid == me }
	ctx := context.Background()
	interpreter, err := adapter.Select(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("selected %s (Python %s, glibc %s)", interpreter.Path, interpreter.Version, interpreter.Glibc)
	if err := adapter.Fetch(ctx, interpreter.ABI, prerequisites.SetupEgress{HTTPSProxy: os.Getenv("HTTPS_PROXY")}); err != nil {
		t.Fatal(err)
	}
	runtime, err := testBuild(adapter, ctx, interpreter)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, err := adapter.Inspect(ctx, interpreter); err != nil || !ready {
		t.Fatalf("Inspect = %t, %v", ready, err)
	}
	err = adapter.WithPython(ctx, runtime, func(launch prerequisites.PythonLaunch) error {
		arguments := append(launch.Arguments, "-c", "import ansible.release, sys; print(ansible.release.__version__, sys.flags.isolated, sys.prefix != sys.base_prefix)")
		process := exec.CommandContext(ctx, launch.Program, arguments...)
		process.Env = launch.Environment
		output, err := process.Output()
		if err != nil {
			return err
		}
		if got, want := strings.TrimSpace(string(output)), lock.AnsibleCore()+" 1 True"; got != want {
			t.Errorf("the launched runtime printed %q, want %q", got, want)
		}
		shim, err := exec.CommandContext(ctx, launch.Shim, "-c", "import sys; print(sys.flags.dont_write_bytecode)").Output()
		if err != nil || strings.TrimSpace(string(shim)) != "1" {
			t.Errorf("the shim printed %q, %v", shim, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func cmpOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
