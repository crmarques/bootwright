//go:build linux && amd64

package hostlinux

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRuntimeRefusesHeldNativePOSIXTransactionWithoutReadingFiles(t *testing.T) {
	i := fixture(t, "btrfs")
	requirement := runtimeFixture(t, i)
	stop := holdNativeTransaction(t, filepath.Join(i.view.root, requirement.LockPath))
	reads := 0
	i.view.stat = func(name string, _ *unix.Stat_t) {
		if name == "/usr/bin/podman" {
			reads++
		}
	}
	_, err := i.Runtime(context.Background(), requirement)
	assertDiagnostic(t, err, "controller.conflict")
	if reads != 0 {
		t.Fatal("runtime files were inspected during a native transaction")
	}
	stop()
	result, err := i.Runtime(context.Background(), requirement)
	if err != nil || !result.Ready {
		t.Fatal("released native lock remained blocked", result, err)
	}
}

func TestConcurrentRuntimeLocksHaveIndependentLifetimes(t *testing.T) {
	i := fixture(t, "btrfs")
	requirement := runtimeFixture(t, i)
	fs, err := i.view.open(context.Background())
	must(t, err)
	defer fs.close()
	first, err := fs.runtimeLock(context.Background(), requirement.LockPath)
	must(t, err)
	second, err := fs.runtimeLock(context.Background(), requirement.LockPath)
	must(t, err)
	defer second.Close()
	must(t, first.Close())
	tryNativeTransaction(t, filepath.Join(i.view.root, requirement.LockPath), "busy")
	must(t, second.Close())
	tryNativeTransaction(t, filepath.Join(i.view.root, requirement.LockPath), "available")
}

func TestRuntimeLockAbsenceAndUnsafeLinksNeverCreateOrRepairState(t *testing.T) {
	for _, variant := range []string{"absent", "symlink", "mutable"} {
		t.Run(variant, func(t *testing.T) {
			i := fixture(t, "btrfs")
			requirement := runtimeFixture(t, i)
			name := filepath.Join(i.view.root, requirement.LockPath)
			switch variant {
			case "absent":
				must(t, os.Remove(name))
			case "symlink":
				must(t, os.Rename(name, name+".real"))
				must(t, os.Symlink(".rpm.lock.real", name))
			case "mutable":
				must(t, os.Chmod(name, 0666))
			}
			before := snapshot(t, i.view.root)
			_, err := i.Runtime(context.Background(), requirement)
			assertDiagnostic(t, err, "controller.unsupported")
			after := snapshot(t, i.view.root)
			if len(before) != len(after) {
				t.Fatal("runtime lock refusal changed the filesystem")
			}
			for name, value := range before {
				if after[name] != value {
					t.Fatal("runtime lock refusal repaired metadata")
				}
			}
		})
	}
}

func holdNativeTransaction(t *testing.T, name string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	command := nativeLockCommand(t, ctx, name, "hold")
	input, err := command.StdinPipe()
	must(t, err)
	output, err := command.StdoutPipe()
	must(t, err)
	must(t, command.Start())
	line, err := bufio.NewReader(output).ReadString('\n')
	must(t, err)
	if line != "locked\n" {
		t.Fatalf("native helper did not lock: %q", line)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		input.Close()
		err := command.Wait()
		cancel()
		if err != nil {
			t.Errorf("native lock helper: %v", err)
		}
	}
	t.Cleanup(stop)
	return stop
}

func tryNativeTransaction(t *testing.T, name, expected string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := nativeLockCommand(t, ctx, name, expected).CombinedOutput()
	if err != nil {
		t.Fatalf("native lock probe: %v %s", err, output)
	}
}

func nativeLockCommand(t *testing.T, ctx context.Context, name, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	must(t, err)
	command := exec.CommandContext(ctx, executable, "-test.run=^TestNativePOSIXLockHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_TEST_NATIVE_LOCK="+name, "BOOTWRIGHT_TEST_NATIVE_LOCK_MODE="+mode)
	return command
}

func TestNativePOSIXLockHelper(t *testing.T) {
	name := os.Getenv("BOOTWRIGHT_TEST_NATIVE_LOCK")
	if name == "" {
		return
	}
	file, err := os.OpenFile(name, os.O_RDWR, 0)
	must(t, err)
	defer file.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	err = unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock)
	mode := os.Getenv("BOOTWRIGHT_TEST_NATIVE_LOCK_MODE")
	if mode == "busy" {
		if err != unix.EAGAIN && err != unix.EACCES {
			t.Fatalf("expected native write contention: %v", err)
		}
		return
	}
	must(t, err)
	if mode == "hold" {
		_, err = io.Copy(os.Stdout, strings.NewReader("locked\n"))
		must(t, err)
		_, err = io.Copy(io.Discard, os.Stdin)
		must(t, err)
	}
}
