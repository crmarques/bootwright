//go:build linux && amd64

package ansiblerunner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// TestLifecycleAdapterChild is the adapter these tests start, not a test of its
// own: it acts only when the runner re-executes the test binary with a mode.
func TestLifecycleAdapterChild(t *testing.T) {
	mode, ok := strings.CutPrefix(os.Args[len(os.Args)-1], "lifecycle-child-")
	if !ok {
		return
	}
	result, authorization := os.NewFile(3, "result"), os.NewFile(4, "authorization")
	switch mode {
	case "waiter":
		// Only a closed authorization channel ends this descendant.
		_, _ = io.Copy(io.Discard, authorization)
		os.Exit(0)
	case "member":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "running":
		// Booting nodes with nothing to report yet: only a signal ends it.
		fmt.Printf("adapter %d\n", os.Getpid())
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "supervising", "supervising-refused", "stubborn":
		superviseLikeTheCollection(mode, result)
	}
	// One descendant shares the adapter's process group. The other waits on
	// the authorization channel in a session of its own, beyond a group kill,
	// and holds the adapter's output, so the run cannot end until the runner
	// releases it.
	member := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-member")
	waiter := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-waiter")
	waiter.ExtraFiles = []*os.File{result, authorization}
	waiter.Stdout = os.Stdout
	waiter.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if member.Start() != nil || waiter.Start() != nil {
		os.Exit(20)
	}
	fmt.Printf("member %d\nwaiter %d\n", member.Process.Pid, waiter.Process.Pid)
	record := map[string]string{
		"malformed":    "not a record",
		"out-of-order": `{"group":"boot","phase":"group","status":"running"}`,
	}[mode]
	_, _ = result.Write([]byte(record + "\n"))
	// Booting nodes: nothing but the runner ends this adapter early.
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// superviseLikeTheCollection stands in for the collection's supervisor. It
// runs a member of its own process group and, unless it ignores termination, a
// worker in a session of its own, as an Ansible worker is, beyond any group
// kill. Neither holds a protocol channel, so only a signal ends them. On
// termination it ends the worker, as the supervisor's handler does, but leaves
// its group to the runner. It prints every process ID that must not survive.
func superviseLikeTheCollection(mode string, result *os.File) {
	terminated := make(chan os.Signal, 1)
	if mode == "stubborn" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(terminated, syscall.SIGTERM)
	}
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	member := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-member")
	if member.Start() != nil {
		os.Exit(20)
	}
	fmt.Printf("pid %d\npid %d\n", os.Getpid(), member.Process.Pid)
	worker := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-member")
	worker.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if mode != "stubborn" {
		if worker.Start() != nil {
			os.Exit(20)
		}
		fmt.Printf("pid %d\n", worker.Process.Pid)
	}
	fmt.Println("ready")
	if mode == "supervising-refused" {
		_, _ = result.Write([]byte("not a record\n"))
	}
	select {
	case <-terminated:
		_ = worker.Process.Kill()
		_ = worker.Wait()
	case <-time.After(30 * time.Second):
	}
	os.Exit(0)
}

// embeddedArea is an approved bundle carrying exactly this build's collection.
type embeddedArea struct {
	prerequisites.BundleArea
	files map[string][]byte
}

func (a embeddedArea) Read(_ context.Context, name string, _ int) ([]byte, error) {
	data, ok := a.files[strings.TrimPrefix(name, "automation/")]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

// running reports whether a process matching the filter still runs. A killed
// process stays a zombie until its new parent reaps it; it runs nothing, so it
// does not count.
func running(t *testing.T, matches func(pid, group int) bool) bool {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name is parenthesized and may hold spaces; the state,
		// parent and process group follow it.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 3 || fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		if group, err := strconv.Atoi(fields[2]); err == nil && matches(pid, group) {
			return true
		}
	}
	return false
}

