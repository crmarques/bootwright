//go:build linux && amd64

package ansiblerunner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
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
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	case "orphaning":
		leaveAnOrphan(result, authorization)
	case "retaining":
		retainTheOutput(result, authorization)
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
				Context: "lab", Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
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

// An adapter failure its own output explains points where the request says
// that output is named, because only the caller knows: an attempt beside its
// log, a bounded run in its own file, and a run that names none nowhere. Every
// failure an adapter can provoke does so: a failed exit, one whose descendant
// holds the channel until the drain closes it, an exit without a result, a
// malformed record and a record out of order. The drain's close is the
// runner's own, so the failed exit stays the failure.
func TestAnAdapterFailureNamesTheOutputItsRequestNames(t *testing.T) {
	child := func(mode string) func() *exec.Cmd {
		return func() *exec.Cmd {
			return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-"+mode)
		}
	}
	for _, failed := range []struct {
		name    string
		adapter func() *exec.Cmd
		message string
	}{
		{"failed-exit", func() *exec.Cmd { return exec.Command("/bin/sh", "-c", "exit 3") }, "the adapter operation did not complete"},
		{"failed-exit-retained", func() *exec.Cmd {
			return exec.Command("/bin/sh", "-c", "(exec >/dev/null 2>&1 4<&-; sleep 5) & exit 3")
		}, "the adapter operation did not complete"},
		{"no-result", func() *exec.Cmd { return exec.Command("/bin/sh", "-c", "exit 0") }, "the adapter operation has no complete result"},
		{"malformed", child("malformed"), "the adapter structured result was incomplete"},
		{"out-of-order", child("out-of-order"), "the adapter capability protocol was invalid"},
	} {
		for index, remediation := range []string{
			"read the adapter output retained beside this attempt's log",
			"read the adapter output retained in this run's run.output",
			"",
		} {
			t.Run(fmt.Sprintf("%s/%d", failed.name, index), func(t *testing.T) {
				runner := sweepingRunner(t, failed.adapter)
				var output bytes.Buffer
				request := adapterRequest(t, &output)
				request.OutputRemediation = remediation
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := runner.Run(ctx, request)
				if ctx.Err() != nil {
					t.Fatalf("the failed adapter ran until the deadline (%v)", err)
				}
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Message != failed.message || reported[0].Remediation != remediation {
					t.Fatalf("a failed adapter under %q reported %+v (%v)", remediation, reported, err)
				}
			})
		}
	}
}

