package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// dataLoss is the token every plan these tests finalize consumes, so an
// invocation that reached authorization without it would refuse.
func dataLoss() []string { return []string{reconciliation.AuthorizationDataLoss} }

// consumingHost is the kill journeys' host whose removal consumes the token,
// counting every probe and observation it answers.
type consumingHost struct {
	*killHost
	reads atomic.Int32
}

func (h *consumingHost) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := h.killHost.Removal(ctx, block)
	removal.Consumes = dataLoss()
	return removal, err
}

func (h *consumingHost) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	h.reads.Add(1)
	return h.killHost.Quiescent(ctx, probe)
}

func (h *consumingHost) Observe(ctx context.Context, execution Execution) (Observation, error) {
	h.reads.Add(1)
	return h.killHost.Observe(ctx, execution)
}

func (h *consumingHost) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	h.reads.Add(1)
	return h.killHost.ObserveRemoval(ctx, execution)
}

// consumingRig is the kill journeys' rig over a plan whose apply and removal
// each consume the token.
func consumingRig(t *testing.T) *killRig {
	t.Helper()
	definitions := killDefinitions()
	for index := range definitions {
		definitions[index].Consumes = dataLoss()
	}
	h := newPlannedHarness(t, definitions)
	host := newKillHost(definitions)
	h.service.capabilities = testResolver{capability: &consumingHost{killHost: host}}
	return &killRig{harness: h, host: host}
}

// authorized runs a verb with the token its plan consumes, without asking.
func authorized(ctx context.Context, service Service, verb reconciliation.Verb) error {
	if verb == reconciliation.Destroy {
		_, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
		return err
	}
	_, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
	return err
}

// killedRun is one invocation killed at one of its writes: the rig, what that
// write left, every write the same invocation makes uninterrupted and the
// state it then leaves.
type killedRun struct {
	rig      *killRig
	snapshot *killSnapshot
	writes   []string
	end      killState
}

// killedAt brings a consuming rig to its start by running each verb of
// prepare, then runs verb killed at the named write.
func killedAt(ctx context.Context, t *testing.T, prepare []reconciliation.Verb, verb reconciliation.Verb, point string) killedRun {
	t.Helper()
	begin := func() *killRig {
		rig := consumingRig(t)
		for _, step := range prepare {
			if err := authorized(ctx, rig.harness.service, step); err != nil {
				t.Fatalf("the %s this run starts from failed: %v", step, err)
			}
		}
		return rig
	}
	named := begin()
	names := &killPoints{counts: map[string]int{}}
	named.arm(names)
	if err := authorized(ctx, named.harness.service, verb); err != nil {
		t.Fatalf("the uninterrupted %s failed: %v", verb, err)
	}
	at := slices.Index(names.keys, point) + 1
	if at == 0 {
		t.Fatalf("the uninterrupted %s makes no write %s: %v", verb, point, names.keys)
	}
	rig := begin()
	points := &killPoints{counts: map[string]int{}, at: at}
	rig.arm(points)
	if err := authorized(ctx, rig.harness.service, verb); err == nil || points.snapshot == nil || points.keys[at-1] != point {
		t.Fatalf("the %s was not killed at %s: %v (%v)", verb, point, points.keys, err)
	}
	return killedRun{
		rig: rig, snapshot: points.snapshot, writes: names.keys,
		end: killStateOf(named.harness.workspace, named.harness.binder, named.host),
	}
}

// repairing is the rig's service over what the kill left. It has no way to
// confirm, and its host counts every probe and observation.
func (k killedRun) repairing() (Service, *consumingHost) {
	service := k.rig.over(k.snapshot)
	host := &consumingHost{killHost: k.snapshot.host}
	service.capabilities = testResolver{capability: host}
	service.options.Confirmer = nil
	return service, host
}

// operations names the current operation and every operation directory the
// workspace holds.
func operations(t *testing.T, w *testWorkspace) (string, []string) {
	t.Helper()
	ctx := context.Background()
	index, err := operationstore.New(w.area, killClock).Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := w.area.Entries(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		if entry.Directory {
			found = append(found, entry.Name)
		}
	}
	return index.Current, found
}

