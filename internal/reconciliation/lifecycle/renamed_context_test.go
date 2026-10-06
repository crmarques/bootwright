package lifecycle

import (
	"context"
	"errors"
	"path"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// renamedWorkspace serves the harness's one context under another name, as a
// second context on the same host is served, so a test tells the context a
// command carries from the harness's own.
type renamedWorkspace struct {
	*testWorkspace
	name string
}

// renamed serves h's context as name from here on.
func renamed(h *harness, name string) {
	h.service.workspace = renamedWorkspace{testWorkspace: h.workspace, name: name}
}

func (w renamedWorkspace) admits(name string) error {
	if name != w.name {
		return errors.New("the selected context does not exist")
	}
	return nil
}

func (w renamedWorkspace) ReadLifecycle(ctx context.Context, name string, callback func(View) error) error {
	if err := w.admits(name); err != nil {
		return err
	}
	return w.testWorkspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		return callback(renamedView{View: view, name: w.name})
	})
}

func (w renamedWorkspace) RunLifecycle(ctx context.Context, name string, callback func(RunView) error) error {
	if err := w.admits(name); err != nil {
		return err
	}
	return w.testWorkspace.RunLifecycle(ctx, testContextName, func(view RunView) error {
		return callback(renamedRunView{RunView: view, name: w.name})
	})
}

func (w renamedWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	if err := w.admits(name); err != nil {
		return err
	}
	return w.testWorkspace.MutateLifecycle(ctx, testContextName, func(tx Transaction) error {
		return callback(renamedTransaction{Transaction: tx, name: w.name})
	})
}

type renamedView struct {
	View
	name string
}

func (v renamedView) Identity() ContextIdentity { return renamedIdentity(v.View, v.name) }

func (v renamedView) Controller() prerequisites.StorageView { return renamedController(v.View, v.name) }

type renamedRunView struct {
	RunView
	name string
}

func (v renamedRunView) Identity() ContextIdentity { return renamedIdentity(v.RunView, v.name) }

func (v renamedRunView) Controller() prerequisites.StorageView {
	return renamedController(v.RunView, v.name)
}

type renamedTransaction struct {
	Transaction
	name string
}

func (v renamedTransaction) Identity() ContextIdentity { return renamedIdentity(v.Transaction, v.name) }

func (v renamedTransaction) Controller() prerequisites.StorageView {
	return renamedController(v.Transaction, v.name)
}

func renamedIdentity(view View, name string) ContextIdentity {
	identity := view.Identity()
	identity.Name = name
	return identity
}

// renamedController names the harness context's controller binding and
// reservations by its other name, as the controller record names a context.
func renamedController(view View, name string) prerequisites.StorageView {
	controller := view.Controller()
	controller.State.Bindings = slices.Clone(controller.State.Bindings)
	for index := range controller.State.Bindings {
		if controller.State.Bindings[index].Context == testContextName {
			controller.State.Bindings[index].Context = name
		}
	}
	controller.State.Reservations = slices.Clone(controller.State.Reservations)
	for index := range controller.State.Reservations {
		if controller.State.Reservations[index].Context == testContextName {
			controller.State.Reservations[index].Context = name
		}
	}
	return controller
}

// contextStopping reports the machine block live, with the stop command a
// machine capability composes from the probe's context, and leaves every other
// block to the harness's capability.
type contextStopping struct {
	*testCapability
	machine string
}

func (c contextStopping) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	if probe.Block.ID != c.machine {
		return c.testCapability.Quiescent(ctx, probe)
	}
	return Quiescence{State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --context " + probe.Context + " --name " + probe.Block.Object}, nil
}

// A removal that would take back a machine still running names the command
// that stops it, as the machine's capability composes it for the context the
// removal runs in, and then the destroy to repeat in that same context with
// every token its plan consumes, so a copied remedy never stops or removes
// another context's machine of the same name.
func TestLiveRemovalNamesTheStopCommandWithItsContext(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab", "rhel-01")
	renamed(h, "lab-b")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.consumes = map[string][]string{"rhel-01": {reconciliation.AuthorizationDataLoss}}
	h.service.capabilities = testResolver{capability: contextStopping{testCapability: h.capability, machine: "rhel-01"}}
	_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab-b", Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	want := "stop it with bootwright machine stop --context lab-b --name rhel-01, then repeat bootwright destroy --context lab-b --authorize data-loss"
	if len(reported) != 1 || reported[0].Code != "lifecycle.live" || reported[0].Remediation != want {
		t.Fatalf("the removal over a running machine reported %+v, want the remedy %q", reported, want)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("the refused removal destroyed %v", h.capability.destroys)
	}
}

// Every attempt and every resolution a capability runs carries the context the
// operation runs in, because the commands a capability composes from it, such
// as a machine's stop command or a pre-boot refusal, must name that context
// and never another one's.
func TestEveryExecutionCarriesTheContextItRunsIn(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	renamed(h, "lab-b")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); firstCode(err) != "lifecycle.unknown" {
		t.Fatalf("the seeded unknown apply = %v", err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
		t.Fatalf("the resolving apply = %v", err)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
		t.Fatalf("the removal = %v", err)
	}
	attempts, resolutions := 0, 0
	for _, execution := range h.capability.executions {
		if execution.Context != "lab-b" {
			t.Fatalf("block %s attempt %d resolution %d ran in context %q, want lab-b",
				execution.Block.ID, execution.Attempt, execution.Resolution, execution.Context)
		}
		if execution.Resolution != 0 {
			resolutions++
		} else {
			attempts++
		}
	}
	if attempts != 2 || resolutions != 1 || len(h.capability.destroys) != 1 {
		t.Fatalf("ran %d attempts, %d resolutions and removals %v, want the apply, its resolution and one removal",
			attempts, resolutions, h.capability.destroys)
	}
}