// An adapter names a refusal its caller remedies by name, then fails. The run
// fails with the caller's own diagnostic for it, which carries the object,
// reason and remedy, whether the refusal is read before or after the failed
// exit, and the adapter is left to print what it refused into its retained
// output rather than stopped. A reason its caller does not name, a refusal
// before the handoff, or a malformed record breaks the protocol instead, and
// the attempt's outcome is unknown in either order too.
func TestANamedRefusalFailsTheRunWithItsCallersDiagnostic(t *testing.T) {
	named := diagnostics.NewFailureWithRemediation("lifecycle.state", "the machine answers as another system", "", "correct its address")
	const handoff = `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `
	// readFirst holds the adapter past its record so the runner reads the
	// record first; readAfter leaves the record to a descendant that writes it
	// once the adapter has exited, so the failed exit is read first.
	readFirst := func(record string) string { return `printf '` + record + `\n' >&3; sleep 0.2; exit 2` }
	readAfter := func(record string) string {
		return `(exec >/dev/null 2>&1; sleep 0.05; printf '` + record + `\n' >&3) & exec 3>&- 4<&-; exit 2`
	}
	const (
		unnamed = `{"phase":"refused","reason":"release-stamp"}`
		refusal = `{"phase":"refused","reason":"identity-mismatch"}`
		invalid = "the adapter capability protocol was invalid"
	)
	for _, test := range []struct {
		name, script, message, printed string
	}{
		{
			name:    "before the exit",
			script:  handoff + `printf '{"phase":"refused","reason":"identity-mismatch"}\n' >&3; sleep 0.2; echo printed after the refusal; exit 2`,
			message: "the machine answers as another system", printed: "printed after the refusal\n",
		},
		{name: "after the exit", script: handoff + readAfter(refusal), message: "the machine answers as another system"},
		{name: "a reason its caller does not name before the exit", script: handoff + readFirst(unnamed), message: invalid},
		{name: "a reason its caller does not name after the exit", script: handoff + readAfter(unnamed), message: invalid},
		{name: "before the handoff and the exit", script: readFirst(refusal), message: invalid},
		{name: "before the handoff and after the exit", script: readAfter(refusal), message: invalid},
		{name: "a malformed record after the exit", script: handoff + readAfter("not a record"), message: "the adapter structured result was incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := sweepingRunner(t, func() *exec.Cmd { return exec.Command("/bin/sh", "-c", test.script) })
			// A record written after the exit reaches the runner before the
			// drain closes the channel, on a loaded machine too.
			runner.drain = resultDrain
			var output bytes.Buffer
			request := adapterRequest(t, &output)
			request.Refusals = map[string]error{"identity-mismatch": named}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := runner.Run(ctx, request)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Message != test.message {
				t.Fatalf("the run reported %+v (%v), want %q", reported, err, test.message)
			}
			if test.message == "the machine answers as another system" && !reflect.DeepEqual(reported, diagnostics.Of(named)) {
				t.Fatalf("the run reported %+v, want the caller's own %+v", reported, diagnostics.Of(named))
			}
			if outcome := lifecycle.AttemptOutcome(err); test.message != "the machine answers as another system" && outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("a record the runner refuses left the attempt %s (%v)", outcome, err)
			}
			if test.printed != "" && output.String() != test.printed {
				t.Fatalf("the adapter printed %q after its refusal, want %q", output.String(), test.printed)
			}
		})
	}
}

