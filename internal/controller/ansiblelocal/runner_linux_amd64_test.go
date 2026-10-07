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
	"strconv"
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
	if strings.HasPrefix(mode, "unreleased") {
		emit(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AddedSources: []string{"tool-oc"}}}, true)
		if mode != "unreleased-before-continue" {
			emit(map[string]any{"phase": "continue"}, true)
		}
		records := []string{`{"phase":"refused","reason":"` + os.Getenv("BOOTWRIGHT_TEST_REASON") + `"}`}
		if strings.HasPrefix(mode, "unreleased-after-completed") {
			completed, _ := json.Marshal(map[string]any{"phase": "completed", "outcome": "changed", "evidence": map[string]any{"request": sha, "before": sha, "after": sha, "planDigest": "", "added": []string{}, "tools": []map[string]string{{"source": "tool-oc", "sha256": strings.Repeat("d", 64), "files": strings.Repeat("e", 64)}}, "postcondition": true}})
			records = append([]string{string(completed)}, records...)
		}
		if mode == "unreleased-after-exit" || mode == "unreleased-after-completed-exit-first" {
			// A descendant writes the records after this process has exited,
			// so the runner reads the failed exit first.
			descendant := exec.Command("/bin/sh", append([]string{"-c", `sleep 0.3; printf '%s\n' "$@" >&3`, "sh"}, records...)...)
			descendant.ExtraFiles = []*os.File{output}
			_ = descendant.Start()
		} else {
			_, _ = output.Write([]byte(strings.Join(records, "\n") + "\n"))
		}
		if mode == "unreleased-after-completed" {
			os.Exit(0)
		}
		os.Exit(2)
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
	request := capabilityRequest{Operation: "setup", Identity: strings.Repeat("a", 64), Bundle: bundleLocation{Path: bundle, Writable: true}, PublicationBundle: bundleLocation{Path: bundle, Writable: true}, Packages: []prerequisites.NativePackage{}, Tools: []prerequisites.ToolDefinition{}}
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

