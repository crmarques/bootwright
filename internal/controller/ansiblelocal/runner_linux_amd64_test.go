//go:build linux && amd64

package ansiblelocal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestRunnerProtocolChild(t *testing.T) {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[len(os.Args)-1], "controller-child-") {
		return
	}
	mode := strings.TrimPrefix(os.Args[len(os.Args)-1], "controller-child-")
	output, input := os.NewFile(3, "result"), bufio.NewReader(os.NewFile(4, "authorization"))
	emit := func(value any, acknowledge bool) {
		data, _ := json.Marshal(value)
		_, _ = output.Write(append(data, '\n'))
		if acknowledge {
			line, _ := input.ReadString('\n')
			if line != "proceed\n" {
				os.Exit(19)
			}
		}
	}
	sha := strings.Repeat("a", 64)
	if mode == "hang" {
		// Completes the load handshake, then waits. Nothing but a reaped
		// process group ends this child.
		emit(map[string]any{"phase": "loaded"}, true)
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode == "leak" {
		emit(map[string]any{"phase": "loaded"}, true)
		emit(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AddedSources: []string{}}}, true)
		emit(map[string]any{"phase": "completed", "outcome": "unchanged", "evidence": map[string]any{"request": sha, "before": sha, "after": sha, "planDigest": "", "added": []string{}, "tools": []string{}, "postcondition": true}}, false)
		// A descendant inherits the result channel and outlives this process,
		// so the channel never reaches end of file on its own.
		descendant := exec.Command("/bin/sleep", "3")
		descendant.ExtraFiles = []*os.File{output}
		_ = descendant.Start()
		os.Exit(0)
	}
	emit(map[string]any{"phase": "loaded"}, true)
	if mode == "refused" || mode == "refused-native" {
		if mode == "refused-native" {
			emit(map[string]any{"phase": "native"}, true)
		}
		_, _ = output.Write([]byte("not a record\n"))
		if mode == "refused-native" {
			// The authorized transaction is still running when the record is
			// refused, and it must be allowed to finish.
			time.Sleep(300 * time.Millisecond)
			_ = os.WriteFile(os.Getenv("BOOTWRIGHT_TEST_TRANSACTION"), []byte("done\n"), 0600)
		}
		// Only a closed authorization channel answers this wait.
		_, _ = input.ReadString('\n')
		os.Exit(19)
	}
	if mode == "recover-native" {
		emit(map[string]any{"phase": "native"}, true)
		emit(map[string]any{"phase": "completed", "outcome": "changed", "evidence": map[string]any{"request": sha, "before": sha, "after": strings.Repeat("b", 64), "planDigest": strings.Repeat("c", 64), "added": []string{"native-one"}, "tools": []string{}, "postcondition": true}}, false)
		os.Exit(0)
	}
	if mode == "mixed" {
		emit(map[string]any{"phase": "continue", "evidence": map[string]bool{}}, false)
		os.Exit(0)
	}
	emit(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AddedSources: []string{}}}, true)
	fmt.Fprintln(os.Stderr, "private-child-diagnostic")
	emit(map[string]any{"phase": "completed", "outcome": "unchanged", "evidence": map[string]any{"request": sha, "before": sha, "after": sha, "planDigest": "", "added": []string{}, "tools": []string{}, "postcondition": true}}, false)
	os.Exit(0)
}

func runnerFixture(t *testing.T, mode string) (prerequisites.PythonLaunch, capabilityRequest, processBoundary) {
	t.Helper()
	bundle := t.TempDir()
	if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
		t.Fatal(err)
	}
	launch := prerequisites.PythonLaunch{Loader: "/qualified/loader", Arguments: []string{"--inhibit-cache", "/qualified/python"}, Directory: bundle, Environment: []string{"LANG=C.UTF-8"}}
	request := capabilityRequest{Operation: "setup", Identity: strings.Repeat("a", 64), Bundle: bundleLocation{Path: bundle, Writable: true}, Packages: []prerequisites.NativePackage{}, Tools: []prerequisites.ToolDefinition{}}
	boundary := processBoundary{owner: os.Geteuid(), jobParent: t.TempDir(), scratchParent: t.TempDir(), command: func(path string, arguments ...string) *exec.Cmd {
		// -u belongs to the boundary: without it the child holds its output
		// back until it exits, and nothing can be followed while it runs.
		if path != launch.Loader || !strings.Contains(strings.Join(arguments, "\x00"), "-u\x00-I\x00-B\x00-S\x00-c") {
			t.Fatal("qualified isolated Python boundary changed")
		}
		return exec.Command(os.Args[0], "-test.run=^TestRunnerProtocolChild$", "--", "controller-child-"+mode)
	}}
	return launch, request, boundary
}