// requireRepaired holds a repair to the state the uninterrupted invocation
// leaves, reached without presenting a plan, reaching the host or registering
// an operation.
func requireRepaired(t *testing.T, run killedRun, host *consumingHost, presented int, effects map[string]int, current string, directories []string) {
	t.Helper()
	w := run.snapshot.workspace
	if state := killStateOf(w, run.snapshot.binder, run.snapshot.host); state != run.end {
		t.Fatalf("the repair left %+v, not %+v", state, run.end)
	}
	if len(run.rig.harness.presenter.presented) != presented {
		t.Fatal("the repair presented a plan")
	}
	if host.reads.Load() != 0 || !maps.Equal(run.snapshot.host.effects, effects) {
		t.Fatalf("the repair reached the host: %d reads, effects %v", host.reads.Load(), run.snapshot.host.effects)
	}
	if now, found := operations(t, w); now != current || !slices.Equal(found, directories) {
		t.Fatalf("the repair registered an operation: %s %v, was %s %v", now, found, current, directories)
	}
}

func evidenceBytes(t *testing.T, verb reconciliation.Verb, state reconciliation.OperationState) []byte {
	t.Helper()
	evidence, err := reconciliation.EvidenceFor(verb, state)
	if err != nil {
		t.Fatal(err)
	}
	data, err := evidence.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A removal interrupted after its record says done, and before it released
// its reservations or Secret binding or published pristine evidence, is
// finished by the next destroy: it settles without the token its plan
// consumes and without a confirmation, releases what remains and publishes
// pristine, presenting nothing and reaching no host.
func TestAnInterruptedRemovalFinalizationIsCompletedByTheNextDestroy(t *testing.T) {
	ctx := context.Background()
	pristine := killPristine(t)
	for _, point := range []string{"release reservations#1", "release secret binding#1", "publish evidence#2"} {
		t.Run(point, func(t *testing.T) {
			run := killedAt(ctx, t, []reconciliation.Verb{reconciliation.Apply}, reconciliation.Destroy, point)
			if point == "publish evidence#2" && run.writes[len(run.writes)-1] != point {
				t.Fatalf("pristine evidence is not a removal's last write: %v", run.writes)
			}
			w := run.snapshot.workspace
			if killRest(w) != "removed" || bytes.Equal(w.evidence, pristine) {
				t.Fatalf("the kill left %s under evidence %q", killRest(w), w.evidence)
			}
			service, host := run.repairing()
			current, directories := operations(t, w)
			presented, effects := len(run.rig.harness.presenter.presented), maps.Clone(run.snapshot.host.effects)
			result, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
			if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.State != "done" || result.Receipt.Operation != current {
				t.Fatalf("the next destroy = %+v (%v)", result, err)
			}
			requireRepaired(t, run, host, presented, effects, current, directories)
		})
	}
}

// A fresh apply killed after it claimed its reservations and before its plan
// landed leaves them held beside the completed removal the context rests on,
// under the running evidence the apply raised first. That removal's
// finalization is then incomplete, so the next destroy releases them without
// the token or a confirmation and settles, presenting nothing and reaching no
// host.
func TestAReservationHeldBesideACompletedRemovalIsReleasedByTheNextDestroy(t *testing.T) {
	ctx := context.Background()
	pristine := killPristine(t)
	run := killedAt(ctx, t, []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy}, reconciliation.Apply, "write <op>/plan.json#1")
	w := run.snapshot.workspace
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	if killRest(w) != "removed" || !bytes.Equal(w.evidence, running) || len(w.reservations) == 0 {
		t.Fatalf("the kill left %s under evidence %q holding %v", killRest(w), w.evidence, w.reservations)
	}
	service, host := run.repairing()
	current, directories := operations(t, w)
	presented, effects := len(run.rig.harness.presenter.presented), maps.Clone(run.snapshot.host.effects)
	result, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != current {
		t.Fatalf("the next destroy = %+v (%v)", result, err)
	}
	if len(w.reservations) != 0 || !bytes.Equal(w.evidence, pristine) {
		t.Fatalf("the next destroy left %v under evidence %q", w.reservations, w.evidence)
	}
	if len(run.rig.harness.presenter.presented) != presented || host.reads.Load() != 0 || !maps.Equal(run.snapshot.host.effects, effects) {
		t.Fatal("the next destroy presented a plan or reached the host")
	}
	if now, found := operations(t, w); now != current || !slices.Equal(found, directories) {
		t.Fatalf("the next destroy registered an operation: %s %v, was %s %v", now, found, current, directories)
	}
}

