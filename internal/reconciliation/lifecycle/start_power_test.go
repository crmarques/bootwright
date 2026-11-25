package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (w *testWorkspace) AdmitLifecycle(ctx context.Context, name string, check func(View) error, write func(Transaction) error) error {
	if err := w.ReadLifecycle(ctx, name, check); err != nil {
		return err
	}
	return w.MutateLifecycle(ctx, name, write)
}

func (c *testCapability) StartPowerSecrets(block reconciliation.Block) ([]string, error) {
	return nil, nil
}

func (c *testCapability) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	return Quiescence{State: Quiescent}, ctx.Err()
}

type powerMachine struct {
	*testCapability
	readings []Probe
	state    string
}

func (m *powerMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	m.readings = append(m.readings, probe)
	return Quiescence{State: m.state, Reason: "native state"}, nil
}

type promptedPowerMachine struct {
	*powerMachine
	events *[]string
}

func (m *promptedPowerMachine) PromptApplyStart(_ context.Context, probe Probe) error {
	*m.events = append(*m.events, "prompt "+probe.Block.ID)
	return nil
}

func (m *promptedPowerMachine) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	*m.events = append(*m.events, "read "+probe.Block.ID)
	return m.powerMachine.StartPower(ctx, probe)
}

func TestAFreshApplyAsksForEveryPasswordBeforeItReadsMachinePowerAndShowsEachReading(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	progress := &testProgress{}
	h.service.options.Progress = progress
	var events []string
	h.service.options.BeginPrompts = func(ctx context.Context) (context.Context, func()) {
		events = append(events, "begin prompts")
		return ctx, func() { events = append(events, "end prompts") }
	}
	bindings := claimResolver{}.Bindings()
	machine := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Live}
	h.service.capabilities = claimResolver{
		bindings[0]: &promptedPowerMachine{machine, &events}, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	reading := "check:prove Machine/node-a is powered off, changing nothing::"
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if ds := diagnostics.Of(err); result != nil || len(ds) != 1 || ds[0].Code != "lifecycle.start-power" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if want := []string{"begin prompts", "prompt machine-first-v1-node-a", "read machine-first-v1-node-a", "end prompts"}; !slices.Equal(events, want) {
		t.Fatalf("a refused start ran %v, want %v", events, want)
	}
	if want := []string{reading + "running:1/1", reading + "failed:1/1"}; !slices.Equal(progress.rows, want) {
		t.Fatalf("a refused reading reported %v, want %v", progress.rows, want)
	}
	events, progress.rows, machine.state = nil, nil, Quiescent
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(events) < 3 || !slices.Equal(events[:3], []string{"begin prompts", "prompt machine-first-v1-node-a", "read machine-first-v1-node-a"}) {
		t.Fatalf("an admitted start ran %v", events)
	}
	if len(progress.rows) < 2 || !slices.Equal(progress.rows[:2], []string{reading + "running:1/1", reading + "ok:1/1"}) {
		t.Fatalf("an admitted reading reported %v", progress.rows)
	}
}

