//go:build linux

package nativeexec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const shell = "/bin/sh"

func TestADrainingOutputKeepsItsBoundAndLetsTheChildFinish(t *testing.T) {
	stdout, stderr := Head(1024), Tail(8)
	script := "head -c 1048576 /dev/zero; printf 'early-late-end' >&2; exit 0"
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", script}, Stdout: stdout, Stderr: stderr})
	if !result.Succeeded() {
		t.Fatalf("a child writing past a draining bound did not finish: %+v", result)
	}
	if stdout.Len() != 1024 || !stdout.Overflowed() || !bytes.Equal(stdout.Bytes(), make([]byte, 1024)) {
		t.Fatalf("head kept %d bytes, overflowed=%t", stdout.Len(), stdout.Overflowed())
	}
	if stderr.String() != "late-end" || !stderr.Overflowed() {
		t.Fatalf("tail kept %q, overflowed=%t", stderr.String(), stderr.Overflowed())
	}
}

func TestADrainingOutputNeverShortWrites(t *testing.T) {
	for _, output := range []*Output{Head(4), Tail(4)} {
		for _, chunk := range []string{"abc", "defgh", "ij"} {
			if written, err := output.Write([]byte(chunk)); err != nil || written != len(chunk) {
				t.Fatalf("Write(%q) = %d, %v", chunk, written, err)
			}
		}
		if output.Len() != 4 || !output.Overflowed() {
			t.Fatalf("kept %q, overflowed=%t", output.String(), output.Overflowed())
		}
	}
	exact := Head(3)
	if _, err := exact.Write([]byte("abc")); err != nil || exact.Overflowed() || exact.String() != "abc" {
		t.Fatalf("an exact fill overflowed: %q", exact.String())
	}
}

func TestARefusingOutputRefusesTheExcessWithoutRetainingIt(t *testing.T) {
	output := Refusing(8)
	if written, err := output.Write([]byte("12345678")); err != nil || written != 8 {
		t.Fatalf("an exact fill was refused: %d %v", written, err)
	}
	if written, err := output.Write([]byte("secret")); !errors.Is(err, ErrOverflow) || written != 0 || !output.Overflowed() || output.String() != "12345678" {
		t.Fatalf("Write past the bound = %d, %v, kept %q", written, err, output.String())
	}
	if _, err := output.Write(nil); !errors.Is(err, ErrOverflow) {
		t.Fatal("a refusing output accepted a write after it overflowed")
	}
	output.Clear()
	if output.Len() != 0 {
		t.Fatal("Clear kept data")
	}
	stdout := Refusing(1024)
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "head -c 1048576 /dev/zero"}, Stdout: stdout})
	if result.Succeeded() || !stdout.Overflowed() || stdout.Len() > 1024 {
		t.Fatalf("a refusing bound let the child succeed: %+v, %d bytes", result, stdout.Len())
	}
}

func TestTheEnvironmentIsOnlyWhatTheCallerGives(t *testing.T) {
	t.Setenv("BOOTWRIGHT_INHERITED_PROBE", "leaked")
	stdout := Head(4096)
	if result := Run(context.Background(), Command{Path: "/usr/bin/env", Stdout: stdout}); !result.Succeeded() || stdout.Len() != 0 {
		t.Fatalf("an empty environment printed %q: %+v", stdout.String(), result)
	}
	stdout = Head(4096)
	if result := Run(context.Background(), Command{Path: "/usr/bin/env", Env: []string{"LC_ALL=C"}, Stdout: stdout}); !result.Succeeded() || stdout.String() != "LC_ALL=C\n" {
		t.Fatalf("the given environment printed %q: %+v", stdout.String(), result)
	}
}

func TestTheChildStartsInTheRootDirectoryWithTheGivenStdin(t *testing.T) {
	stdout := Head(4096)
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "pwd; cat"}, Stdin: strings.NewReader("given\n"), Stdout: stdout})
	if !result.Succeeded() || stdout.String() != "/\ngiven\n" {
		t.Fatalf("printed %q: %+v", stdout.String(), result)
	}
}

func TestEveryExitIsClassified(t *testing.T) {
	expired, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	for _, test := range []struct {
		name string
		ctx  context.Context
		path string
		args []string
		want Result
	}{
		{"exited", context.Background(), shell, []string{"-c", "exit 3"}, Result{Outcome: Exited, ExitCode: 3}},
		{"succeeded", context.Background(), shell, []string{"-c", "exit 0"}, Result{Outcome: Exited}},
		{"signalled", context.Background(), shell, []string{"-c", "kill -TERM $$"}, Result{Outcome: Signalled, ExitCode: -1, Signal: syscall.SIGTERM}},
		{"deadline", expired, shell, []string{"-c", "sleep 30"}, Result{Outcome: Deadline, ExitCode: -1, Signal: syscall.SIGKILL}},
		{"cancelled before start", cancelled, shell, []string{"-c", "exit 0"}, Result{Outcome: Cancelled, ExitCode: -1}},
		{"missing", context.Background(), "/nonexistent/bootwright-probe", nil, Result{Outcome: StartFailed, ExitCode: -1}},
		{"relative", context.Background(), "sh", nil, Result{Outcome: StartFailed, ExitCode: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Run(test.ctx, Command{Path: test.path, Args: test.args})
			if got.Outcome != test.want.Outcome || got.ExitCode != test.want.ExitCode || got.Signal != test.want.Signal {
				t.Fatalf("Run = %+v, want %+v", got, test.want)
			}
			if (got.Err == nil) != (test.want.Outcome == Exited) {
				t.Fatalf("Run error = %v for outcome %d", got.Err, got.Outcome)
			}
		})
	}
}