func TestRunnerDrainsCompletionAfterChildExit(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	released, published := false, false
	result, err := runProcess(ctx, launch, request, func() error { released = true; return nil }, func(ctx context.Context, value prerequisites.NativePreparation) error {
		if !released {
			t.Error("preparation preceded loaded handoff")
		}
		published = true
		return nil
	}, nil, nil, boundary)
	if err != nil || result.Outcome != "unchanged" || !released || !published {
		t.Fatalf("completion lost: %s %v", result.Outcome, err)
	}
	entries, err := os.ReadDir(boundary.jobParent)
	if err != nil || len(entries) != 0 {
		t.Fatal("invocation files retained")
	}
}

// Every protocol phase names the work Ansible is about to do, so the native
// transaction and each tool transfer are visible instead of one silent action.
func TestRunnerReportsProtocolPhasesAsProgress(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var details []string
	progress := func(event prerequisites.ProgressEvent) { details = append(details, event.Status+": "+event.Detail) }
	if _, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, progress, nil, boundary); err != nil {
		t.Fatal(err)
	}
	if want := []string{"running: starting the private Ansible runtime", "running: reading the native package inventory"}; !slices.Equal(details, want) {
		t.Fatalf("progress = %q, want %q", details, want)
	}
	launch, request, boundary = runnerFixture(t, "recover-native")
	request.Operation = "recover"
	request.Native = &prerequisites.NativeResolvedPlan{Digest: strings.Repeat("c", 64), BeforeSHA256: strings.Repeat("a", 64), AfterSHA256: strings.Repeat("b", 64), Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}
	transitions, err := prerequisites.NativeTransitionsDigest(request.Native.Actions)
	if err != nil {
		t.Fatal(err)
	}
	request.Preparation = &prerequisites.NativePreparation{InventorySHA256: request.Native.BeforeSHA256, AfterInventorySHA256: request.Native.AfterSHA256, PlanDigest: request.Native.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}
	details = nil
	if _, err := runProcess(ctx, launch, request, func() error { return nil }, nil, progress, nil, boundary); err != nil {
		t.Fatal(err)
	}
	if want := []string{"running: starting the private Ansible runtime", "running: verifying the recorded native transaction", "running: installing 1 native package"}; !slices.Equal(details, want) {
		t.Fatalf("recovery progress = %q, want %q", details, want)
	}
}

func TestRunnerRefusesCancellationAtDurablePreparation(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { cancel(); return nil }, nil, nil, boundary)
	if !errors.Is(err, context.Canceled) || result.Outcome != "unknown" {
		t.Fatalf("cancellation lost durable intent: %s %v", result.Outcome, err)
	}
}

func TestRunnerSanitizesInvalidProtocol(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "mixed")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error {
		t.Error("invalid phase published intent")
		return nil
	}, nil, nil, boundary)
	if err == nil || result.Outcome != "failed" || strings.Contains(err.Error(), "private-child-diagnostic") {
		t.Fatalf("invalid protocol accepted: %s %v", result.Outcome, err)
	}
}

func TestRunnerAcknowledgesFrozenNativeRecoveryWithoutNewPreparation(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "recover-native")
	request.Operation = "recover"
	request.Native = &prerequisites.NativeResolvedPlan{Digest: strings.Repeat("c", 64), BeforeSHA256: strings.Repeat("a", 64), AfterSHA256: strings.Repeat("b", 64), Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}
	transitions, err := prerequisites.NativeTransitionsDigest(request.Native.Actions)
	if err != nil {
		t.Fatal(err)
	}
	request.Preparation = &prerequisites.NativePreparation{InventorySHA256: request.Native.BeforeSHA256, AfterInventorySHA256: request.Native.AfterSHA256, PlanDigest: request.Native.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, nil, nil, nil, boundary)
	if err != nil || result.Outcome != "changed" {
		t.Fatalf("frozen native recovery failed: %s %v", result.Outcome, err)
	}
}

// Cancellation before an authorized native transaction must reap the process
// group. A recovery run begins with durable intent already recorded, so intent
// must not be mistaken for installation in progress.
func TestRunnerReapsUnauthorizedChildOnCancellationDuringRecovery(t *testing.T) {
	for _, operation := range []string{"setup", "recover"} {
		t.Run(operation, func(t *testing.T) {
			launch, request, boundary := runnerFixture(t, "hang")
			request.Operation = operation
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Cancel only after the child is past its handshake, so the closed
			// authorization pipe cannot end it instead of the reap.
			go func() { time.Sleep(500 * time.Millisecond); cancel() }()
			started := time.Now()
			result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Fatalf("unauthorized child was not reaped: %s took %s", operation, elapsed)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s cancellation = %v (outcome %s)", operation, err, result.Outcome)
			}
		})
	}
}