// An adapter that refuses an unstamped oc names the refusal before it fails, so
// the operator is told of the release-stamp check rather than to restore the
// source a rerun reuses. It is named whichever of the record and the failed
// exit the runner reads first, and only for the client being installed. Out of
// its place, before that client's continue or after completed, the record is
// refused like any other, again whichever the runner reads first.
func TestAnUnstampedClientRefusalNamesTheReleaseStampCheck(t *testing.T) {
	for _, check := range []struct {
		name, mode, reason, kind string
		named                    bool
		message                  string
	}{
		{"record first", "unreleased", "release-stamp", "openshift-clients", true, ""},
		{"exit first", "unreleased-after-exit", "release-stamp", "openshift-clients", true, ""},
		{"another reason", "unreleased", "source integrity", "openshift-clients", false, "the Ansible capability protocol was invalid"},
		{"another tool", "unreleased", "release-stamp", "helm", false, "the Ansible capability protocol was invalid"},
		{"before the client's continue", "unreleased-before-continue", "release-stamp", "openshift-clients", false, "the Ansible capability protocol was invalid"},
		{"after completed", "unreleased-after-completed", "release-stamp", "openshift-clients", false, "the Ansible capability protocol was invalid"},
		{"after completed, exit first", "unreleased-after-completed-exit-first", "release-stamp", "openshift-clients", false, "the Ansible capability protocol was invalid"},
	} {
		t.Run(check.name, func(t *testing.T) {
			launch, request, boundary := runnerFixture(t, check.mode)
			boundary.completedDrain, boundary.authorizedDrain = 5*time.Second, 5*time.Second
			launch.Environment = append(launch.Environment, "BOOTWRIGHT_TEST_REASON="+check.reason)
			request.Tools = []prerequisites.ToolDefinition{{Kind: check.kind, Version: "4.21.15", Source: prerequisites.DependencySource{ID: "tool-oc", SHA256: strings.Repeat("d", 64), Bytes: 1}}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
			found := diagnostics.Of(err)
			if len(found) != 1 || result.Outcome != "unknown" {
				t.Fatalf("the refused client left the run %s (%v)", result.Outcome, err)
			}
			named := found[0].Code == "controller.setup" && found[0].Message == "the oc of OpenShift client release 4.21.15 does not name its frozen release" &&
				strings.Contains(found[0].Remediation, "passes the release-stamp check")
			if named != check.named {
				t.Fatalf("diagnostic %+v, want the release-stamp refusal: %v", found[0], check.named)
			}
			if check.message != "" && found[0].Message != check.message {
				t.Fatalf("diagnostic %+v, want %q", found[0], check.message)
			}
		})
	}
}

// A record the adapter wrote before its failed exit may be read after it. The
// runner judges it as if it had read it first, so a record it refuses or cannot
// read fails the run as a protocol breach whichever it reads first. One read
// first holds the adapter until the refusal closes its acknowledgement channel;
// one read after is written by a descendant once the adapter is reaped. Records
// it accepts after the exit leave that failure and move the protocol on, so a
// completed is judged after its loaded, prepared, native or continue, but
// nothing is released, published, authorized or reported for them: release
// and the inventory read follow only a loaded read before the exit, intent
// only a prepared read before it, and no native or continue read after it
// reports the installation that authorizes it.
// The drain's close of a channel a descendant holds is the runner's own read
// and no record, so it leaves the failed exit too.
func TestARecordReadAfterTheFailedExitIsJudgedAsIfReadFirst(t *testing.T) {
	const handoff = `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `
	acknowledged := func(record string) string {
		return `printf '` + record + `' >&3; read -r reply <&4; `
	}
	readFirst := func(records string) string {
		return handoff + `printf '` + records + `' >&3; read -r reply <&4; exit 2`
	}
	afterTheExit := func(records string) string {
		return `adapter=$$; (exec >/dev/null 2>&1 4<&-; while kill -0 "$adapter" 2>/dev/null; do sleep 0.01; done; sleep 0.05; printf '` + records + `' >&3) & exec 3>&- 4<&-; exit 2`
	}
	readAfter := func(records string) string { return handoff + afterTheExit(records) }
	record := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data) + `\n`
	}
	sha := strings.Repeat("a", 64)
	prepared := `{"phase":"prepared","preparation":{"addedSources":[],"inventorySHA256":"` + sha + `"}}\n`
	completed := `{"evidence":{"added":[],"after":"` + sha + `","before":"` + sha + `","planDigest":"","postcondition":true,"request":"` + sha + `","tools":[]},"outcome":"unchanged","phase":"completed"}\n`
	plan := &prerequisites.NativeResolvedPlan{Digest: strings.Repeat("c", 64), BeforeSHA256: sha, AfterSHA256: strings.Repeat("b", 64), Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}
	transitions, err := prerequisites.NativeTransitionsDigest(plan.Actions)
	if err != nil {
		t.Fatal(err)
	}
	nativePrepared := record(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AfterInventorySHA256: plan.AfterSHA256, PlanDigest: plan.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}})
	nativeCompleted := record(map[string]any{"phase": "completed", "outcome": "changed", "evidence": map[string]any{"request": sha, "before": sha, "after": plan.AfterSHA256, "planDigest": plan.Digest, "added": []string{"native-one"}, "tools": []string{}, "postcondition": true}})
	tool := prerequisites.ToolDefinition{Kind: "helm", Version: "3.17.3", Source: prerequisites.DependencySource{ID: "tool-helm", SHA256: strings.Repeat("d", 64), Bytes: 1}}
	toolPrepared := record(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AddedSources: []string{tool.Source.ID}}})
	toolCompleted := record(map[string]any{"phase": "completed", "outcome": "changed", "evidence": map[string]any{"request": sha, "before": sha, "after": sha, "planDigest": "", "added": []string{}, "tools": []map[string]string{{"source": tool.Source.ID, "sha256": tool.Source.SHA256, "files": strings.Repeat("e", 64)}}, "postcondition": true}})
	withNative := func(request *capabilityRequest) { request.Native = plan }
	withTool := func(request *capabilityRequest) { request.Tools = []prerequisites.ToolDefinition{tool} }
	const (
		malformed  = `not a record\n`
		misplaced  = `{"phase":"refused","reason":"release-stamp"}\n`
		incomplete = "the Ansible structured result was incomplete"
		invalid    = "the Ansible capability protocol was invalid"
		failedExit = "Ansible did not complete the authorized dependency operation"
	)
	for _, check := range []struct {
		name, script, code, message string
		drain                       time.Duration
		request                     func(*capabilityRequest)
		released, published         bool
	}{
		{"a malformed record read first", readFirst(malformed), "controller.unknown", incomplete, 5 * time.Second, nil, true, false},
		{"a malformed record read after the exit", readAfter(malformed), "controller.unknown", incomplete, 5 * time.Second, nil, true, false},
		{"a record out of its place read first", readFirst(misplaced), "controller.unknown", invalid, 5 * time.Second, nil, true, false},
		{"a record out of its place read after the exit", readAfter(misplaced), "controller.unknown", invalid, 5 * time.Second, nil, true, false},
		{"valid records read after the exit", readAfter(prepared + completed), "controller.setup", failedExit, 5 * time.Second, nil, true, false},
		{"a loaded record read after the exit", afterTheExit(`{"phase":"loaded"}\n` + prepared + completed), "controller.setup", failedExit, 5 * time.Second, nil, false, false},
		{"a native record read after the exit", handoff + acknowledged(nativePrepared) + afterTheExit(`{"phase":"native"}\n`+nativeCompleted), "controller.setup", failedExit, 5 * time.Second, withNative, true, true},
		{"a continue record read after the exit", handoff + acknowledged(toolPrepared) + afterTheExit(`{"phase":"continue"}\n`+toolCompleted), "controller.setup", failedExit, 5 * time.Second, withTool, true, true},
		{"a channel the drain closes after the exit", handoff + `(exec >/dev/null 2>&1 4<&-; sleep 5) & exit 2`, "controller.setup", failedExit, 200 * time.Millisecond, nil, true, false},
	} {
		t.Run(check.name, func(t *testing.T) {
			launch, request, boundary := runnerFixture(t, "unused")
			if check.request != nil {
				check.request(&request)
			}
			boundary.completedDrain, boundary.authorizedDrain = check.drain, check.drain
			boundary.command = func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", check.script) }
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			released, published := 0, false
			var details []string
			result, err := runProcess(ctx, launch, request, func() error { released++; return nil }, func(context.Context, prerequisites.NativePreparation) error {
				published = true
				return nil
			}, func(event prerequisites.ProgressEvent) { details = append(details, event.Detail) }, nil, boundary)
			if ctx.Err() != nil {
				t.Fatalf("the adapter ran until the deadline (%v)", err)
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != check.code || found[0].Message != check.message {
				t.Fatalf("the run reported %+v (%v), want %s %q", found, err, check.code, check.message)
			}
			// Setup's own run, which carries no tool, records failed until
			// Go acknowledges a native record; with a tool it is unknown
			// once prepared.
			outcome := "failed"
			if check.published && len(request.Tools) > 0 {
				outcome = "unknown"
			}
			if result.Outcome != outcome || published != check.published {
				t.Fatalf("the run left %s with intent published %v, want %s with %v", result.Outcome, published, outcome, check.published)
			}
			releases, progress := 0, []string{"starting the private Ansible runtime"}
			if check.released {
				releases, progress = 1, append(progress, "reading the native package inventory")
			}
			if released != releases || !slices.Equal(details, progress) {
				t.Fatalf("the run released %d times and reported %q, want %d and %q", released, details, releases, progress)
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

func clientTools(sizes ...int64) []prerequisites.ToolDefinition {
	tools := make([]prerequisites.ToolDefinition, 0, len(sizes))
	for index, size := range sizes {
		tools = append(tools, prerequisites.ToolDefinition{Kind: "kubectl", Source: prerequisites.DependencySource{ID: "tool-" + strconv.Itoa(index), Bytes: size}})
	}
	return tools
}

func nativePackages(sizes ...int64) []prerequisites.NativePackage {
	packages := make([]prerequisites.NativePackage, 0, len(sizes))
	for index, size := range sizes {
		packages = append(packages, prerequisites.NativePackage{Source: prerequisites.DependencySource{ID: "native-" + strconv.Itoa(index), Bytes: size}})
	}
	return packages
}

// A run's deadline is its base plus the bound its native packages are staged
// under and each tool's acquisition deadline. Native staging alone is held
// within the ceiling, so a large native closure is admitted at the ceiling,
// while one with tools past it is refused before Ansible starts.
func TestAClientStageDeadlineIsDerivedFromItsSources(t *testing.T) {
	for name, check := range map[string]struct {
		packages []prerequisites.NativePackage
		tools    []prerequisites.ToolDefinition
		want     time.Duration
		refused  bool
	}{
		"no tools":                           {nil, nil, runTimeout, false},
		"two tools":                          {nil, clientTools(1, 44_433_552), runTimeout + 121*time.Second + 205*time.Second, false},
		"past the ceiling":                   {nil, clientTools(1<<30, 1<<30, 1<<30, 1<<30), clientStageCeiling, true},
		"native only":                        {nativePackages(100 << 20), nil, runTimeout + 320*time.Second, false},
		"4 GiB native":                       {nativePackages(1<<30, 1<<30, 1<<30, 1<<30), nil, clientStageCeiling, false},
		"native plus tools past the ceiling": {nativePackages(1<<30, 1<<30, 1<<30), clientTools(1 << 30), clientStageCeiling, true},
	} {
		request := capabilityRequest{Packages: check.packages, Tools: check.tools}
		if got := runDeadline(request); got != check.want {
			t.Errorf("%s: runDeadline = %s, want %s", name, got, check.want)
		}
		if err := requireClientStageCeiling(request); (err != nil) != check.refused {
			t.Errorf("%s: the ceiling check returned %v, want refused %v", name, err, check.refused)
		}
	}
	if runTimeout != 600*time.Second || clientStageCeiling != 7_200*time.Second {
		t.Fatalf("runTimeout %s and clientStageCeiling %s changed; the exact seconds above assume 10 minutes and 2 hours", runTimeout, clientStageCeiling)
	}
	if got := nativeStaging(nativePackages(1<<30, 1<<30, 1<<30, 1<<30)); got != nativeStagingCeiling {
		t.Errorf("4 GiB of native packages stage under %s, want the ceiling %s the adapter admits", got, nativeStagingCeiling)
	}
	if nativeStagingCeiling != clientStageCeiling-runTimeout {
		t.Fatalf("nativeStagingCeiling %s is not the stage ceiling %s less the run base %s", nativeStagingCeiling, clientStageCeiling, runTimeout)
	}
}

// Four 1 GiB clients need 600 + 4 x 2,168 = 9,272 seconds, past the 7,200 the
// ceiling allows, so the run refuses before a single invocation file exists.
func TestAClientClosureBeyondTheCeilingRefusesBeforeAnsibleStarts(t *testing.T) {
	launch, request, boundary := runnerFixture(t, "complete")
	request.Tools = clientTools(1<<30, 1<<30, 1<<30, 1<<30)
	var called []string
	boundary.command = func(path string, arguments ...string) *exec.Cmd {
		called = append(called, path)
		return exec.Command("/bin/false")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
	var classified *diagnostics.Failure
	if !errors.As(err, &classified) || len(classified.Diagnostics) != 1 || classified.Diagnostics[0].Code != "controller.unsupported" || result.Outcome != "failed" {
		t.Fatalf("a closure past the ceiling ran: %s %v", result.Outcome, err)
	}
	if want := "the selected target clients total 4294967296 bytes, whose acquisition deadlines exceed the controller stage's 2-hour ceiling"; classified.Diagnostics[0].Message != want {
		t.Fatalf("refusal = %q, want %q", classified.Diagnostics[0].Message, want)
	}
	if len(called) != 0 {
		t.Fatalf("Ansible was started for a refused closure: %v", called)
	}
	for _, parent := range []string{boundary.jobParent, boundary.scratchParent} {
		if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
			t.Fatalf("%s holds %v (%v) after a refusal", parent, entries, err)
		}
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
	// Setup's own run acknowledged no native record, so nothing it did is
	// left to resolve: it is failed, never a proved outcome.
	if result.Outcome != "failed" {
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