func TestFreshMachineAdmissionLeavesNoStateOnRefusalAndRecoveryKeepsItsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machine := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Live}
	h.service.capabilities = claimResolver{
		bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	apply := func() (*OperationResult, error) {
		return h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	}
	destroy := func() (*OperationResult, error) {
		return h.service.Destroy(ctx, DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	}
	refused := func(run func() (*OperationResult, error)) {
		t.Helper()
		mutations := h.workspace.mutations
		files := maps.Clone(h.workspace.area.files)
		logs := maps.Clone(h.workspace.logArea.files)
		evidence := slices.Clone(h.workspace.evidence)
		result, err := run()
		if ds := diagnostics.Of(err); result != nil || len(ds) != 1 || ds[0].Code != "lifecycle.start-power" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		if mutations != h.workspace.mutations || !reflect.DeepEqual(files, h.workspace.area.files) || !reflect.DeepEqual(logs, h.workspace.logArea.files) || !slices.Equal(evidence, h.workspace.evidence) {
			t.Fatal("a refused start changed state")
		}
	}
	refused(apply)
	machine.state = Unproved
	refused(apply)
	if len(machine.applies) != 0 || len(machine.readings) != 2 {
		t.Fatalf("attempts=%v readings=%d", machine.applies, len(machine.readings))
	}
	machine.state = Quiescent
	done, err := apply()
	if err != nil {
		t.Fatal(err)
	}
	machine.state = Live
	refused(destroy)
	last := machine.readings[len(machine.readings)-1]
	if last.ApplyOperation != done.Receipt.Operation {
		t.Fatalf("destroy probed owner %q; want %q", last.ApplyOperation, done.Receipt.Operation)
	}
	if len(machine.destroys) != 0 {
		t.Fatal("destroy ran an inverse before admission")
	}
	if _, err := apply(); err != nil || len(machine.readings) != 4 {
		t.Fatalf("settled apply read power or refused: %v", err)
	}
}

func TestPowerAdmissionGuardsNewRemovalOverPartialApplyAndExemptsRegisteredRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machine := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Quiescent}
	h.service.capabilities = claimResolver{
		bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	machine.applyErr = errors.New("interrupted")
	_, err := h.service.Apply(ctx, ApplyRequest{SkipConfirmation: true})
	if err == nil {
		t.Fatal("injected apply failure did not stop")
	}
	first := currentOperation(t, h)
	machine.applyErr = nil
	machine.state = Live
	if _, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true}); err == nil || len(machine.readings) != 2 || currentOperation(t, h) != first {
		t.Fatalf("new destroy over a partial apply: %v", err)
	}
	resumed, err := h.service.Apply(ctx, ApplyRequest{SkipConfirmation: true})
	if err != nil || resumed.Receipt.Operation != first || len(machine.readings) != 2 {
		t.Fatalf("apply did not continue on a running machine: %+v %v", resumed, err)
	}
	rewriteState(t, h, path.Join(first, "operation.json"), string(reconciliation.OperationDone))
	h.workspace.evidence = []byte(`{"operation":"pending","ownership":"retained","version":1}`)
	before := slices.Clone(h.workspace.evidence)
	mutations := h.workspace.mutations
	_, err = h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true})
	if ds := diagnostics.Of(err); len(ds) != 1 || ds[0].Code != "lifecycle.start-power" || !slices.Equal(before, h.workspace.evidence) || mutations != h.workspace.mutations {
		t.Fatalf("dirty finalization changed before refusal: %v", err)
	}
	machine.state = Quiescent
	prior, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true})
	if err != nil || prior.Recovered != RecoveredBeforeStart || prior.Receipt.Next != "destroy" || prior.Verb != "apply" || currentOperation(t, h) != first || len(machine.destroys) != 0 {
		t.Fatalf("guarded finalization started the next flow: %+v, %v", prior, err)
	}
	machine.state = Live
	if _, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true}); err == nil || len(machine.destroys) != 0 {
		t.Fatal("fresh destroy skipped admission after recovery")
	}
}

func TestReplacementDestroyStopsAProvedLiveMachineInsideARegisteredAttempt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	m := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Quiescent}
	h.service.capabilities = claimResolver{bindings[0]: m, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1")}
	completeApply(t, h)
	m.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if err := destroyWith(h); err == nil {
		t.Fatal("destroy should have been interrupted")
	}
	first, _ := durableOperation(t, h)
	m.state = Live
	stops := 0
	h.service.options.DriveMachine = func(_ context.Context, e Execution, proof BlockEvidence, on bool) error {
		if on || ReadOnlyRun(e) || e.Operation == first.ID || proof.Object != "node-a" || proof.State != reconciliation.BlockDone || len(proof.Evidence) == 0 {
			t.Fatalf("invalid stop: %+v, %+v", e, proof)
		}
		stops++
		m.state = Quiescent
		return nil
	}
	if err := destroyWith(h); err != nil || stops != 1 {
		t.Fatalf("replacement issued %d registered stops: %v", stops, err)
	}
}

type concurrentPowerMachine struct {
	*testCapability
	read func(context.Context, Probe) (Quiescence, error)
}

func (m *concurrentPowerMachine) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	if m.read == nil {
		return m.testCapability.StartPower(ctx, probe)
	}
	return m.read(ctx, probe)
}