// A refusal over a completed apply names the commands of the context it read:
// a changed input the removal that takes back what the apply owns and the
// apply that follows, and a lost block record the status that shows it.
func TestRefusalsOverACompletedApplyNameTheirContext(t *testing.T) {
	ctx := context.Background()
	t.Run("a changed input", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		renamed(h, "lab-b")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
		}}
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true})
		reported := diagnostics.Of(err)
		want := "take back what it owns with bootwright destroy --context lab-b, then run bootwright apply --context lab-b"
		if len(reported) != 1 || reported[0].Message != "the desired state changed after this apply completed" || reported[0].Remediation != want {
			t.Fatalf("the apply of a changed input = %+v (%v), want the remedy %q", reported, err, want)
		}
	})
	t.Run("a lost block record", func(t *testing.T) {
		h := newHarness(t, "alpha", "bravo")
		renamed(h, "lab-b")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		applied, _ := durableOperation(t, h)
		lose(h, path.Join(applied.ID, "blocks", "bravo")+"/")
		h.capability.destroys = nil
		_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab-b", SkipConfirmation: true})
		reported := diagnostics.Of(err)
		message := "the completed apply " + applied.ID + " records no block completion for these blocks, and a removal that skipped one would leave its effect in place: bravo (pending)"
		want := "review its durable state with bootwright status --context lab-b"
		if len(reported) != 1 || reported[0].Message != message || reported[0].Remediation != want {
			t.Fatalf("the removal = %+v (%v), want %q with %q", reported, err, message, want)
		}
		if len(h.capability.destroys) != 0 {
			t.Fatalf("the refused removal destroyed %v", h.capability.destroys)
		}
	})
}

// A continuation of a context the controller state records no binding for
// refuses before any effect, naming that context's way out that works where it
// refuses. The apply that would bind continues the incomplete apply and
// repeats the refusal, so the remedy is the destroy that supersedes it, after
// which the apply binds again. A removal no destroy may supersede names the
// binding's restoration, which lets it continue, or the context's deletion.
func TestAContinuationOfAnUnboundContextNamesAnExitThatWorks(t *testing.T) {
	ctx := context.Background()
	unbound := func(t *testing.T, h *harness, verb reconciliation.Verb, contextName, want string) {
		t.Helper()
		before := durableOf(h)
		var err error
		if verb == reconciliation.Destroy {
			_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: contextName, SkipConfirmation: true})
		} else {
			_, err = h.service.Apply(ctx, ApplyRequest{ContextName: contextName, SkipConfirmation: true})
		}
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "controller.identity" || reported[0].Message != "this context is not bound to a controller host" || reported[0].Remediation != want {
			t.Fatalf("the %s continuation = %+v (%v), want the remedy %q", verb, reported, err, want)
		}
		if !before.equal(durableOf(h)) {
			t.Fatalf("the refused %s continuation changed durable state or reached the host", verb)
		}
	}
	t.Run("an incomplete apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		renamed(h, "lab-b")
		h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}}
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err == nil {
			t.Fatal("the seeded failure applied")
		}
		h.capability.outcomeFor = nil
		h.workspace.controller.State.Bindings = nil
		want := "take back what this operation owns with bootwright destroy --context lab-b, after which bootwright apply --context lab-b binds the context again, or restore the controller state that recorded this context's binding from a matching backup"
		unbound(t, h, reconciliation.Apply, "lab-b", want)
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("the destroy the remedy names = %+v (%v)", result, err)
		}
		if result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil || result.Receipt.State != string(reconciliation.OperationDone) || len(h.workspace.controller.State.Bindings) != 1 {
			t.Fatalf("the apply after it = %+v (%v) with bindings %+v", result, err, h.workspace.controller.State.Bindings)
		}
	})
	t.Run("an unknown removal", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		unknownRemoval(t, h)
		registered, _ := durableOperation(t, h)
		bindings := slices.Clone(h.workspace.controller.State.Bindings)
		h.workspace.controller.State.Bindings = nil
		want := "restore the controller state that recorded this context's binding from a matching backup, or delete the context with bootwright context delete --name lab --purge --allow-orphans, which abandons what it may still own"
		unbound(t, h, reconciliation.Destroy, testContextName, want)
		h.workspace.controller.State.Bindings = bindings
		h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("the continuation under the restored binding = %+v (%v)", result, err)
		}
	})
}

// A completed removal whose context moved while it gave back its bindings
// refuses its pristine publication naming the destroy of the context it ran
// in, which the record it captured does not carry by itself.
func TestACompletedRemovalWhoseContextMovedNamesItsDestroy(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	renamed(h, "lab-b")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.binder.kill = func(point string) error {
		if point == "release secret binding" {
			h.binder.kill = nil
			lose(h, "index.json")
		}
		return nil
	}
	_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab-b", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	want := "repeat bootwright destroy --context lab-b to plan from what the context holds now"
	if len(reported) != 2 || reported[0].Remediation != want ||
		reported[1].Remediation != "repeat bootwright destroy --context lab-b to finish it" {
		t.Fatalf("the removal = %+v (%v), want the remedy %q", reported, err, want)
	}
}