// A completed removal that cannot release its Secret binding still reports
// done, fails naming the destroy that finishes it beside any log fault it
// latched, and keeps its running projection rather than claiming pristine
// evidence. A destroy repeated while the release still fails reports the same,
// and once it can release, the next destroy finishes it; neither needs the
// token or a confirmation.
func TestARemovalWhoseBindingReleaseFailsReportsIncompleteFinalization(t *testing.T) {
	ctx := context.Background()
	for name, faulted := range map[string]bool{"alone": false, "beside a log fault": true} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.capability.consumes = map[string][]string{"alpha": dataLoss()}
			h.capability.reservations = []prerequisites.HostReservation{{Context: testContextName, Kind: "artifact-server", Service: "alpha", Keys: []string{"socket:192.0.2.1:8443"}}}
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			if faulted {
				h.capability.destroyHold = failDuring(h, path.Join("op-"+strings.Repeat("02", 16), "logs", "blocks", "alpha", "attempt-000001.jsonl"))
			}
			h.binder.releaseErr = errors.New("the custody store refused the release")
			result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
			if result == nil || result.Receipt.State != "done" {
				t.Fatalf("the removal's receipt = %+v", result)
			}
			reported := diagnostics.Of(err)
			if faulted {
				if len(reported) < 2 || reported[0].Code != "runtime.log" {
					t.Fatalf("the removal lost its log fault: %+v (%v)", reported, err)
				}
				reported = reported[len(reported)-1:]
			}
			requireIncompleteRemoval(t, reported, err)
			running := evidenceBytes(t, reconciliation.Destroy, reconciliation.OperationRunning)
			if !bytes.Equal(h.workspace.evidence, running) {
				t.Fatalf("evidence = %q, want the running projection %q", h.workspace.evidence, running)
			}
			if len(h.binder.released) != 0 || len(h.workspace.reservations) != 0 {
				t.Fatalf("released %v, reservations %v", h.binder.released, h.workspace.reservations)
			}
			h.service.options.Confirmer = nil
			removals, presented := len(h.capability.destroys), len(h.presenter.presented)
			failed, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
			if failed != nil {
				t.Fatalf("the repeated destroy reported %+v while its release still fails", failed)
			}
			requireIncompleteRemoval(t, diagnostics.Of(err), err)
			if !bytes.Equal(h.workspace.evidence, running) || len(h.binder.released) != 0 {
				t.Fatalf("the repeated destroy left evidence %q and released %v", h.workspace.evidence, h.binder.released)
			}
			h.binder.releaseErr = nil
			repeated, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
			if err != nil || !repeated.Settled || repeated.Receipt.Operation != result.Receipt.Operation {
				t.Fatalf("the next destroy = %+v (%v)", repeated, err)
			}
			if !bytes.Equal(h.workspace.evidence, killPristine(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("the next destroy left evidence %q and released %v", h.workspace.evidence, h.binder.released)
			}
			if len(h.capability.destroys) != removals || len(h.presenter.presented) != presented {
				t.Fatal("the repeated destroys removed or presented something")
			}
		})
	}
}

// requireIncompleteRemoval holds a failure to the one diagnostic a completed
// removal reports when it could not give back everything it owned.
func requireIncompleteRemoval(t *testing.T, reported []diagnostics.Diagnostic, err error) {
	t.Helper()
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the removal completed, but releasing what it owned is incomplete" ||
		reported[0].Remediation != "repeat bootwright destroy to finish it" {
		t.Fatalf("the removal failed with %+v (%v)", reported, err)
	}
}

// A completed apply whose projection an interruption never published is
// finalized by the repeated apply, which then settles without the token its
// plan consumes and without a confirmation.
func TestAnInterruptedApplyFinalizationIsCompletedByTheRepeatedApply(t *testing.T) {
	ctx := context.Background()
	for name, left := range map[string]reconciliation.OperationState{
		"pending evidence": reconciliation.OperationRunning,
		"failed evidence":  reconciliation.OperationFailed,
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha"), destructive("bravo")})
			first, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
			if err != nil {
				t.Fatal(err)
			}
			h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, left)
			h.service.options.Confirmer = nil
			applies, presented, mutations := len(h.capability.applies), len(h.presenter.presented), h.workspace.mutations
			result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
			if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != first.Receipt.Operation {
				t.Fatalf("the repeated apply = %+v (%v)", result, err)
			}
			if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, applied) {
				t.Fatalf("evidence = %q, want %q", h.workspace.evidence, applied)
			}
			if len(h.capability.applies) != applies || len(h.presenter.presented) != presented || h.workspace.mutations != mutations+1 {
				t.Fatalf("the repeated apply ran %d blocks, presented %d plans and opened %d transactions",
					len(h.capability.applies)-applies, len(h.presenter.presented)-presented, h.workspace.mutations-mutations)
			}
		})
	}
}