func TestACancelledRunKeepsHowTheChildEnded(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "trapped")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() {
		script := "trap 'exit 7' TERM; echo $$ > " + marker + "; while :; do sleep 0.05; done"
		done <- Run(ctx, Command{Path: shell, Args: []string{"-c", script}, Interrupt: syscall.SIGTERM, Foreground: true})
	}()
	waitForPID(t, marker)
	cancel()
	if result := <-done; result.Outcome != Cancelled || result.ExitCode != 7 || result.Signal != 0 || result.Err == nil {
		t.Fatalf("Run = %+v, want cancelled with the child's exit code", result)
	}
}

func TestCancellationKillsTheWholeProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() {
		done <- Run(ctx, Command{Path: shell, Args: []string{"-c", "sleep 30 & echo $! > " + marker + "; wait"}})
	}()
	grandchild := waitForPID(t, marker)
	cancel()
	if result := <-done; result.Outcome != Cancelled {
		t.Fatalf("Run = %+v, want cancelled", result)
	}
	waitGone(t, grandchild)
}

func TestTheWaitBoundEndsARunWhoseDescendantHoldsThePipes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild")
	started := time.Now()
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "sleep 30 & echo $! > " + marker}, Stdout: Head(64), WaitDelay: 200 * time.Millisecond})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run blocked %s on a descendant holding its pipes", elapsed)
	}
	if result.Outcome != Lingered || result.ExitCode != 0 || result.Err == nil || result.Succeeded() {
		t.Fatalf("Run = %+v, want lingered", result)
	}
	waitGone(t, waitForPID(t, marker))
}

func TestAFailedRunLeavesNoDescendantInItsGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild")
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "sleep 30 & echo $! > " + marker + "; exit 3"}, Stdout: Head(64), WaitDelay: 200 * time.Millisecond})
	if result.Outcome != Exited || result.ExitCode != 3 {
		t.Fatalf("Run = %+v, want exited 3", result)
	}
	waitGone(t, waitForPID(t, marker))
}

func TestTheWaitBoundEndsACancelledRunWhoseDescendantHoldsThePipes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := Run(ctx, Command{Path: shell, Args: []string{"-c", "sleep 30 & echo $! > " + marker + "; sleep 30"}, Stdout: Head(64), Foreground: true, Interrupt: syscall.SIGTERM, WaitDelay: 200 * time.Millisecond})
	_ = syscall.Kill(waitForPID(t, marker), syscall.SIGKILL)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run blocked %s on a descendant holding its pipes", elapsed)
	}
	if result.Outcome != Deadline {
		t.Fatalf("Run = %+v, want deadline", result)
	}
}

func TestAGroupedChildLeadsItsGroupAndAForegroundChildSharesOurs(t *testing.T) {
	for _, foreground := range []bool{false, true} {
		stdout := Head(256)
		result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "echo $$; cut -d' ' -f5 /proc/$$/stat"}, Stdout: stdout, Foreground: foreground})
		fields := strings.Fields(stdout.String())
		if !result.Succeeded() || len(fields) != 2 {
			t.Fatalf("printed %q: %+v", stdout.String(), result)
		}
		want := fields[0]
		if foreground {
			want = strconv.Itoa(syscall.Getpgrp())
		}
		if fields[1] != want {
			t.Fatalf("foreground=%t: process group %s, want %s", foreground, fields[1], want)
		}
	}
}

func TestAParentDeathSignalRunsFromALockedThread(t *testing.T) {
	stdout := Head(256)
	result := Run(context.Background(), Command{Path: shell, Args: []string{"-c", "echo ran"}, Stdout: stdout, ParentDeath: syscall.SIGKILL})
	if !result.Succeeded() || stdout.String() != "ran\n" {
		t.Fatalf("printed %q: %+v", stdout.String(), result)
	}
}

func waitForPID(t *testing.T, marker string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		data, err := os.ReadFile(marker)
		if pid, parsed := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && parsed == nil && pid > 0 {
			return pid
		}
	}
	t.Fatal("the grandchild never recorded its process ID")
	return 0
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil || strings.Contains(string(data), ") Z ") {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("descendant %d outlived the run", pid)
}
