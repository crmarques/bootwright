//go:build linux && amd64

package privilege

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "__bootwright_account_root", "__bootwright_account_user":
			uid, gid, home := 0, 0, "/root"
			if os.Args[1] == "__bootwright_account_user" {
				uid, gid, home = 60001, 60002, "/home/operator"
			}
			account, err := (Resolver{}).Resolve(context.Background())
			if err != nil || account.UID != uid || account.GID != gid || account.Home != home || account.SudoParentPID != 0 {
				os.Exit(2)
			}
			fmt.Println("verified")
			os.Exit(0)
		case "__bootwright_account_parent", "__bootwright_account_root_shell", "__bootwright_account_reexecution":
			mode, environment := "__bootwright_account_child", []string{"SUDO_UID=60001", "SUDO_GID=60002", "SUDO_USER=operator", "HOME=/incorrect"}
			switch os.Args[1] {
			case "__bootwright_account_root_shell":
				mode, environment = "__bootwright_account_root", append(environment, "SUDO_COMMAND=/bin/bash")
			case "__bootwright_account_reexecution":
				mode, environment = "__bootwright_account_root", append(environment, "SUDO_COMMAND=/proc/1/exe status")
			}
			child := exec.Command("/usr/bin/sudo", mode)
			child.Env = environment
			child.Stdin = os.Stdin
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Run() != nil {
				os.Exit(2)
			}
			os.Exit(0)
		case "__bootwright_account_child":
			account, err := (Resolver{}).Resolve(context.Background())
			if err != nil || account.UID != 60001 || account.GID != 60002 || account.Home != "/home/operator" || account.Name != "operator" || account.SudoParentPID != os.Getppid() {
				os.Exit(2)
			}
			fmt.Println("verified")
			os.Exit(0)
		case "__bootwright_guard_child":
			release, err := GuardParent(os.Getppid())
			if err != nil {
				os.Exit(2)
			}
			defer release()
			thread := syscall.Gettid()
			for range 20 {
				runtime.GC()
				runtime.Gosched()
				time.Sleep(time.Millisecond)
				if syscall.Gettid() != thread {
					os.Exit(2)
				}
			}
			fmt.Println("ready")
			time.Sleep(time.Hour)
			os.Exit(3)
		case "__bootwright_guard_parent":
			self, err := os.Executable()
			if err != nil {
				os.Exit(2)
			}
			child := exec.Command(self, "__bootwright_guard_child")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Start() != nil {
				os.Exit(2)
			}
			fmt.Printf("pid %d\n", child.Process.Pid)
			var release [1]byte
			if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		case "__bootwright_relay_success", "__bootwright_relay_interrupted", "__bootwright_relay_orphaned", "__bootwright_relay_slow_cleanup":
			relayedChild(os.Args[1])
		case "__bootwright_relay_stubborn":
			signal.Ignore(syscall.SIGTERM)
			fmt.Printf("child %d\nready\n", os.Getpid())
			time.Sleep(time.Minute)
			os.Exit(0)
		case "__bootwright_hold_output":
			time.Sleep(time.Minute)
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// relayedChild waits for the signal its supervisor relays and then ends as its
// mode says: with its document and 0, with 130, with its document and 0 while
// a process it started still holds its standard output, or with its document
// and 0 after a cleanup of a second and a half.
func relayedChild(mode string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	if mode == "__bootwright_relay_orphaned" {
		self, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		holder := exec.Command(self, "__bootwright_hold_output")
		holder.Stdout = os.Stdout
		if holder.Start() != nil {
			os.Exit(2)
		}
		fmt.Printf("holder %d\n", holder.Process.Pid)
	}
	fmt.Println("ready")
	<-signals
	switch mode {
	case "__bootwright_relay_interrupted":
		os.Exit(130)
	case "__bootwright_relay_slow_cleanup":
		time.Sleep(1500 * time.Millisecond)
	}
	fmt.Println(`{"exitCode":0}`)
	os.Exit(0)
}

// readyOutput is a child's standard output that says when the child is ready
// for the relayed signal. It holds its buffer in a field, since an embedded
// one would lend io.Copy a ReadFrom that bypasses Write.
type readyOutput struct {
	written bytes.Buffer
	ready   chan struct{}
	seen    bool
}