// An operation interrupted after its last block's outcome and before its
// record says done is finalized by its own verb alone. The other verb keeps
// its own decision: an apply refuses an incomplete destroy and a destroy
// supersedes an incomplete apply with a presented removal.
func TestARunningOperationWhoseBlocksAreAllDoneIsFinalized(t *testing.T) {
	ctx := context.Background()
	applied := []reconciliation.Verb{reconciliation.Apply}
	t.Run("an apply over its running apply settles", func(t *testing.T) {
		run := killedAt(ctx, t, nil, reconciliation.Apply, "replace <op>/operation.json#1")
		requireRunningWithEveryBlockDone(t, run, reconciliation.Apply)
		service, host := run.repairing()
		current, directories := operations(t, run.snapshot.workspace)
		presented, effects := len(run.rig.harness.presenter.presented), maps.Clone(run.snapshot.host.effects)
		result, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName})
		if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != current {
			t.Fatalf("the apply = %+v (%v)", result, err)
		}
		requireRepaired(t, run, host, presented, effects, current, directories)
	})
	t.Run("an apply of a changed input over its running apply refuses as over a completed one", func(t *testing.T) {
		run := killedAt(ctx, t, nil, reconciliation.Apply, "replace <op>/operation.json#1")
		service, _ := run.repairing()
		run.snapshot.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
		}}
		_, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Message != "the desired state changed after this apply completed" {
			t.Fatalf("the apply = %+v (%v)", reported, err)
		}
		if killRest(run.snapshot.workspace) != "applied" ||
			!bytes.Equal(run.snapshot.workspace.evidence, evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone)) {
			t.Fatalf("the refusal did not follow the finalization: %s under %q", killRest(run.snapshot.workspace), run.snapshot.workspace.evidence)
		}
	})
	t.Run("a destroy over its running destroy completes, releases and publishes pristine", func(t *testing.T) {
		run := killedAt(ctx, t, applied, reconciliation.Destroy, "replace <op>/operation.json#1")
		requireRunningWithEveryBlockDone(t, run, reconciliation.Destroy)
		service, host := run.repairing()
		current, directories := operations(t, run.snapshot.workspace)
		presented, effects := len(run.rig.harness.presenter.presented), maps.Clone(run.snapshot.host.effects)
		result, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
		if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != current {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		requireRepaired(t, run, host, presented, effects, current, directories)
	})
	t.Run("an apply over a running destroy whose blocks are all done still refuses", func(t *testing.T) {
		run := killedAt(ctx, t, applied, reconciliation.Destroy, "replace <op>/operation.json#1")
		service, _ := run.repairing()
		w := run.snapshot.workspace
		records, evidence, mutations := w.area.clone(), slices.Clone(w.evidence), w.mutations
		before := killStateOf(w, run.snapshot.binder, run.snapshot.host)
		_, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Message != "an incomplete destroy must be continued before another operation" {
			t.Fatalf("the apply = %+v (%v)", reported, err)
		}
		if !sameFiles(w.area, records) || !bytes.Equal(w.evidence, evidence) || w.mutations != mutations ||
			killStateOf(w, run.snapshot.binder, run.snapshot.host) != before {
			t.Fatal("the refused apply wrote something")
		}
	})
	t.Run("a destroy over a running apply whose blocks are all done presents a fresh removal", func(t *testing.T) {
		run := killedAt(ctx, t, nil, reconciliation.Apply, "replace <op>/operation.json#1")
		service, _ := run.repairing()
		w := run.snapshot.workspace
		superseded, _ := operations(t, w)
		presented := len(run.rig.harness.presenter.presented)
		records, evidence, mutations := w.area.clone(), slices.Clone(w.evidence), w.mutations
		before := killStateOf(w, run.snapshot.binder, run.snapshot.host)
		_, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.authorization" {
			t.Fatalf("the unauthorized destroy = %+v (%v)", reported, err)
		}
		if !sameFiles(w.area, records) || !bytes.Equal(w.evidence, evidence) || w.mutations != mutations ||
			killStateOf(w, run.snapshot.binder, run.snapshot.host) != before || len(run.rig.harness.presenter.presented) != presented {
			t.Fatal("the unauthorized destroy finalized the apply it supersedes")
		}
		result, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
		if err != nil || result.Settled || result.Receipt.State != "done" || result.Receipt.Operation == superseded {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		shown := run.rig.harness.presenter.presented
		if len(shown) != presented+1 || shown[presented].Continuation || shown[presented].Verb != string(reconciliation.Destroy) {
			t.Fatalf("the destroy presented %+v", shown[presented:])
		}
	})
}