// A refused record ends the protocol at once. The closed authorization channel
// releases a descendant waiting on it even outside the adapter's process group,
// and the group kill stops the adapter, which otherwise keeps booting nodes
// until it exits or the two-hour deadline passes. The attempt stays unknown.
func TestAProtocolRefusalEndsTheAdapterPromptly(t *testing.T) {
	assets := ansible.Assets()
	for _, mode := range []string{"malformed", "out-of-order"} {
		t.Run(mode, func(t *testing.T) {
			bundle := t.TempDir()
			if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
				t.Fatal(err)
			}
			var adapter *exec.Cmd
			runner := Runner{
				jobParent: t.TempDir(), scratchParent: t.TempDir(), drain: 200 * time.Millisecond,
				playbooks: map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"},
				command: func(string, ...string) *exec.Cmd {
					adapter = exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-"+mode)
					return adapter
				},
			}
			var output bytes.Buffer
			request := lifecycle.RunRequest{
				Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
				Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
				Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
				Bundle: prerequisites.BundleLocation{Path: bundle}, Area: embeddedArea{files: assets}, Output: &output,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started := time.Now()
			_, err := runner.Run(ctx, request)
			if elapsed := time.Since(started); ctx.Err() != nil || elapsed > 5*time.Second {
				t.Fatalf("the refused adapter ran on for %s (%v)", elapsed, err)
			}
			if outcome := lifecycle.AttemptOutcome(err); outcome != reconciliation.OutcomeUnknown || err == nil {
				t.Fatalf("a refused record left the attempt %s (%v)", outcome, err)
			}
			var member, waiter int
			if _, err := fmt.Sscanf(output.String(), "member %d\nwaiter %d\n", &member, &waiter); err != nil {
				t.Fatalf("the adapter did not start its descendants: %q (%v)", output.String(), err)
			}
			group := adapter.Process.Pid
			for deadline := time.Now().Add(3 * time.Second); running(t, func(pid, pgid int) bool {
				return pgid == group || pid == member || pid == waiter
			}); time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the refused adapter's process group or its waiting descendant survived the run")
				}
			}
		})
	}
}

// Cancellation and a refused record reach an Ansible worker in a session of
// its own, which no group kill does. The runner signals the adapter first, so
// the supervisor's handler ends that worker, and kills the group once the
// adapter is reaped, or once the drain passes for an adapter that ignored the
// signal. Nothing the adapter started runs on.
func TestStoppingAnAdapterEndsItsTreeBeyondItsGroup(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		cancel     bool
	}{
		{"canceled", "supervising", true},
		{"refused", "supervising-refused", false},
		{"ignoring termination", "stubborn", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle := t.TempDir()
			if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
				t.Fatal(err)
			}
			runner := Runner{
				drain: 500 * time.Millisecond,
				command: func(string, ...string) *exec.Cmd {
					return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-"+test.mode)
				},
			}
			progress, output, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer progress.Close()
			request := lifecycle.RunRequest{
				Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
				Bundle: prerequisites.BundleLocation{Path: bundle}, Output: output,
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			job, scratch := t.TempDir(), t.TempDir()
			ended := make(chan error, 1)
			go func() {
				_, err := runner.execute(ctx, job, scratch, "apply.yml", request)
				output.Close()
				ended <- err
			}()
			var pids []int
			lines := bufio.NewScanner(progress)
			for lines.Scan() && lines.Text() != "ready" {
				var pid int
				if _, err := fmt.Sscanf(lines.Text(), "pid %d", &pid); err == nil {
					pids = append(pids, pid)
				}
			}
			t.Cleanup(func() {
				if t.Failed() {
					for _, pid := range pids {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			if lines.Text() != "ready" || len(pids) < 2 {
				cancel()
				t.Fatalf("the adapter did not start its descendants: %v (%v)", pids, <-ended)
			}
			if test.cancel {
				cancel()
			}
			var runErr error
			select {
			case runErr = <-ended:
			case <-time.After(5 * time.Second):
				t.Fatal("the stopped adapter held its invocation")
			}
			if test.cancel && !errors.Is(runErr, context.Canceled) {
				t.Fatalf("a canceled run ended with %v", runErr)
			}
			if outcome := lifecycle.AttemptOutcome(runErr); !test.cancel && (outcome != reconciliation.OutcomeUnknown || runErr == nil) {
				t.Fatalf("a refused record left the attempt %s (%v)", outcome, runErr)
			}
			group := pids[0]
			for deadline := time.Now().Add(3 * time.Second); running(t, func(pid, pgid int) bool {
				return pgid == group || slices.Contains(pids, pid)
			}); time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the stopped adapter left a process running")
				}
			}
		})
	}
}