// A record the adapter wrote before its failed exit can still wait for the
// runner when the drain closes the channel a descendant holds. The runner
// judges it as if it had read it first, whichever of it and the drain it takes
// first: the named refusal gives the caller's own diagnostic, a reason its
// caller does not name breaks the protocol, and a malformed record leaves the
// result incomplete. Only the read that the drain's own close ends leaves the
// failed exit. The first group record holds the runner until the adapter is
// reaped, so the rest wait beside the failed exit and a drain that passes at
// once, and each case runs rounds enough that the drain is taken first.
func TestARecordWaitingAtTheDrainIsJudgedAsIfReadFirst(t *testing.T) {
	named := diagnostics.NewFailureWithRemediation("lifecycle.state", "the machine answers as another system", "", "correct its address")
	const group = `{"group":"boot","phase":"group","status":"running"}\n`
	for _, test := range []struct{ name, record, message string }{
		{"a named refusal", `{"phase":"refused","reason":"identity-mismatch"}`, "the machine answers as another system"},
		{"a reason its caller does not name", `{"phase":"refused","reason":"release-stamp"}`, "the adapter capability protocol was invalid"},
		{"a malformed record", "not a record", "the adapter structured result was incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; printf '` + strings.Repeat(group, 8) + test.record +
				`\n' >&3; (exec >/dev/null 2>&1 4<&-; sleep 5) & exit 2`
			for round := range 6 {
				var adapter *exec.Cmd
				runner := sweepingRunner(t, func() *exec.Cmd {
					adapter = exec.Command("/bin/sh", "-c", script)
					return adapter
				})
				runner.drain = time.Nanosecond
				var output bytes.Buffer
				request := adapterRequest(t, &output)
				request.Refusals = map[string]error{"identity-mismatch": named}
				held := false
				request.Progress = func(context.Context, string, string) {
					if held {
						return
					}
					held = true
					for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
						if _, err := os.Stat(fmt.Sprintf("/proc/%d", adapter.Process.Pid)); errors.Is(err, os.ErrNotExist) {
							break
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, err := runner.Run(ctx, request)
				cancel()
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Message != test.message {
					t.Fatalf("round %d reported %+v (%v), want %q", round, reported, err, test.message)
				}
				if test.message == "the machine answers as another system" && !reflect.DeepEqual(reported, diagnostics.Of(named)) {
					t.Fatalf("round %d reported %+v, want the caller's own %+v", round, reported, diagnostics.Of(named))
				}
				if outcome := lifecycle.AttemptOutcome(err); test.message != "the machine answers as another system" && outcome != reconciliation.OutcomeUnknown {
					t.Fatalf("round %d left the attempt %s (%v)", round, outcome, err)
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
				Context: "lab", Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
				Bundle: prerequisites.BundleLocation{Path: bundle}, Output: output,
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			job, scratch := t.TempDir(), t.TempDir()
			ended := make(chan error, 1)
			go func() {
				_, err := runner.execute(ctx, job, scratch, nil, "apply.yml", request)
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
		Context: "lab", Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: t.TempDir()},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Only the invocation this builds is under test.
	_, _ = runner.execute(ctx, t.TempDir(), t.TempDir(), nil, "apply.yml", request)
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
		Context: "lab", Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
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
				`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`)
		},
	}
	request := lifecycle.RunRequest{
		Context: "lab", Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
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
		if _, err := runner.execute(context.Background(), job, scratch, nil, "apply.yml", request); err != nil {
			t.Fatalf("a thread ending elsewhere in the invocation signaled its adapter: %v", err)
		}
	}
}

// lifecycleRunOf runs one shell script as the adapter and returns the run's
// result and error.
func lifecycleRunOf(t *testing.T, script string) (lifecycle.RunResult, error) {
	t.Helper()
	runner := sweepingRunner(t, func() *exec.Cmd { return exec.Command("/bin/sh", "-c", script) })
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, adapterRequest(t, &output))
	if ctx.Err() != nil {
		t.Fatalf("the adapter ran until the deadline (%v)", err)
	}
	return result, err
}

// A record the collection's canonical writer never emits, such as one spaced
// after its colon, breaks the protocol: the attempt is unknown, never failed.
func TestALifecycleRunEndsUnknownOnANonCanonicalRecord(t *testing.T) {
	_, err := lifecycleRunOf(t, `printf '{"phase": "loaded"}\n' >&3; read -r reply <&4; exit 2`)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unknown" || reported[0].Message != "the adapter structured result was incomplete" {
		t.Fatalf("a non-canonical record reported %+v (%v)", reported, err)
	}
	if outcome := lifecycle.AttemptOutcome(err); outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a non-canonical record left the attempt %s", outcome)
	}
}