func requireRunningWithEveryBlockDone(t *testing.T, run killedRun, verb reconciliation.Verb) {
	t.Helper()
	if rest := killRest(run.snapshot.workspace); rest != "incomplete "+string(verb)+" running" {
		t.Fatalf("the kill left %s", rest)
	}
	ctx := context.Background()
	store := operationstore.New(run.snapshot.workspace.area, killClock)
	current, _ := operations(t, run.snapshot.workspace)
	plan, err := store.ReadPlan(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.BlockStates(ctx, current, plan)
	if err != nil || pendingRemains(plan, states) {
		t.Fatalf("the kill left blocks %v (%v)", states, err)
	}
}

// An operation recorded unknown whose blocks are all done is finalized by its
// own verb alone, exactly as a running one is: its blocks prove completion and
// only its record lags. The own verb records it done and projects it without
// the token its plan consumes, a confirmation or a presented plan, and the
// other verb keeps its own decision: a destroy supersedes the apply with a
// presented removal and an apply refuses the destroy.
func TestAnUnknownOperationWhoseBlocksAreAllDoneIsFinalizedByItsOwnVerb(t *testing.T) {
	ctx := context.Background()
	t.Run("an apply over its unknown apply settles", func(t *testing.T) {
		h := unknownApplyWithEveryBlockDone(t)
		applied := currentOperation(t, h)
		h.service.options.Confirmer = nil
		applies, observes, presented := len(h.capability.applies), len(h.capability.observes), len(h.presenter.presented)
		result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
		if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.State != "done" || result.Receipt.Operation != applied {
			t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
		}
		requireRecordedWithEveryBlockDone(t, h, reconciliation.Apply, reconciliation.OperationDone)
		if want := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, want) {
			t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
		}
		if len(h.capability.applies) != applies || len(h.capability.observes) != observes || len(h.presenter.presented) != presented {
			t.Fatal("the apply ran or observed a block, or presented a plan")
		}
	})
	t.Run("a destroy over an unknown apply whose blocks are all done presents a fresh removal", func(t *testing.T) {
		h := unknownApplyWithEveryBlockDone(t)
		applied := currentOperation(t, h)
		h.service.options.Confirmer = nil
		records, evidence, mutations, presented := h.workspace.area.clone(), slices.Clone(h.workspace.evidence), h.workspace.mutations, len(h.presenter.presented)
		_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.authorization" {
			t.Fatalf("the unauthorized destroy = %+v (%v)", reported, err)
		}
		if !sameFiles(h.workspace.area, records) || !bytes.Equal(h.workspace.evidence, evidence) || h.workspace.mutations != mutations ||
			len(h.presenter.presented) != presented {
			t.Fatal("the unauthorized destroy finalized the apply it supersedes")
		}
		h.capability.destroys = nil
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
		if err != nil || result.Settled || result.Receipt.State != "done" || result.Receipt.Operation == applied {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		shown := h.presenter.presented
		if len(shown) != presented+1 || shown[presented].Continuation || shown[presented].Verb != string(reconciliation.Destroy) {
			t.Fatalf("the destroy presented %+v", shown[presented:])
		}
		if !slices.Equal(h.capability.destroys, []string{"alpha"}) {
			t.Fatalf("the removal took back %v", h.capability.destroys)
		}
	})
	t.Run("a destroy over its unknown destroy completes, releases and publishes pristine", func(t *testing.T) {
		h := unknownDestroyWithEveryBlockDone(t)
		removal := currentOperation(t, h)
		h.service.options.Confirmer = nil
		destroys, observes, presented := len(h.capability.destroys), len(h.capability.observes), len(h.presenter.presented)
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
		if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.State != "done" || result.Receipt.Operation != removal {
			t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
		}
		requireRecordedWithEveryBlockDone(t, h, reconciliation.Destroy, reconciliation.OperationDone)
		if !bytes.Equal(h.workspace.evidence, killPristine(t)) || len(h.workspace.reservations) != 0 || !slices.Equal(h.binder.released, []string{"bind-1"}) {
			t.Fatalf("the destroy left evidence %q, reservations %v and released %v", h.workspace.evidence, h.workspace.reservations, h.binder.released)
		}
		if len(h.capability.destroys) != destroys || len(h.capability.observes) != observes || len(h.presenter.presented) != presented {
			t.Fatal("the destroy ran or observed a block, or presented a plan")
		}
	})
	t.Run("an apply over an unknown destroy whose blocks are all done still refuses", func(t *testing.T) {
		h := unknownDestroyWithEveryBlockDone(t)
		records, evidence, mutations := h.workspace.area.clone(), slices.Clone(h.workspace.evidence), h.workspace.mutations
		released, reservations := slices.Clone(h.binder.released), slices.Clone(h.workspace.reservations)
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Message != "an incomplete destroy must be continued before another operation" {
			t.Fatalf("the apply = %+v (%v)", reported, err)
		}
		if !sameFiles(h.workspace.area, records) || !bytes.Equal(h.workspace.evidence, evidence) || h.workspace.mutations != mutations ||
			!slices.Equal(h.binder.released, released) || len(h.workspace.reservations) != len(reservations) {
			t.Fatal("the refused apply wrote or released something")
		}
	})
}

