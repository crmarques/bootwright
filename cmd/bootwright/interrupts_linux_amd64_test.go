//go:build linux && amd64

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"os/signal"
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
		t.Skipf("no pseudo-terminal is available to this test: %v", err)
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
	addSecretInput(t, input, "environment.yaml", strings.ReplaceAll(syntheticEnvironment, "example.test", "changed.test"))
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
	if !errors.As(err, &exit) || exit.ExitCode() != 130 || !strings.HasPrefix(stdout.String(), "Context update plan\n") || strings.Contains(stdout.String(), "[OK]") || !strings.Contains(stderr.String(), "runtime.interrupted") {
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
	deps.Streams.Out, deps.Streams.Err = os.Stdout, os.Stderr
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

// A closed terminal or a lost SSH session delivers SIGHUP. Uncaught, it ends
// the process at once, skipping the cancellation that stops and reaps the
// operation's adapters, so it cancels exactly as SIGINT and SIGTERM do.
func TestHangupCancelsTheOperationLikeTerminate(t *testing.T) {
	for _, received := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(received.String(), func(t *testing.T) {
			// A second subscriber keeps an unhandled signal from ending the test
			// binary, so a missing registration fails here instead.
			guard := make(chan os.Signal, 1)
			signal.Notify(guard, received)
			defer signal.Stop(guard)
			ctx, finish := beginSignalOperation(context.Background())
			defer finish()
			if err := syscall.Kill(os.Getpid(), received); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not cancel the operation", received)
			}
			if cause := context.Cause(ctx); !errors.Is(cause, cli.ErrInterrupted) {
				t.Fatalf("%s canceled with %v, want the interrupt cause", received, cause)
			}
		})
	}
}

// A process started with hangups ignored, as nohup starts it, was asked to
// outlive them, so a hangup leaves its operation running.
func TestAnIgnoredHangupLeavesTheOperationRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The shell ignores SIGHUP and the helper inherits that across exec.
	command := exec.CommandContext(ctx, "/bin/sh", "-c", `trap '' HUP; exec "$0" "$@"`, os.Args[0], "-test.run=^TestIgnoredHangupHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_HANGUP_HELPER=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(output)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		cancel()
		_ = command.Wait()
		t.Fatalf("the operation did not begin: %q (%v)", line, err)
	}
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	// The hangup is pending or handled before the helper reads this line.
	if _, err := io.WriteString(input, "sent\n"); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(reader)
	if err := command.Wait(); err != nil || string(rest) != "running\n" {
		t.Fatalf("an ignored hangup ended the operation: %q (%v)", rest, err)
	}
}

func TestIgnoredHangupHelper(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_HANGUP_HELPER") != "1" {
		return
	}
	ctx, finish := beginSignalOperation(context.Background())
	defer finish()
	fmt.Println("ready")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(20)
	}
	select {
	case <-ctx.Done():
		fmt.Println("canceled")
	case <-time.After(500 * time.Millisecond):
		fmt.Println("running")
	}
	os.Exit(0)
}

// sudo can hand the elevated child one terminal interrupt twice, the kernel's
// and the supervisor's relay, so without the proof that it is the foreground
// of a pseudo-terminal of its own the child never escalates on a second
// signal: its bounded cancellation runs to its own end, and only the
// supervisor's second signal kills sudo. The test binary is no supervised
// child, so it never has that proof.
func TestASecondInterruptLeavesTheCancellationRunning(t *testing.T) {
	for _, received := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(received.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecondInterruptHelper$")
			command.Env = append(os.Environ(), "BOOTWRIGHT_SECOND_INTERRUPT_HELPER=1")
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(output)
			expect := func(want string) {
				t.Helper()
				if line, err := reader.ReadString('\n'); err != nil || line != want {
					cancel()
					_ = command.Wait()
					t.Fatalf("the helper wrote %q (%v), want %q", line, err, want)
				}
			}
			expect("ready\n")
			if err := command.Process.Signal(received); err != nil {
				t.Fatal(err)
			}
			expect("canceled\n")
			if err := command.Process.Signal(received); err != nil {
				t.Fatal(err)
			}
			// The second signal is pending or handled before the helper reads this line.
			if _, err := io.WriteString(input, "sent\n"); err != nil {
				t.Fatal(err)
			}
			rest, _ := io.ReadAll(reader)
			if err := command.Wait(); err != nil || string(rest) != "cleaned\n" {
				t.Fatalf("a second %s cut the cancellation short: %q (%v)", received, rest, err)
			}
		})
	}
}

func TestASecondInterruptEndsAChildInItsOwnForegroundTerminalAtOnce(t *testing.T) {
	for _, received := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(received.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecondInterruptHelper$")
			command.Env = append(os.Environ(), "BOOTWRIGHT_SECOND_INTERRUPT_HELPER=escalating")
			if _, err := command.StdinPipe(); err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(output)
			expect := func(want string) {
				t.Helper()
				if line, err := reader.ReadString('\n'); err != nil || line != want {
					cancel()
					_ = command.Wait()
					t.Fatalf("the helper wrote %q (%v), want %q", line, err, want)
				}
			}
			expect("ready\n")
			if err := command.Process.Signal(received); err != nil {
				t.Fatal(err)
			}
			expect("canceled\n")
			if err := command.Process.Signal(received); err != nil {
				t.Fatal(err)
			}
			rest, _ := io.ReadAll(reader)
			err = command.Wait()
			var exit *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 130 || len(rest) != 0 {
				t.Fatalf("a second %s left the helper %v with %q (deadline %v), want status 130 at once", received, err, rest, ctx.Err())
			}
		})
	}
}

func TestSecondInterruptHelper(t *testing.T) {
	switch os.Getenv("BOOTWRIGHT_SECOND_INTERRUPT_HELPER") {
	case "1":
	case "escalating":
		escalatesOnItsOwn = func() bool { return true }
	default:
		return
	}
	ctx, finish := beginSignalOperation(context.Background())
	defer finish()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("canceled")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(20)
	}
	time.Sleep(500 * time.Millisecond)
	fmt.Println("cleaned")
	os.Exit(0)
}

// Most operations end without a signal. Finishing one must still return, since
// every invocation finishes its operation before it exits, and must not report
// the operation as interrupted.
func TestFinishingAnUninterruptedOperationReturns(t *testing.T) {
	ctx, finish := beginSignalOperation(context.Background())
	if ctx.Err() != nil {
		t.Fatalf("the operation began canceled: %v", context.Cause(ctx))
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		finish()
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("finish did not return")
	}
	if ctx.Err() == nil {
		t.Fatal("finish left the operation running")
	}
	if cause := context.Cause(ctx); errors.Is(cause, cli.ErrInterrupted) {
		t.Fatalf("an operation finished without a signal ended with %v", cause)
	}
}

// privilege.Begin is the one signal subscription, which the supervisor relays
// and the CLI's operation reads as an interrupt, so the composition root holds
// none of its own.
func TestTheCompositionRootSubscribesToNoSignalOfItsOwn(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil || len(sources) == 0 {
		t.Fatal("no composition source", err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			if imported.Path.Value == `"os/signal"` {
				t.Errorf("%s subscribes to signals itself; derive the operation's cancellation from privilege.Begin", source)
			}
		}
	}
}