// A completion proves its postcondition through evidence, so one whose
// evidence is null is no result, even when the adapter exits cleanly.
func TestALifecycleCompletionWithNullEvidenceEndsUnknown(t *testing.T) {
	result, err := lifecycleRunOf(t, `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`printf '{"evidence":null,"outcome":"changed","phase":"completed"}\n' >&3; exit 0`)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unknown" || reported[0].Message != "the adapter structured result was incomplete" {
		t.Fatalf("a completion with null evidence reported %+v (%v)", reported, err)
	}
	if result.Outcome != "" || result.Evidence != nil {
		t.Fatalf("a completion with null evidence completed the run: %+v", result)
	}
}

// A named refusal is the adapter's last record, so the runner closes the
// acknowledgement channel: an adapter still waiting on it is released instead
// of holding the run to its deadline, and nothing is killed for it.
func TestANamedRefusalClosesTheAcknowledgementChannel(t *testing.T) {
	named := diagnostics.NewFailureWithRemediation("lifecycle.state", "the machine answers as another system", "", "correct its address")
	runner := sweepingRunner(t, func() *exec.Cmd {
		return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
			`printf '{"phase":"refused","reason":"identity-mismatch"}\n' >&3; read -r reply <&4; echo released; exit 2`)
	})
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Refusals = map[string]error{"identity-mismatch": named}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runner.Run(ctx, request)
	if ctx.Err() != nil {
		t.Fatalf("the refusing adapter waited on its acknowledgement channel until the deadline (%v)", err)
	}
	if !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(named)) || output.String() != "released\n" {
		t.Fatalf("the run reported %+v and the adapter printed %q", diagnostics.Of(err), output.String())
	}
}

// An SSH placement's job holds a client configuration of its own that keeps
// only the host crypto policy, private to root; a local placement runs no ssh
// and its job holds none.
func TestAnSSHPlacementsJobHoldsItsOwnClientConfiguration(t *testing.T) {
	job := t.TempDir()
	paths := map[string]string{}
	if err := writeSSHConfig(job, sshRequest(), paths); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(job, "ssh_config")
	if paths["ssh_config"] != target {
		t.Fatalf("ssh_config path = %q", paths["ssh_config"])
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("ssh_config = %v (%v)", info, err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "Include /etc/crypto-policies/back-ends/openssh.config\n" {
		t.Fatalf("ssh_config content = %q (%v)", content, err)
	}
	local := t.TempDir()
	paths = map[string]string{}
	if err := writeSSHConfig(local, localRequest(), paths); err != nil {
		t.Fatal(err)
	}
	if _, present := paths["ssh_config"]; present {
		t.Fatal("a local placement was given an ssh configuration")
	}
	if entries, _ := os.ReadDir(local); len(entries) != 0 {
		t.Fatalf("a local placement's job holds %v", entries)
	}
}

// A run on an SSH placement hands its adapter a job holding that client
// configuration, and an inventory whose SSH arm reads it first.
func TestARunOnAnSSHPlacementHandsItsAdapterTheGeneratedConfiguration(t *testing.T) {
	capture := t.TempDir()
	runner := sweepingRunner(t, nil)
	runner.command = func(_ string, arguments ...string) *exec.Cmd {
		job := ""
		if at := slices.Index(arguments, "-i"); at >= 0 && at+1 < len(arguments) {
			job = filepath.Dir(arguments[at+1])
		}
		return exec.Command("/bin/sh", "-c", `cp -p "$1/ssh_config" "$1/inventory.json" "$2/" || exit 9; `+
			`printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
			`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`, "sh", job, capture)
	}
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	placement := sshRequest().Placement
	request.Placement = placement
	request.Materials = lifecycle.Materials(placement)
	request.Material = sshRequest().Material
	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatalf("the run on an SSH placement failed: %v (%s)", err, output.String())
	}
	info, err := os.Stat(filepath.Join(capture, "ssh_config"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("the job's ssh_config = %v (%v)", info, err)
	}
	if content, err := os.ReadFile(filepath.Join(capture, "ssh_config")); err != nil || string(content) != "Include /etc/crypto-policies/back-ends/openssh.config\n" {
		t.Fatalf("the job's ssh_config content = %q (%v)", content, err)
	}
	encoded, err := os.ReadFile(filepath.Join(capture, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		All struct {
			Children struct {
				Target struct {
					Hosts map[string]map[string]any `json:"hosts"`
				} `json:"bootwright_target"`
			} `json:"children"`
		} `json:"all"`
	}
	if err := json.Unmarshal(encoded, &written); err != nil {
		t.Fatal(err)
	}
	arguments, _ := written.All.Children.Target.Hosts[placement.Machine]["ansible_ssh_common_args"].(string)
	fields := strings.Fields(arguments)
	if len(fields) < 2 || fields[0] != "-F" || filepath.Base(fields[1]) != "ssh_config" || !strings.HasPrefix(filepath.Base(filepath.Dir(fields[1])), jobPrefix) {
		t.Fatalf("the SSH arm's arguments do not start with the job's configuration: %q", arguments)
	}
}

// retainTheOutput completes the protocol, closes its result channel and leaves
// a descendant in a session of its own holding the adapter's own output, then
// exits zero. It records the descendant's process ID for the test to end.
func retainTheOutput(result, authorization *os.File) {
	_, _ = result.Write([]byte(`{"phase":"loaded"}` + "\n"))
	if line, _ := bufio.NewReader(authorization).ReadString('\n'); line != "proceed\n" {
		os.Exit(19)
	}
	_, _ = result.Write([]byte(`{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}` + "\n"))
	_ = result.Close()
	_ = authorization.Close()
	holder := exec.Command("/bin/sleep", "5")
	holder.Stdout = os.Stdout
	holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if holder.Start() != nil {
		os.Exit(20)
	}
	_ = os.WriteFile(os.Getenv("BOOTWRIGHT_TEST_HOLDER"), []byte(strconv.Itoa(holder.Process.Pid)), 0600)
	os.Exit(0)
}

// endHolder kills the descendant whose process ID the adapter recorded.
func endHolder(t *testing.T, path string) {
	recorded, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(string(recorded)); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// An adapter that answers the acknowledgement channel by closing it lets no
// acknowledgement through, so nothing was authorized, and the outcome is its
// exit's whichever of the two the runner reads first: a failed exit stays the
// failed exit. Read first, the record meets a channel no process holds; read
// after, a descendant that holds only the result channel writes it once the
// adapter has exited. An adapter that lingers after closing the channel is
// signaled at once, and like a supervisor ends its descendant and exits, well
// inside both its own sleep and the drain.
func TestAnAcknowledgementAfterAFailedExitKeepsThatExit(t *testing.T) {
	for _, check := range []struct{ name, script string }{
		{"record first", `exec 4<&-; printf '{"phase":"loaded"}\n' >&3; sleep 0.3; exit 3`},
		{"exit first", `exec 4<&-; /bin/sh -c 'sleep 0.3; printf "%s\n" "$0" >&3' '{"phase":"loaded"}' </dev/null >/dev/null 2>&1 & exit 3`},
		{"record first, adapter lingers", `exec 4<&-; trap 'kill $!; exit 3' TERM; printf '{"phase":"loaded"}\n' >&3; sleep 3 & wait; exit 3`},
	} {
		t.Run(check.name, func(t *testing.T) {
			runner := sweepingRunner(t, func() *exec.Cmd { return exec.Command("/bin/sh", "-c", check.script) })
			runner.drain = 2 * time.Second
			var output bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started := time.Now()
			_, err := runner.Run(ctx, adapterRequest(t, &output))
			if ctx.Err() != nil {
				t.Fatalf("the adapter ran until the deadline (%v)", err)
			}
			if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
				t.Fatalf("an undelivered acknowledgement left the adapter running for %s", elapsed)
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != "the adapter operation did not complete" {
				t.Fatalf("an acknowledgement nothing could receive reported %+v (%v)", reported, err)
			}
		})
	}
}

// A descendant in a session of its own that still holds the adapter's output
// after the adapter exits is cut at the drain: the run returns at its bound,
// and the completed record it read is no result.
func TestARunReturnsAtItsDrainWhenADescendantHoldsItsOutput(t *testing.T) {
	holder := filepath.Join(t.TempDir(), "holder")
	t.Cleanup(func() { endHolder(t, holder) })
	runner := sweepingRunner(t, func() *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-retaining")
	})
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Launch.Environment = []string{"BOOTWRIGHT_TEST_HOLDER=" + holder}
	request.OutputRemediation = "read the adapter output retained beside this attempt's log"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	result, err := runner.Run(ctx, request)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the run waited %s on a descendant holding its output", elapsed)
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unknown" || reported[0].Message != "adapter descendants retained the adapter's output after it exited" || reported[0].Remediation != request.OutputRemediation {
		t.Fatalf("retained output reported %+v (%v)", reported, err)
	}
	if result.Outcome != "" {
		t.Fatalf("retained output completed the run: %+v", result)
	}
	if _, err := os.Stat(holder); err != nil {
		t.Fatalf("the adapter left no descendant holding its output: %v", err)
	}
}