// unknownApplyWithEveryBlockDone applies alpha to an unknown outcome and then
// runs a removal whose resolution proves alpha done and whose record of what it
// proved fails, which leaves the apply unknown beside no block that is not done.
func unknownApplyWithEveryBlockDone(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	h.capability.consumes = map[string][]string{"alpha": dataLoss()}
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose block lost its outcome reported success")
	}
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	recorded := "replace " + path.Join(currentOperation(t, h), "operation.json")
	h.workspace.area.fail[recorded] = errors.New("interrupted")
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true}); err == nil {
		t.Fatal("a removal that could not record its resolution reported success")
	}
	delete(h.workspace.area.fail, recorded)
	if !slices.Equal(h.capability.observes, []string{"alpha"}) || len(h.capability.destroys) != 0 {
		t.Fatalf("the removal observed %v and took back %v", h.capability.observes, h.capability.destroys)
	}
	requireRecordedWithEveryBlockDone(t, h, reconciliation.Apply, reconciliation.OperationUnknown)
	return h
}

// unknownDestroyWithEveryBlockDone applies alpha and removes it with a removal
// whose record of alpha's completion lands and then reports a failure, as a
// rename whose read-back or sync failed does: the removal keeps alpha unknown
// and records itself unknown beside alpha's done record, still holding the
// reservations and the Secret binding it would release.
func unknownDestroyWithEveryBlockDone(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	h.capability.consumes = map[string][]string{"alpha": dataLoss()}
	h.capability.reservations = []prerequisites.HostReservation{{Context: testContextName, Kind: "artifact-server", Service: "alpha", Keys: []string{"socket:192.0.2.1:8443"}}}
	if err := authorized(ctx, h.service, reconciliation.Apply); err != nil {
		t.Fatal(err)
	}
	applied := currentOperation(t, h)
	area, completed := h.workspace.area, false
	area.landing = func(operation, target string, files map[string][]byte) error {
		if operation != "replace" || path.Base(target) != "state.json" || strings.HasPrefix(target, applied+"/") {
			return nil
		}
		if strings.Contains(string(files[path.Join(path.Dir(target), "attempt-000001.json")]), `"phase":"observed"`) {
			completed = true
			area.failAfter["replace "+target] = errors.New("the record could not be read back")
		}
		return nil
	}
	if err := authorized(ctx, h.service, reconciliation.Destroy); err == nil {
		t.Fatal("a removal whose completion record reported a failure reported success")
	}
	area.landing = nil
	if !completed || !slices.Equal(h.capability.destroys, []string{"alpha"}) {
		t.Fatalf("the removal never completed alpha: it took back %v", h.capability.destroys)
	}
	requireRecordedWithEveryBlockDone(t, h, reconciliation.Destroy, reconciliation.OperationUnknown)
	if len(h.workspace.reservations) == 0 || len(h.binder.released) != 0 {
		t.Fatalf("the unknown removal left reservations %v and released %v", h.workspace.reservations, h.binder.released)
	}
	return h
}