func (o *readyOutput) Write(data []byte) (int, error) {
	n, err := o.written.Write(data)
	if !o.seen && strings.Contains(o.written.String(), "ready\n") {
		o.seen = true
		close(o.ready)
	}
	return n, err
}

type relayEnding struct {
	code int
	err  error
}

// relayInterrupt runs one child in mode, as the elevated command when elevated
// says so, with escalated as its escalation channel. Once the child is ready
// it relays SIGTERM and returns what the child writes, when it relayed and
// the channel the child's ending arrives on.
func relayInterrupt(t *testing.T, mode string, elevated bool, grace time.Duration, escalated <-chan struct{}) (*readyOutput, time.Time, <-chan relayEnding) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(withEscalation(context.Background(), escalated))
	t.Cleanup(func() { cancel(nil) })
	output := &readyOutput{ready: make(chan struct{})}
	ended := make(chan relayEnding, 1)
	go func() {
		code, err := runProcess(ctx, Command{Executable: self, Arguments: []string{mode}, Environment: []string{"LANG=C"}, Output: output, Elevated: elevated}, grace)
		ended <- relayEnding{code, err}
	}()
	select {
	case <-output.ready:
	case end := <-ended:
		t.Fatalf("the child ended before it was ready: %d, %v", end.code, end.err)
	case <-time.After(30 * time.Second):
		t.Fatal("the child never became ready")
	}
	relayed := time.Now()
	cancel(signalCause{signal: syscall.SIGTERM})
	return output, relayed, ended
}

// awaitEnding fails rather than hangs when a relayed command outlives within.
func awaitEnding(t *testing.T, ended <-chan relayEnding, within time.Duration) relayEnding {
	t.Helper()
	select {
	case end := <-ended:
		return end
	case <-time.After(within):
		t.Fatalf("the command did not end within %v", within)
	}
	return relayEnding{}
}