// A refused record ends the protocol at once: the closed authorization channel
// fails an adapter waiting on it instead of leaving it to the ten-minute
// deadline. Nothing is killed, so an authorized native transaction still
// running when its record is refused finishes first.
func TestAProtocolRefusalReleasesAWaitingAdapter(t *testing.T) {
	for name, mode := range map[string]string{"before preparation": "refused", "during a native transaction": "refused-native"} {
		t.Run(name, func(t *testing.T) {
			launch, request, boundary := runnerFixture(t, mode)
			boundary.authorizedDrain, boundary.completedDrain = 200*time.Millisecond, 200*time.Millisecond
			transaction := filepath.Join(t.TempDir(), "transaction")
			launch.Environment = append(launch.Environment, "BOOTWRIGHT_TEST_TRANSACTION="+transaction)
			want := "failed"
			if mode == "refused-native" {
				request.Operation = "recover"
				request.Native = &prerequisites.NativeResolvedPlan{Digest: strings.Repeat("c", 64), BeforeSHA256: strings.Repeat("a", 64), AfterSHA256: strings.Repeat("b", 64), Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}
				transitions, err := prerequisites.NativeTransitionsDigest(request.Native.Actions)
				if err != nil {
					t.Fatal(err)
				}
				request.Preparation = &prerequisites.NativePreparation{InventorySHA256: request.Native.BeforeSHA256, AfterInventorySHA256: request.Native.AfterSHA256, PlanDigest: request.Native.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}
				want = "unknown"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started := time.Now()
			result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
			if elapsed := time.Since(started); ctx.Err() != nil || elapsed > 5*time.Second {
				t.Fatalf("the refused adapter waited %s for an acknowledgement (%v)", elapsed, err)
			}
			if err == nil || result.Outcome != want {
				t.Fatalf("a refused record left the run %s (%v), want %s", result.Outcome, err, want)
			}
			if _, err := os.Stat(transaction); mode == "refused-native" && err != nil {
				t.Fatalf("the authorized native transaction did not finish: %v", err)
			}
		})
	}
}

// Package staging must not land in the ambient temporary directory: the child
// gets private durable scratch that is removed when the run ends.
func TestRunnerStagesInPrivateDurableScratch(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	parent := boundary.scratchParent
	var child *exec.Cmd
	inner := boundary.command
	boundary.command = func(path string, arguments ...string) *exec.Cmd {
		child = inner(path, arguments...)
		return child
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary); err != nil {
		t.Fatal(err)
	}
	var scratch string
	for _, entry := range child.Env {
		if strings.HasPrefix(entry, "TMPDIR=") {
			scratch = strings.TrimPrefix(entry, "TMPDIR=")
		}
	}
	if scratch == "" || filepath.Dir(scratch) != parent {
		t.Fatalf("child TMPDIR = %q, want a directory under %q", scratch, parent)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("staging scratch survived the run: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging parent retained %v (%v)", entries, err)
	}
}

func TestStagingCapacityRefusesBeforeEffects(t *testing.T) {
	scratch := t.TempDir()
	if err := requireScratchCapacity(scratch, capabilityRequest{}); err != nil {
		t.Fatalf("an empty closure was refused: %v", err)
	}
	small := capabilityRequest{Packages: []prerequisites.NativePackage{{Source: prerequisites.DependencySource{Bytes: 1}}}}
	if err := requireScratchCapacity(scratch, small); err != nil {
		t.Fatalf("a one-byte closure was refused: %v", err)
	}
	huge := capabilityRequest{Packages: []prerequisites.NativePackage{{Source: prerequisites.DependencySource{Bytes: 1 << 60}}}}
	if err := requireScratchCapacity(scratch, huge); err == nil {
		t.Fatal("a closure larger than the filesystem was accepted")
	}
	tools := capabilityRequest{Tools: []prerequisites.ToolDefinition{{Source: prerequisites.DependencySource{Bytes: 1 << 60}}}}
	if err := requireScratchCapacity(scratch, tools); err == nil {
		t.Fatal("target client payloads were excluded from the capacity check")
	}
}

// A leaked descendant keeps the result channel open after Ansible exits. The
// run must still end on its own bound instead of waiting for that descendant.
func TestRunnerBoundsDrainWhenDescendantRetainsResultChannel(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "leak")
	boundary.authorizedDrain, boundary.completedDrain = 200*time.Millisecond, 200*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		t.Fatal("run outlived its own drain bound and hit the test deadline")
	}
	var classified *diagnostics.Failure
	if !errors.As(err, &classified) || len(classified.Diagnostics) == 0 || !strings.Contains(classified.Diagnostics[0].Message, "retained the result channel") {
		t.Fatalf("retained channel was not reported: %s %v", result.Outcome, err)
	}
	if result.Outcome != "unknown" {
		t.Fatalf("retained channel claimed a proved outcome: %s", result.Outcome)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("drain bound was not applied: %s", elapsed)
	}
}

// The controller stage runs as one lifecycle block, so what its Ansible prints
// belongs to that block's attempt. A run given no retention discards it rather
// than letting it reach the operator's terminal.
func TestRunnerRetainsWhatAnsiblePrintsWhenGivenSomewhereToPutIt(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var retained strings.Builder
	result, err := runProcess(ctx, launch, request, func() error { return nil },
		func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, &retained, boundary)
	if err != nil || result.Outcome != "unchanged" {
		t.Fatalf("run = %s (%v)", result.Outcome, err)
	}
	if !strings.Contains(retained.String(), "private-child-diagnostic") {
		t.Fatalf("retained = %q, want what the run printed", retained.String())
	}
}
