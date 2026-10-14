package lifecycle

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// movedLock is the runtime lock a later build embeds: another lock digest
// and the ansible-core release it pins.
var movedLock = RuntimeLock{Digest: strings.Repeat("5", 64), AnsibleCore: "2.21.6"}

// closureOf is the closure an operation registered under a lock and an
// interpreter ABI freezes.
func closureOf(lock RuntimeLock, abi string) operationstore.Closure {
	return operationstore.Closure{Digest: prerequisites.RuntimeClosureDigest(lock.Digest, abi), Python: abi, Ansible: lock.AnsibleCore}
}

// durable is everything a refused continuation must leave as it found it.
type durable struct {
	files             map[string][]byte
	evidence          []byte
	binds             int
	guarded, executed int
}

func durableOf(h *harness) durable {
	h.guard.mutex.Lock()
	guarded := h.guard.calls
	h.guard.mutex.Unlock()
	h.capability.mutex.Lock()
	executed := len(h.capability.calls) + len(h.capability.destroys)
	h.capability.mutex.Unlock()
	return durable{
		files: h.workspace.area.clone().files, evidence: slices.Clone(h.workspace.evidence),
		binds: h.workspace.binds, guarded: guarded, executed: executed,
	}
}

func (d durable) equal(other durable) bool {
	return maps.EqualFunc(d.files, other.files, bytes.Equal) && bytes.Equal(d.evidence, other.evidence) &&
		d.binds == other.binds && d.guarded == other.guarded && d.executed == other.executed
}

// refusedUnchanged runs verb, which must refuse lifecycle.state with exactly
// message and remediation, and proves the refusal left every record, the
// evidence and the host as they were: no log restored, no state marked, no
// runtime launched and no effect.
func refusedUnchanged(t *testing.T, h *harness, verb reconciliation.Verb, message, remediation string) {
	t.Helper()
	before := durableOf(h)
	var err error
	if verb == reconciliation.Destroy {
		_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("the continued %s reported %+v, want %q with %q", verb, reported, message, remediation)
	}
	if !before.equal(durableOf(h)) {
		t.Fatalf("the refused continuation of %s changed durable state or reached the host", verb)
	}
}

// failedApply leaves the harness's apply failed on its one block, which a
// fresh removal may supersede.
func failedApply(t *testing.T, h *harness) {
	t.Helper()
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := apply(h); err == nil {
		t.Fatal("the apply a continuation starts from did not fail")
	}
}

// applyLeftUnknown leaves the harness's apply unknown on its one block, which a
// continuation resolves before it does anything else.
func applyLeftUnknown(t *testing.T, h *harness) {
	t.Helper()
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("the apply a continuation starts from did not lose its outcome")
	}
	if operation, _ := durableOperation(t, h); operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationUnknown {
		t.Fatalf("the apply is %s %s, not an unknown apply", operation.Verb, operation.State)
	}
}

// unknownRemoval leaves the harness's removal of a completed apply unknown on
// its one block, which only a continuation may settle.
func unknownRemoval(t *testing.T, h *harness) {
	t.Helper()
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("the removal a continuation starts from did not lose its outcome")
	}
	if operation, _ := durableOperation(t, h); operation.Verb != reconciliation.Destroy || operation.State != reconciliation.OperationUnknown {
		t.Fatalf("the removal is %s %s, not an unknown destroy", operation.Verb, operation.State)
	}
}

// Every registration records the closure of the runtime its effects run in,
// the runtime lock and the interpreter's ABI, and a fresh verb runs under the
// runtime in hand: a removal registered by a build with another lock records
// that lock's closure, and launches that lock's runtime.
func TestARegistrationFreezesTheRuntimesExecutionClosure(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	applied, _ := durableOperation(t, h)
	if applied.Version != operationstore.OperationVersion || applied.Closure == nil || *applied.Closure != closureOf(testLock, "3.14") {
		t.Fatalf("the apply registered version %d with closure %+v", applied.Version, applied.Closure)
	}
	h.service.options.Lock = movedLock
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("a fresh removal under a moved lock: %v", err)
	}
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy || removal.Closure == nil || *removal.Closure != closureOf(movedLock, "3.14") {
		t.Fatalf("the removal registered closure %+v", removal.Closure)
	}
	launched := h.guard.launched[len(h.guard.launched)-1]
	if launched.LockDigest != movedLock.Digest || launched.Digest != prerequisites.RuntimeDigest(movedLock.Digest, "3.14", "/usr/bin/python3.14") ||
		launched.Interpreter.Path != "/usr/bin/python3.14" || launched.AnsibleCore != movedLock.AnsibleCore {
		t.Fatalf("the removal launched %+v", launched)
	}
}