// killStubbornChildOnFailure ends a stubborn child that a failing test left
// running; a passing test has reaped it already.
func killStubbornChildOnFailure(t *testing.T, output *readyOutput) {
	t.Helper()
	first, _, _ := strings.Cut(output.written.String(), "\n")
	pid, err := strconv.Atoi(strings.TrimPrefix(first, "child "))
	if err != nil || pid <= 1 {
		t.Fatalf("no child in output %q", output.written.String())
	}
	t.Cleanup(func() {
		if t.Failed() {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// A child that ends after the supervisor relayed an interrupt chose its status
// itself, so the cancellation that relayed it never replaces that status, 0
// included.
func TestARelayedInterruptLeavesTheChildItsOwnStatus(t *testing.T) {
	for _, test := range []struct {
		name, mode, output string
		code               int
	}{
		{name: "a child that finished with its document", mode: "__bootwright_relay_success", output: "ready\n" + `{"exitCode":0}` + "\n", code: 0},
		{name: "a child that reported the interrupt", mode: "__bootwright_relay_interrupted", output: "ready\n", code: 130},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, _, ended := relayInterrupt(t, test.mode, true, relayGrace, nil)
			end := awaitEnding(t, ended, 30*time.Second)
			if end.err != nil || end.code != test.code || output.written.String() != test.output {
				t.Fatalf("status %d, error %v, output %q; want status %d, no error, output %q", end.code, end.err, output.written.String(), test.code, test.output)
			}
		})
	}
}

// The elevated child bounds its own cancellation, which can outlast the stream
// grace, so the supervisor waits for it and keeps the status it chose.
func TestARelayedChildWhoseCleanupOutlastsTheGraceKeepsItsStatus(t *testing.T) {
	output, relayed, ended := relayInterrupt(t, "__bootwright_relay_slow_cleanup", true, 300*time.Millisecond, nil)
	end := awaitEnding(t, ended, 30*time.Second)
	elapsed := time.Since(relayed)
	if end.err != nil || end.code != 0 || !strings.Contains(output.written.String(), `{"exitCode":0}`) || elapsed < 1500*time.Millisecond {
		t.Fatalf("status %d, error %v, output %q after %v; want status 0, no error and the document after the cleanup", end.code, end.err, output.written.String(), elapsed)
	}
}

// Only a second operator signal kills the elevated command: the supervisor
// sets no deadline of its own, however long the child takes after the relay.
func TestASecondOperatorSignalKillsTheRelayedChild(t *testing.T) {
	escalate := make(chan struct{})
	output, _, ended := relayInterrupt(t, "__bootwright_relay_stubborn", true, 300*time.Millisecond, escalate)
	killStubbornChildOnFailure(t, output)
	select {
	case end := <-ended:
		t.Fatalf("the relayed child ended before a second signal: %d, %v", end.code, end.err)
	case <-time.After(500 * time.Millisecond):
	}
	close(escalate)
	if end := awaitEnding(t, ended, 10*time.Second); end.err != nil || end.code != 137 {
		t.Fatalf("status %d, error %v; want 137 after the kill", end.code, end.err)
	}
}

// The policy probe and the refreshes keep their kill deadline: a bounded call
// that ignores the relayed signal is killed once the grace ends.
func TestABoundedProbeIsStillKilledAfterItsGrace(t *testing.T) {
	const grace = 300 * time.Millisecond
	output, relayed, ended := relayInterrupt(t, "__bootwright_relay_stubborn", false, grace, nil)
	killStubbornChildOnFailure(t, output)
	end := awaitEnding(t, ended, 10*time.Second)
	if elapsed := time.Since(relayed); end.err != nil || end.code != 137 || elapsed < grace {
		t.Fatalf("status %d, error %v after %v; want 137 once the grace ended", end.code, end.err, elapsed)
	}
}

// A child that exits 0 after the relay while a process it started still holds
// its standard output when the grace after its exit ends may have lost output,
// so it stays a run error.
func TestARelayedChildWhoseOutputOutlivesTheGraceFails(t *testing.T) {
	output, _, ended := relayInterrupt(t, "__bootwright_relay_orphaned", true, 2*time.Second, nil)
	end := awaitEnding(t, ended, 30*time.Second)
	first, _, _ := strings.Cut(output.written.String(), "\n")
	if holder, parsed := strconv.Atoi(strings.TrimPrefix(first, "holder ")); parsed == nil && holder > 1 {
		syscall.Kill(holder, syscall.SIGKILL)
	} else {
		t.Errorf("no holder in output %q", output.written.String())
	}
	if end.code != 1 || end.err == nil {
		t.Fatalf("status %d, error %v; want a run error", end.code, end.err)
	}
}

func TestGuardedCommandDiesAndIsReapedAfterParentExit(t *testing.T) {
	const getSubreaper, setSubreaper = 37, 36
	var previous int32
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, getSubreaper, uintptr(unsafe.Pointer(&previous)), 0, 0, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	_, _, errno = syscall.Syscall6(syscall.SYS_PRCTL, setSubreaper, 1, 0, 0, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer syscall.Syscall6(syscall.SYS_PRCTL, setSubreaper, uintptr(previous), 0, 0, 0, 0)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, self, "__bootwright_guard_parent")
	parent.WaitDelay = time.Second
	input, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := 0
	defer func() {
		if parent.ProcessState == nil {
			parent.Process.Kill()
			parent.Wait()
		}
		if childPID != 0 {
			syscall.Kill(childPID, syscall.SIGKILL)
			var status syscall.WaitStatus
			syscall.Wait4(childPID, &status, 0, nil)
		}
	}()
	scanner := bufio.NewScanner(output)
	ready := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "ready" {
			ready = true
		}
		if strings.HasPrefix(line, "pid ") {
			childPID, _ = strconv.Atoi(strings.TrimPrefix(line, "pid "))
		}
		if ready && childPID > 0 {
			break
		}
	}
	if !ready || childPID <= 0 {
		t.Fatal("guard fixture did not start")
	}
	if _, err := input.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	input.Close()
	if err := parent.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(childPID, &status, syscall.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid == childPID {
			childPID = 0
			if !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("child status %v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("guarded child survived its parent")
}

func TestGuardRejectsChangedParent(t *testing.T) {
	if release, err := GuardParent(os.Getppid() + 1); err == nil {
		release()
		t.Fatal("incorrect parent accepted")
	}
}

func TestReexecutionPathPinsRunningExecutable(t *testing.T) {
	path, err := ReexecutionPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/proc/"+strconv.Itoa(os.Getpid())+"/exe" {
		t.Fatal("reexecution is not bound to the supervisor")
	}
	selected, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Stat("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(selected, self) {
		t.Fatal("reexecution selected another executable")
	}
}

// The unprivileged supervisor resolves the executable's path before it
// elevates. A directory on that path it cannot search refuses naming the
// invoking account, never root, and the local copy, and the elevation reports
// that refusal before it reaches sudo.
func TestAnUnreadableExecutablePathNamesTheLocalCopyRemedy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root overrides directory permissions")
	}
	directory := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "bootwright")
	if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(directory, 0o700) })
	_, err := verifiedExecutable(path)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "runtime.privilege" || !strings.Contains(reported[0].Message, path) ||
		!strings.HasPrefix(reported[0].Message, "the invoking account cannot resolve the Bootwright executable") ||
		strings.Contains(reported[0].Message, "root cannot") ||
		!strings.Contains(reported[0].Remediation, "both the invoking account and root can read") ||
		!strings.Contains(reported[0].Remediation, "/usr/local/bin") {
		t.Fatalf("unreadable executable = %v (%+v)", err, reported)
	}
	reached := false
	outcome := Elevator{
		Executable: func() (string, error) { return verifiedExecutable(path) },
		Sudo:       func() (string, error) { reached = true; return "/usr/bin/sudo", nil },
	}.Run(context.Background(), Invocation{Arguments: []string{"status"}, Output: io.Discard, Error: io.Discard})
	if reached || outcome.ExitCode != 1 || outcome.Diagnostic == nil || *outcome.Diagnostic != reported[0] {
		t.Fatalf("the elevation reported %#v (sudo reached: %t), want %#v", outcome.Diagnostic, reached, reported[0])
	}
}