// Only the lifecycle runner passes the marker that ties the supervisor to its
// invocation, and the supervisor reads it only as its first argument. Without
// it, the parent-death signal ends the supervisor alone and leaves the
// playbook and its workers running.
func TestTheSupervisorIsStartedInLifecycleMode(t *testing.T) {
	var arguments []string
	runner := Runner{
		drain: time.Millisecond,
		command: func(_ string, args ...string) *exec.Cmd {
			arguments = args
			return exec.Command("/bin/true")
		},
	}
	request := lifecycle.RunRequest{
		Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: t.TempDir()},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Only the invocation this builds is under test.
	_, _ = runner.execute(ctx, t.TempDir(), t.TempDir(), "apply.yml", request)
	supervisor := slices.IndexFunc(arguments, func(argument string) bool {
		return strings.HasSuffix(argument, "/plugins/module_utils/controller_supervisor.py")
	})
	if supervisor < 0 {
		t.Fatalf("adapter invocation = %q, want the collection's supervisor", arguments)
	}
	if rest := arguments[supervisor+1:]; len(rest) == 0 || rest[0] != "--lifecycle" {
		t.Fatalf("supervisor arguments = %q, want the lifecycle marker first", rest)
	}
}

// TestLifecycleInvocationHelper is the invocation the parent-death test kills,
// not a test of its own: it acts only when that test starts it with an area.
func TestLifecycleInvocationHelper(t *testing.T) {
	area := os.Getenv("BOOTWRIGHT_INVOCATION_AREA")
	if area == "" {
		return
	}
	bundle := filepath.Join(area, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "automation"), 0700); err != nil {
		os.Exit(20)
	}
	runner := Runner{
		jobParent: area, scratchParent: area,
		playbooks: map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"},
		command: func(string, ...string) *exec.Cmd {
			return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-running")
		},
	}
	request := lifecycle.RunRequest{
		Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
		Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
		Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: bundle}, Area: embeddedArea{files: ansible.Assets()}, Output: os.Stdout,
	}
	_, _ = runner.Run(context.Background(), request)
	os.Exit(21)
}

// An invocation killed outright runs none of its own cleanup: the elevated
// child is SIGKILLed when its sudo parent dies. The parent-death signal still
// reaches the adapter, so none runs on into the next invocation.
func TestAKilledInvocationTakesItsAdapter(t *testing.T) {
	invocation := exec.Command(os.Args[0], "-test.run=^TestLifecycleInvocationHelper$")
	invocation.Env = append(os.Environ(), "BOOTWRIGHT_INVOCATION_AREA="+t.TempDir())
	stdout, err := invocation.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Start(); err != nil {
		t.Fatal(err)
	}
	var adapter int
	if _, err := fmt.Fscanf(stdout, "adapter %d\n", &adapter); err != nil {
		_ = invocation.Process.Kill()
		_ = invocation.Wait()
		t.Fatalf("the invocation did not start its adapter: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			_ = syscall.Kill(adapter, syscall.SIGKILL)
		}
	})
	if err := invocation.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = invocation.Wait()
	for deadline := time.Now().Add(5 * time.Second); running(t, func(pid, _ int) bool { return pid == adapter }); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the adapter outlived its killed invocation")
		}
	}
}

// The kernel sends the parent-death signal when the thread that forked the
// adapter ends, and the runtime ends a thread whose locked goroutine exits. A
// start on a thread other goroutines share would let any of them take a
// running adapter down; the runner keeps that thread to itself instead.
func TestThreadChurnNeverSignalsARunningAdapter(t *testing.T) {
	bundle := t.TempDir()
	if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		drain: 200 * time.Millisecond,
		// A shell speaks the protocol and completes: one record, one
		// acknowledgement, one result.
		command: func(string, ...string) *exec.Cmd {
			return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
				`printf '{"phase":"completed","outcome":"changed","evidence":[{}]}\n' >&3`)
		},
	}
	request := lifecycle.RunRequest{
		Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: bundle}, Output: io.Discard,
	}
	job, scratch := t.TempDir(), t.TempDir()
	stop, churned := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(churned)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Each goroutine exits locked, so the runtime ends its thread.
			var group sync.WaitGroup
			for range 8 {
				group.Go(runtime.LockOSThread)
			}
			group.Wait()
		}
	}()
	defer func() { close(stop); <-churned }()
	// The window is at each start, so many short runs expose it.
	for range 100 {
		if _, err := runner.execute(context.Background(), job, scratch, "apply.yml", request); err != nil {
			t.Fatalf("a thread ending elsewhere in the invocation signaled its adapter: %v", err)
		}
	}
}
