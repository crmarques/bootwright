//go:build linux && amd64

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/crmarques/bootwright/internal/cli"
)

func openTestTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	descriptor, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open PTY master: %v", err)
	}
	master := os.NewFile(uintptr(descriptor), "test-terminal-master")
	t.Cleanup(func() { master.Close() })
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("unlock PTY slave: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number))); errno != 0 {
		t.Fatalf("resolve PTY slave number: %v", errno)
	}
	descriptor, err = syscall.Open(fmt.Sprintf("/dev/pts/%d", number), syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open PTY slave: %v", err)
	}
	slave := os.NewFile(uintptr(descriptor), "test-terminal-slave")
	t.Cleanup(func() { slave.Close() })
	return master, slave
}

func TestInterruptDuringRealConfirmationPreservesSelection(t *testing.T) {
	parent := t.TempDir()
	input := filepath.Join(parent, "input")
	stateParent := filepath.Join(parent, "state")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(syntheticEnvironment), 0600); err != nil {
		t.Fatal(err)
	}
	addSecretInput(t, input, "controller.yaml", serviceHost)
	root := filepath.Join(stateParent, "bootwright")
	if err := os.MkdirAll(stateParent, 0700); err != nil {
		t.Fatal(err)
	}
	repository := testRepository(root)
	services := testServices(t, repository, root)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	before, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pointer := testContextWiring(t, root).Selection
	selectedBefore, err := pointer.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, slave := openTestTerminal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInteractiveInterruptHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_INTERRUPT_HELPER=1", "BOOTWRIGHT_INTERRUPT_INPUT="+input, "BOOTWRIGHT_INTERRUPT_STATE="+root)
	command.Stdin = slave
	var stdout bytes.Buffer
	command.Stdout = &stdout
	errorPipe, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			cancel()
			_ = command.Wait()
		}
	})
	reader := bufio.NewReader(errorPipe)
	var stderr strings.Builder
	for !strings.HasSuffix(stderr.String(), "[y/N] ") {
		value, err := reader.ReadByte()
		if err != nil {
			cancel()
			_ = command.Wait()
			t.Fatal("confirmation prompt missing", stderr.String(), err)
		}
		stderr.WriteByte(value)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	for {
		value, err := reader.ReadByte()
		if err != nil {
			break
		}
		stderr.WriteByte(value)
	}
	err = command.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 130 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "runtime.interrupted") {
		t.Fatalf("interrupt: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	after, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("interrupted confirmation changed context state")
	}
	selectedAfter, err := pointer.Read(context.Background())
	if err != nil || selectedAfter != selectedBefore {
		t.Fatal("interrupted confirmation changed user selection", err)
	}
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
}

func TestInteractiveInterruptHelper(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_INTERRUPT_HELPER") != "1" {
		return
	}
	root := os.Getenv("BOOTWRIGHT_INTERRUPT_STATE")
	repository := testRepository(root)
	confirmer := cli.NewConfirmation(readStdin, os.Stderr, stdinTerminal)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	deps.Confirmer, deps.SecretInput = confirmer, secretInputFunc(readStdin)
	services := assembleServices(deps)
	os.Exit(runServices(context.Background(), []string{"context", "update", "--name", "alpha", "--input-dir", os.Getenv("BOOTWRIGHT_INTERRUPT_INPUT")}, os.Stdout, os.Stderr, services, invocationHooks{begin: beginSignalOperation}))
}

func TestTerminalFlagsAreRestoredAfterReadyAndEmptyReads(t *testing.T) {
	master, slave := openTestTerminal(t)
	fd := int(slave.Fd())
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFL, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	assertFlags := func() {
		t.Helper()
		got, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFL, 0)
		if errno != 0 || got != flags {
			t.Fatal("terminal flags changed", got, flags, errno)
		}
	}
	buffer := make([]byte, 64)
	if _, err := readReadyInput(fd, buffer); err != syscall.EAGAIN {
		t.Fatal("empty terminal blocked or read", err)
	}
	assertFlags()
	if _, err := master.Write([]byte("yes\n")); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = original })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if n, err := readStdin(ctx, buffer); err != nil || string(buffer[:n]) != "yes\n" {
		t.Fatal(n, string(buffer), err)
	}
	assertFlags()
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := readStdin(canceled, buffer); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertFlags()
}