// Run with a statically linked test binary in a private user/mount namespace
// and BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE=1. Ordinary root test runs do not
// imply the mount capabilities or isolation this fixture requires.
// The fixture has its own account database and verified sudo-parent executable;
// no real account, home, sudo policy, credential cache or state store is changed.
func TestRootManualSudoAccountProvenanceFixture(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE") != "1" {
		t.Skip("requires explicit privileged account fixture opt-in and an isolated root user/mount namespace")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"etc", "usr/bin", "proc"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{
		"passwd": "root:x:0:0::/root:/bin/sh\noperator:x:60001:60002::/home/operator:/bin/sh\n",
		"group":  "root:x:0:\noperator:x:60002:\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "etc", name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sudo", "launcher"} {
		if err := os.WriteFile(filepath.Join(root, "usr/bin", name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	proc := filepath.Join(root, "proc")
	if err := syscall.Mount("/proc", proc, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(proc, syscall.MNT_DETACH)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, test := range []struct {
		name, executable, mode string
		user                   bool
		deny                   bool
	}{
		{"manual sudo", "sudo", "__bootwright_account_parent", false, false},
		{"untrusted parent", "launcher", "__bootwright_account_root_shell", false, false},
		{"a re-execution whose sudo is gone", "launcher", "__bootwright_account_reexecution", false, true},
		{"direct root", "sudo", "__bootwright_account_root", false, false},
		{"nonroot spoofed metadata", "sudo", "__bootwright_account_user", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, "/usr/bin/"+test.executable, test.mode)
			command.SysProcAttr = &syscall.SysProcAttr{Chroot: root}
			command.Env = []string{"LANG=C", "HOME=/incorrect"}
			if test.user {
				command.SysProcAttr.Credential = &syscall.Credential{Uid: 60001, Gid: 60002, Groups: []uint32{60002}}
				command.Env = append(command.Env, "SUDO_UID=0", "SUDO_GID=0", "SUDO_USER=root")
			}
			output, err := command.CombinedOutput()
			if test.deny {
				if err == nil {
					t.Fatal("the metadata of a re-execution whose sudo is gone was accepted")
				}
				return
			}
			if err != nil || string(output) != "verified\n" {
				t.Fatalf("account provenance fixture: %v %q", err, output)
			}
		})
	}
}