// requireRecordedWithEveryBlockDone holds the context's current operation to a
// verb and state beside alpha's done record.
func requireRecordedWithEveryBlockDone(t *testing.T, h *harness, verb reconciliation.Verb, state reconciliation.OperationState) {
	t.Helper()
	operation, states := durableOperation(t, h)
	if operation.Verb != verb || operation.State != state || !maps.Equal(states, map[string]reconciliation.BlockState{"alpha": reconciliation.BlockDone}) {
		t.Fatalf("the records read %s %s %v, want %s %s with alpha done", operation.Verb, operation.State, states, verb, state)
	}
}

// A fresh apply over a removal whose finalization an interruption cut short
// finishes it before it presents its own plan, so the plan an operator
// confirms is taken over a context already at rest.
func TestAFreshApplyOverAnUnfinalizedRemovalFinishesItFirst(t *testing.T) {
	ctx := context.Background()
	pristine := killPristine(t)
	run := killedAt(ctx, t, []reconciliation.Verb{reconciliation.Apply}, reconciliation.Destroy, "release secret binding#1")
	service, _ := run.repairing()
	w, binder := run.snapshot.workspace, run.snapshot.binder
	var seen []string
	service.options.Presenter = presenterFunc(func(_ context.Context, result PlanResult) error {
		seen = append(seen, result.Verb, killRest(w), strings.TrimSpace(string(w.evidence)))
		if killUnreleased(binder) != 0 || len(w.reservations) != 0 {
			seen = append(seen, "still held")
		}
		return nil
	})
	result, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
	if err != nil || result.Settled || result.Receipt.State != "done" {
		t.Fatalf("the apply = %+v (%v)", result, err)
	}
	if want := []string{"apply", "removed", strings.TrimSpace(string(pristine))}; !slices.Equal(seen, want) {
		t.Fatalf("at presentation the context held %q, want %q", seen, want)
	}
}

type presenterFunc func(context.Context, PlanResult) error

func (f presenterFunc) PresentLifecyclePlan(ctx context.Context, result PlanResult) error {
	return f(ctx, result)
}

// forgetfulWorkspace hands every transaction a publication of evidence that
// reports success and keeps nothing, as a store that lost the write would.
type forgetfulWorkspace struct{ *testWorkspace }

type forgetfulTransaction struct{ Transaction }

func (forgetfulTransaction) PublishEvidence(context.Context, []byte) error { return nil }

func (w forgetfulWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	return w.testWorkspace.MutateLifecycle(ctx, name, func(tx Transaction) error { return callback(forgetfulTransaction{tx}) })
}

// A finalization after which the context still needs one refuses rather than
// finalizing again or going on to authorize, present or run anything.
func TestAFinalizationThatLeavesAnotherBehindRefuses(t *testing.T) {
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	pending := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	h.workspace.evidence = pending
	h.service.workspace = forgetfulWorkspace{h.workspace}
	applies, presented, mutations := len(h.capability.applies), len(h.presenter.presented), h.workspace.mutations
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if result != nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the operation's finalization did not complete" || reported[0].Remediation != "repeat bootwright apply" {
		t.Fatalf("the apply = %+v, %+v (%v)", result, reported, err)
	}
	if len(h.capability.applies) != applies || len(h.presenter.presented) != presented || h.workspace.mutations != mutations+1 {
		t.Fatalf("the refusal ran %d blocks, presented %d plans and opened %d transactions",
			len(h.capability.applies)-applies, len(h.presenter.presented)-presented, h.workspace.mutations-mutations)
	}
	if !bytes.Equal(h.workspace.evidence, pending) {
		t.Fatalf("evidence = %q", h.workspace.evidence)
	}
}