// A rebuild that embeds the same runtime lock, as any Go or automation fix
// does, continues an operation another build registered: the closure is the
// lock and the ABI, never the build.
func TestASameLockRebuildContinuesALiveOperation(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		t.Run(string(verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			if verb == reconciliation.Apply {
				failedApply(t, h)
			} else {
				unknownRemoval(t, h)
			}
			registered, _ := durableOperation(t, h)
			h.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			result, err := continued(h, verb)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the same-lock rebuild's continuation = %+v (%v)", result, err)
			}
			if operation, _ := durableOperation(t, h); operation.Closure == nil || *operation.Closure != closureOf(testLock, "3.14") {
				t.Fatalf("the continued operation holds closure %+v", operation.Closure)
			}
		})
	}
}

// An OS Python update within the ABI, or an OS upgrade that keeps it, leaves
// the interpreter's path and ABI as they were: the receipt's full version
// moves, the closure does not, and the operation continues.
func TestAPythonZStreamUpdateContinues(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	registered, _ := durableOperation(t, h)
	h.setup.set(completedSetup("3.14", "3.14.9"))
	result, err := apply(h)
	if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation after a z-stream update = %+v (%v)", result, err)
	}
}

// A build with another runtime lock, or a setup that chose an interpreter of
// another ABI, carries another closure, so a continuation of either verb
// refuses before it restores its log or marks its operation running. The
// refusal names the kept runtime the registering build continues it with,
// and an apply also the removal that takes back what it owns; restoring the
// lock the operation registered under continues it.
func TestALockChangeRefusesContinuationNamingTheKeptRuntime(t *testing.T) {
	const kept = "continue it with bootwright devel (abcdef1), whose runtime this host keeps (Python 3.14, ansible-core 2.21.5)"
	for _, change := range []struct {
		name    string
		move    func(*harness)
		message string
	}{
		{"another lock", func(h *harness) { h.service.options.Lock = movedLock },
			"this build's controller runtime carries another Python and Ansible closure (Python 3.14, ansible-core 2.21.6) than the one this operation registered with"},
		{"another ABI", func(h *harness) { h.setup.set(completedSetup("3.13", "3.13.12")) },
			"this build's controller runtime carries another Python and Ansible closure (Python 3.13, ansible-core 2.21.5) than the one this operation registered with"},
	} {
		for _, test := range []struct {
			verb        reconciliation.Verb
			start       func(*testing.T, *harness)
			remediation string
			continued   func(*harness) []string
		}{
			{reconciliation.Apply, failedApply, kept + ", or take back what it owns with bootwright destroy --context lab", func(h *harness) []string { return h.capability.applies }},
			{reconciliation.Destroy, unknownRemoval, kept, func(h *harness) []string { return h.capability.observes }},
		} {
			t.Run(change.name+"/"+string(test.verb), func(t *testing.T) {
				h := newHarness(t, "artifact-server-lab")
				test.start(t, h)
				registered, _ := durableOperation(t, h)
				change.move(h)
				refusedUnchanged(t, h, test.verb, change.message, test.remediation)
				h.service.options.Lock = testLock
				h.setup.set(completedSetup("3.14", "3.14.8"))
				h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
				before := len(test.continued(h))
				result, err := continued(h, test.verb)
				if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) || len(test.continued(h)) == before {
					t.Fatalf("the continuation under the restored lock = %+v (%v)", result, err)
				}
			})
		}
	}
}