func TestInvocationParallelismBoundsApplyAndDestroyStartPower(t *testing.T) {
	t.Parallel()
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, limit := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s/%d", verb, limit), func(t *testing.T) {
				t.Parallel()
				h := newHarness(t)
				nodes := make([]string, MaxRunningBlocks+1)
				for i := range nodes {
					nodes[i] = fmt.Sprintf("node-%02d", i)
				}
				bindings := claimResolver{}.Bindings()
				machine := &concurrentPowerMachine{testCapability: claimingCapability("machine-first-v1", nodes...)}
				h.service.capabilities = claimResolver{
					bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
				}
				if verb == reconciliation.Destroy {
					completeApply(t, h)
				}
				h.service.options.Concurrency = 0
				checks := &testProgress{}
				h.service.options.Progress = checks
				type scopeKey struct{}
				var released bool
				h.service.options.BeginInvocation = func(ctx context.Context) (context.Context, func()) {
					return context.WithValue(ctx, scopeKey{}, true), func() { released = true }
				}
				watch := newOverlap(len(nodes))
				t.Cleanup(watch.open)
				machine.read = func(ctx context.Context, probe Probe) (Quiescence, error) {
					watch.enter(probe.Block.Object)
					defer watch.leave(probe.Block.Object)
					if !probe.Admission || ctx.Value(scopeKey{}) != true {
						return Quiescence{}, errors.New("power proof lacked its read-only invocation scope")
					}
					if slices.Contains(nodes[:2], probe.Block.Object) {
						return Quiescence{State: Live, Reason: "native state", Remedy: "stop the machines"}, nil
					}
					return Quiescence{State: Quiescent}, nil
				}

				mutations := h.workspace.mutations
				finished := make(chan error, 1)
				go func() {
					var err error
					if verb == reconciliation.Apply {
						_, err = h.service.Apply(t.Context(), ApplyRequest{SkipConfirmation: true, Parallelism: limit})
					} else {
						_, err = h.service.Destroy(t.Context(), DestroyRequest{SkipConfirmation: true, Parallelism: limit})
					}
					finished <- err
				}()
				bound := limit
				if bound == 0 {
					bound = MaxRunningBlocks
				}
				for remaining := len(nodes); remaining > 0; remaining -= bound {
					watch.gather(t, min(bound, remaining))
					if remaining > bound {
						watch.admit()
					}
				}
				watch.open()
				ds := diagnostics.Of(returnedWithin(t, finished))
				want := "cannot start " + string(verb) + ": Machine/" + nodes[0] + " (native state), Machine/" + nodes[1] + " (native state)"
				if len(ds) != 1 || ds[0].Code != "lifecycle.start-power" || ds[0].Message != want ||
					strings.Count(ds[0].Remediation, "stop the machines") != 1 {
					t.Fatalf("power refusal = %+v, want %q", ds, want)
				}
				if watch.highest() != bound || !released || h.workspace.mutations != mutations {
					t.Fatalf("peak = %d, scope released = %v, writes = %d", watch.highest(), released, h.workspace.mutations-mutations)
				}
				rows := checks.reported()
				if len(rows) != 2*len(nodes) {
					t.Fatalf("checks reported %v", rows)
				}
				for i, node := range nodes {
					prefix := "check:prove Machine/" + node + " is powered off, changing nothing::"
					suffix := fmt.Sprintf(":%d/%d", i+1, len(nodes))
					status := "ok"
					if i < 2 {
						status = "failed"
					}
					running, settled := slices.Index(rows, prefix+"running"+suffix), slices.Index(rows, prefix+status+suffix)
					if running < 0 || settled <= running {
						t.Fatalf("no matching start and outcome for %s in %v", node, rows)
					}
				}
			})
		}
	}
}

func TestCanceledStartPowerJoinsInFlightReadingsWithoutAdmittingMore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, exited := make(chan string, 3), make(chan string, 3)
	joined := 0
	h.service.options.BeginInvocation = func(ctx context.Context) (context.Context, func()) {
		return ctx, func() { joined = len(exited) }
	}
	bindings := claimResolver{}.Bindings()
	machine := &concurrentPowerMachine{
		testCapability: claimingCapability("machine-first-v1", "node-a", "node-b", "node-c"),
		read: func(ctx context.Context, probe Probe) (Quiescence, error) {
			node := probe.Block.Object
			entered <- node
			<-ctx.Done()
			exited <- node
			// Even positive in-flight results cannot admit a canceled invocation.
			return Quiescence{State: Quiescent}, nil
		},
	}
	h.service.capabilities = claimResolver{
		bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(ctx, ApplyRequest{SkipConfirmation: true, Parallelism: 2})
		finished <- err
	}()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(gatherWithin):
			t.Fatal("two power readings did not enter")
		}
	}
	cancel()
	if err := returnedWithin(t, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled apply: %v", err)
	}
	if joined != 2 || len(entered) != 0 || h.workspace.mutations != 0 {
		t.Fatalf("joined %d readings, admitted %d more and wrote %d times", joined, len(entered), h.workspace.mutations)
	}
}