// Both finalization writes re-prove, under the exclusive lock, the state they
// were decided from, so another invocation's work in between is never
// overwritten with a projection of what the context no longer holds.
func TestAFinalizationRefusesWhenTheContextChangedBeforeIt(t *testing.T) {
	ctx := context.Background()
	t.Run("an apply's finalization over a context another destroy removed", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		applied, removal := currentOperation(t, h), ""
		h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
		h.workspace.beforeMutation = func() {
			h.workspace.beforeMutation = nil
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			removal = currentOperation(t, h)
		}
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		requireContextChanged(t, err, "apply", "it was planned from operation "+applied+" (done), "+
			"and the context now holds operation "+removal+" (done)")
		if !bytes.Equal(h.workspace.evidence, killPristine(t)) {
			t.Fatalf("the refused finalization left evidence %q over a removed context", h.workspace.evidence)
		}
	})
	t.Run("a removal's pristine publication over an apply that followed it", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		reapplied, start := "", h.workspace.mutations
		// The removal's second transaction is the one that would publish pristine.
		h.workspace.beforeMutation = func() {
			if h.workspace.mutations != start+1 {
				return
			}
			h.workspace.beforeMutation = nil
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			reapplied = currentOperation(t, h)
		}
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		if result == nil || result.Receipt.State != "done" || reapplied == "" {
			t.Fatalf("the removal's receipt = %+v, reapplied %q", result, reapplied)
		}
		reported := diagnostics.Of(err)
		changed := "the context changed after this command read it: it was planned from operation " + result.Receipt.Operation +
			" (done), and the context now holds operation " + reapplied + " (done)"
		if len(reported) != 2 || reported[0].Message != changed {
			t.Fatalf("the removal failed with %+v (%v)", reported, err)
		}
		requireIncompleteRemoval(t, reported[1:], err)
		if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, applied) {
			t.Fatalf("evidence = %q, want the later apply's %q", h.workspace.evidence, applied)
		}
	})
}

// Records that hold a block that is not done prove no completion, so nothing
// finalizes them: a completed apply refuses as its repeat always does over
// such records, and a completed removal settles, both leaving the evidence,
// the bindings and every record exactly as they were.
func TestNoFinalizationOverContradictedRecords(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		prepare []reconciliation.Verb
		verb    reconciliation.Verb
		point   string
		refusal string
	}{
		"a completed apply with a lost block record": {
			verb: reconciliation.Apply, point: "publish evidence#2",
			refusal: "records no block completion for these blocks, so this input cannot be proved applied: b (pending)",
		},
		"a completed destroy with a lost block record": {
			prepare: []reconciliation.Verb{reconciliation.Apply}, verb: reconciliation.Destroy, point: "release secret binding#1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			run := killedAt(ctx, t, test.prepare, test.verb, test.point)
			w := run.snapshot.workspace
			current, _ := operations(t, w)
			lose(&harness{workspace: w}, path.Join(current, "blocks", "b")+"/")
			service, _ := run.repairing()
			records, mutations := w.area.clone(), w.mutations
			before := killStateOf(w, run.snapshot.binder, run.snapshot.host)
			var result *OperationResult
			var err error
			if test.verb == reconciliation.Apply {
				result, err = service.Apply(ctx, ApplyRequest{ContextName: testContextName})
			} else {
				result, err = service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
			}
			reported := diagnostics.Of(err)
			switch {
			case test.refusal == "" && (err != nil || !result.Settled):
				t.Fatalf("the %s = %+v (%v)", test.verb, result, err)
			case test.refusal != "" && (len(reported) != 1 || !strings.HasSuffix(reported[0].Message, test.refusal)):
				t.Fatalf("the %s = %+v (%v)", test.verb, reported, err)
			}
			if !sameFiles(w.area, records) || w.mutations != mutations || killStateOf(w, run.snapshot.binder, run.snapshot.host) != before {
				t.Fatal("contradicted records were finalized")
			}
		})
	}
}

// Repeating a verb whose operation's finalization is complete reads durable
// state under the shared lock only: it opens no transaction, whichever verb it
// is, even while its completed apply holds reservations.
func TestASettledVerbWithCompleteFinalizationOpensNoTransaction(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	h.capability.reservations = []prerequisites.HostReservation{{Context: testContextName, Kind: "artifact-server", Service: "alpha", Keys: []string{"socket:192.0.2.1:8443"}}}
	invoke := func(verb reconciliation.Verb, skip bool) (*OperationResult, error) {
		if verb == reconciliation.Apply {
			return h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: skip})
		}
		return h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: skip})
	}
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		if _, err := invoke(verb, true); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		mutations, asked := h.workspace.mutations, h.confirmer.asked
		result, err := invoke(verb, false)
		if err != nil || !result.Settled || result.Recovered != "" {
			t.Fatalf("the repeated %s = %+v (%v)", verb, result, err)
		}
		if h.workspace.mutations != mutations || h.confirmer.asked != asked {
			t.Fatalf("the repeated %s opened %d transactions and asked %d times", verb, h.workspace.mutations-mutations, h.confirmer.asked-asked)
		}
	}
}