// continued runs the confirmed verb that continues the harness's operation.
func continued(h *harness, verb reconciliation.Verb) (*OperationResult, error) {
	if verb == reconciliation.Destroy {
		return h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	return apply(h)
}

// A build stamped with a commit and no version, as make build stamps one cut
// from no tag, is devel in the version command's spelling, so status, the
// continuation refusal and the removal refusal name devel and that commit
// rather than losing the build that registered the operation. A record naming
// neither names the record itself.
func TestARecordWithOnlyACommitNamesDevelAndTheCommit(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		executable Executable
		named      string
		build      string
	}{
		{Executable{Commit: "abcdef1"}, "devel (abcdef1)", "bootwright devel (abcdef1)"},
		{Executable{}, "", "the executable its operation.json records"},
	} {
		t.Run(test.build, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.service.options.Executable = test.executable
			failedApply(t, h)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || status.Lifecycle == nil || status.Lifecycle.Executable != test.named {
				t.Fatalf("status names the registering build %+v (%v), want %q", status.Lifecycle, err, test.named)
			}
			h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
			h.capability.definitions[0].ContentDigest = strings.Repeat("8", 64)
			refusedUnchanged(t, h, reconciliation.Apply, "this executable would freeze the block artifact-server-lab differently from the plan this operation froze",
				"take back what this operation owns under this executable with bootwright destroy --context lab, or install "+test.build+", which registered it")
			h.capability.removalErr = failure("lifecycle.state", "the frozen request has an unsupported version", "")
			_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Remediation != "remove it with "+test.build {
				t.Fatalf("the removal of an unreadable block reported %+v, want it to name %q", reported, test.build)
			}
		})
	}
}

// A fresh apply killed at each write of its registration, or just after it,
// replays under the closure it froze. Under the same lock the retry
// converges and every record names that closure. Under a moved lock, a
// registration that landed refuses its continuation with no effect, and one
// that did not is a fresh apply again, registering the moved closure.
func TestARegistrationKilledAtEachWriteReplaysUnderItsClosure(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		point      string
		registered bool
	}{
		{"write <op>/plan.json#1", false}, {"write <op>/operation.json#1", false}, {"replace index.json#1", false},
		{"append <op>/logs/operation.jsonl#1", true}, {"replace <op>/blocks/a/state.json#1", true},
	} {
		point := test.point
		for _, move := range []bool{false, true} {
			rig := newKillRig(t)
			killed := &killPoints{counts: map[string]int{}, key: point}
			rig.arm(killed)
			if err := killInvoke(ctx, rig.harness.service, reconciliation.Apply); err == nil || killed.snapshot == nil {
				t.Fatalf("the apply was not killed at %s: %v", point, killed.keys)
			}
			snapshot := killed.snapshot
			service := rig.over(snapshot)
			want := closureOf(testLock, "3.14")
			registered := killRest(snapshot.workspace) != "removed"
			if registered != test.registered {
				t.Fatalf("killed at %s the context rests %s", point, killRest(snapshot.workspace))
			}
			if move {
				service.options.Lock = movedLock
				want = closureOf(movedLock, "3.14")
			}
			if move && registered {
				files := snapshot.workspace.area.clone().files
				err := killInvoke(ctx, service, reconciliation.Apply)
				if reported := diagnostics.Of(err); len(reported) != 1 || !strings.HasPrefix(reported[0].Message, "this build's controller runtime carries another Python and Ansible closure") {
					t.Fatalf("killed at %s, the continuation under a moved closure reported %+v", point, reported)
				}
				if !maps.EqualFunc(files, snapshot.workspace.area.clone().files, bytes.Equal) || snapshot.host.effectCount(reconciliation.Apply, "a") != 0 {
					t.Fatalf("killed at %s, the refused continuation changed its records or reached the host", point)
				}
				continue
			}
			for retry := 0; retry < killBoundFreshApply && killRest(snapshot.workspace) != "applied"; retry++ {
				_ = killInvoke(ctx, service, reconciliation.Apply)
			}
			if rest := killRest(snapshot.workspace); rest != "applied" {
				t.Fatalf("killed at %s (moved %t), %d applies leave the context %s", point, move, killBoundFreshApply, rest)
			}
			if failures := killReadBack(snapshot.workspace); len(failures) != 0 {
				t.Fatalf("killed at %s (moved %t): %v", point, move, failures)
			}
			store := operationstore.New(snapshot.workspace.area, killClock)
			index, err := store.Index(ctx)
			if err != nil {
				t.Fatal(err)
			}
			operation, err := store.ReadOperation(ctx, index.Current)
			if err != nil || operation.Version != operationstore.OperationVersion || operation.Closure == nil || *operation.Closure != want {
				t.Fatalf("killed at %s (moved %t), the applied operation reads %+v (%v)", point, move, operation, err)
			}
		}
	}
}
