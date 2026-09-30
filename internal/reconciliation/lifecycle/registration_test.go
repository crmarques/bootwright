package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// journal is every durable publication of one harness in the order it lands:
// evidence with the bytes it publishes, each Secret binding issued or released
// by identity, the controller binding and reservation publications, and each
// operation record write, with each block's effect as it starts.
type journal struct {
	mutex   sync.Mutex
	entries []string
}

func (j *journal) add(entry string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.entries = append(j.entries, entry)
}

func (j *journal) read() []string {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	return slices.Clone(j.entries)
}

// journaled wires the harness's landing and kill hooks, its capability's hold
// and its service's transactions and binder into one journal.
func journaled(h *harness) *journal {
	j := &journal{}
	h.workspace.kill = func(point string) error {
		if point != "publish evidence" {
			j.add(point)
		}
		return nil
	}
	h.workspace.area.landing = func(operation, target string, _ map[string][]byte) error {
		j.add(operation + " " + killElide(target))
		return nil
	}
	h.capability.hold = func(block string) { j.add("effect " + block) }
	h.service.workspace = journalingWorkspace{testWorkspace: h.workspace, journal: j}
	h.service.binder = journalingBinder{testBinder: h.binder, journal: j}
	return j
}

type journalingWorkspace struct {
	*testWorkspace
	journal *journal
}

func (w journalingWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	return w.testWorkspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		return callback(journalingTransaction{Transaction: tx, journal: w.journal})
	})
}

type journalingTransaction struct {
	Transaction
	journal *journal
}

func (tx journalingTransaction) PublishEvidence(ctx context.Context, data []byte) error {
	var evidence struct {
		Operation string `json:"operation"`
		Ownership string `json:"ownership"`
	}
	_ = json.Unmarshal(data, &evidence)
	tx.journal.add("publish evidence " + evidence.Operation + "/" + evidence.Ownership)
	return tx.Transaction.PublishEvidence(ctx, data)
}

type journalingBinder struct {
	*testBinder
	journal *journal
}

func (b journalingBinder) Bind(ctx context.Context, request custody.BindRequest) (secretstore.Binding, error) {
	binding, err := b.testBinder.Bind(ctx, request)
	if err == nil {
		b.journal.add("bind " + binding.ID)
	}
	return binding, err
}

func (b journalingBinder) Release(ctx context.Context, request custody.BindingRequest) (bool, error) {
	b.journal.add("release " + request.BindingID)
	return b.testBinder.Release(ctx, request)
}

// requireOrder fails unless every entry appears in the journal in this order.
func requireOrder(t *testing.T, entries []string, ordered ...string) {
	t.Helper()
	at := -1
	for _, want := range ordered {
		next := slices.Index(entries[at+1:], want)
		if next < 0 {
			t.Fatalf("the journal does not hold %q in order %q: %q", want, ordered, entries)
		}
		at += next + 1
	}
}

// strand issues a Secret binding no operation names, as a registration killed
// after its binding and before its index leaves one.
func strand(t *testing.T, h *harness) string {
	t.Helper()
	binding, err := h.binder.Bind(context.Background(), custody.BindRequest{ContextName: testContextName, Names: []string{"artifact-server-tls"}})
	if err != nil {
		t.Fatal(err)
	}
	return binding.ID
}

// unopenable issues bindings whose material cannot be reopened, as a custody
// store that fails between the two calls does.
type unopenable struct{ *testBinder }

func (unopenable) Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	return nil, errors.New("the bound material cannot be reopened")
}

func reservationOf(service string) []prerequisites.HostReservation {
	return []prerequisites.HostReservation{{Context: testContextName, Kind: "artifact-server", Service: service, Keys: []string{"socket:192.0.2.1:8443"}}}
}

// A fresh apply raises its running evidence before it binds a Secret, claims
// the controller host, reserves or writes a record, and publishes no other
// evidence before its first effect, so nothing it holds is ever covered by
// evidence that lets the context be updated or deleted.
func TestEvidenceProtectsTheContextBeforeItBindsOrReserves(t *testing.T) {
	h := newHarness(t, "alpha")
	h.capability.reservations = reservationOf("alpha")
	j := journaled(h)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	entries := j.read()
	effect := slices.Index(entries, "effect alpha")
	if effect < 0 {
		t.Fatalf("the apply performed no effect: %q", entries)
	}
	var evidence []string
	for _, entry := range entries[:effect] {
		if strings.HasPrefix(entry, "publish evidence") {
			evidence = append(evidence, entry)
		}
	}
	if !slices.Equal(evidence, []string{"publish evidence pending/retained"}) || entries[0] != evidence[0] {
		t.Fatalf("before its first effect the apply published evidence %q, first writing %q", evidence, entries[0])
	}
	requireOrder(t, entries[:effect], "publish evidence pending/retained", "bind bind-1", "publish binding", "publish reservations",
		"write <op>/plan.json", "write <op>/operation.json", "replace index.json")
}

// A fresh removal raises its running evidence before its registration writes
// anything, because once the index names it no other invocation can restore
// that index, and a registered removal must never run under evidence that lets
// the context be updated.
func TestARemovalRaisesItsEvidenceBeforeItRegisters(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	j := journaled(h)
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	entries := j.read()
	raised, planned := slices.Index(entries, "publish evidence pending/retained"), slices.Index(entries, "write <op>/plan.json")
	registered := slices.Index(entries, "replace index.json")
	if raised < 0 || planned < raised || registered < raised {
		t.Fatalf("the removal raised its evidence at %d, wrote its plan at %d and its index at %d: %q", raised, planned, registered, entries)
	}
}

// A fresh apply claims its operation's directory in the transaction that
// raises its evidence, and before it raises it, so no running evidence ever
// lands without adding a directory. The directory already exists, empty,
// when the apply binds its Secrets, and the operation it registers is that
// directory's.
func TestAFreshApplyClaimsItsOperationBeforeItBinds(t *testing.T) {
	h := newHarness(t, "alpha")
	var raising, claimed []string
	filled, raised := false, false
	h.workspace.kill = func(point string) error {
		if point == "publish evidence" && !raised {
			_, raising = operations(t, h.workspace)
			raised = true
		}
		return nil
	}
	h.binder.kill = func(point string) error {
		if point == "publish secret binding" {
			_, claimed = operations(t, h.workspace)
			for _, directory := range claimed {
				filled = filled || h.workspace.area.written(directory)
			}
		}
		return nil
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || filled || !slices.Equal(raising, claimed) {
		t.Fatalf("when the apply raised its evidence it held directories %v, and when it bound %v, filled %t", raising, claimed, filled)
	}
	if record, _ := durableOperation(t, h); record.ID != claimed[0] || !slices.Equal(record.Bindings, []string{"bind-1"}) {
		t.Fatalf("the apply registered %s bound to %v, not its claim %s", record.ID, record.Bindings, claimed[0])
	}
	if _, directories := operations(t, h.workspace); !slices.Equal(directories, claimed) {
		t.Fatalf("the apply left directories %v", directories)
	}
}

// Evidence that cannot be raised protects nothing, so the apply binds,
// reserves and registers nothing. A publication that landed before it failed
// is restored to what the index implies, and one that never landed is left
// alone, so the failure is the only one reported. Either way the evidence
// reads pristine again, so the apply's empty claim is reclaimed.
func TestAFailedEvidencePublicationBindsNothing(t *testing.T) {
	refused := failure("lifecycle.state", "the evidence could not be published", "restore the context store")
	for name, landed := range map[string]bool{"refused before it landed": false, "failed after it landed": true} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.capability.reservations = reservationOf("alpha")
			if landed {
				h.workspace.landThenFail = refused
			} else {
				h.workspace.failPublish = refused
			}
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the evidence could not be published" {
				t.Fatalf("the apply failed with %+v (%v)", reported, err)
			}
			current, directories := operations(t, h.workspace)
			if h.binder.issued != 0 || h.workspace.binds != 0 || len(h.workspace.reservations) != 0 || current != "" ||
				len(directories) != 0 {
				t.Fatalf("the apply bound %d, claimed the host %d times, reserved %v and left %q %v",
					h.binder.issued, h.workspace.binds, h.workspace.reservations, current, directories)
			}
			if len(h.capability.applies) != 0 || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
				t.Fatalf("the apply ran %v and left evidence %q", h.capability.applies, h.workspace.evidence)
			}
		})
	}
}

// A registration whose index provably does not name its operation registered
// nothing, so what it raised is given back: the binding the apply created and
// the running evidence, restored to what the index implies once read again,
// which for a removal is its apply's state as its own resolutions left it.
func TestARegistrationThatProvablyFailedRestoresTheEvidence(t *testing.T) {
	ctx := context.Background()
	removal := "op-" + strings.Repeat("02", 16)
	t.Run("a first apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		h.workspace.area.fail["replace index.json"] = errors.New("interrupted")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an interrupted registration reported success")
		}
		if !bytes.Equal(h.workspace.evidence, killPristine(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
			t.Fatalf("evidence %q, released %v", h.workspace.evidence, h.binder.released)
		}
	})
	t.Run("a first apply whose binding cannot be reopened", func(t *testing.T) {
		h := newHarness(t, "alpha")
		h.service.binder = unopenable{h.binder}
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an apply without its material reported success")
		}
		current, directories := operations(t, h.workspace)
		if current != "" || len(directories) != 0 {
			t.Fatalf("the apply left %q %v", current, directories)
		}
		if !bytes.Equal(h.workspace.evidence, killPristine(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
			t.Fatalf("evidence %q, released %v", h.workspace.evidence, h.binder.released)
		}
	})
	t.Run("a removal of a completed apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		h.workspace.area.fail["write "+path.Join(removal, "plan.json")] = errors.New("interrupted")
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an interrupted registration reported success")
		}
		if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, applied) {
			t.Fatalf("evidence = %q, want %q", h.workspace.evidence, applied)
		}
		if len(h.binder.released) != 0 {
			t.Fatalf("a removal released the binding it inherited: %v", h.binder.released)
		}
	})
	t.Run("a removal of an unknown apply its resolution proved failed", func(t *testing.T) {
		h := newHarness(t, "alpha")
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an unknown outcome reported success")
		}
		h.capability.observations = []Observation{{Effect: reconciliation.EffectNoEffect}}
		h.workspace.area.fail["write "+path.Join(removal, "plan.json")] = errors.New("interrupted")
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an interrupted registration reported success")
		}
		if record, _ := durableOperation(t, h); record.State != reconciliation.OperationFailed {
			t.Fatalf("the resolution left the apply %s", record.State)
		}
		if failed := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationFailed); !bytes.Equal(h.workspace.evidence, failed) {
			t.Fatalf("evidence = %q, want %q", h.workspace.evidence, failed)
		}
	})
}

// An apply interrupted after it bound and before it registered gives back
// what it raised although its own context is cancelled: the binding it created
// is released and the evidence restored, so the interruption leaves nothing
// running on disk.
func TestAnApplyInterruptedBeforeItRegistersGivesBackWhatItRaised(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHarness(t, "alpha")
	h.binder.kill = func(point string) error {
		if point == "publish secret binding" {
			cancel()
		}
		return nil
	}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted apply reported success")
	}
	current, _ := operations(t, h.workspace)
	if current != "" || !slices.Equal(h.binder.released, []string{"bind-1"}) || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("the interrupted apply left %q, released %v under evidence %q", current, h.binder.released, h.workspace.evidence)
	}
}

// A restoration that fails is reported after the failure that caused it, so
// the owner learns that the running evidence stayed raised.
func TestAFailedRestorationIsReportedBesideItsCause(t *testing.T) {
	h := newHarness(t, "alpha")
	h.binder.kill = func(point string) error {
		if point != "publish secret binding" {
			return nil
		}
		h.workspace.failPublish = failure("lifecycle.state", "the evidence could not be restored", "restore the context store")
		return failure("secret.source", "binding requires current material matching every declaration", "")
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 2 || reported[0].Message != "binding requires current material matching every declaration" ||
		reported[1].Message != "the evidence could not be restored" {
		t.Fatalf("the apply failed with %+v (%v)", reported, err)
	}
	if running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning); !bytes.Equal(h.workspace.evidence, running) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, running)
	}
}

// A registration that failed after its reservations landed leaves them held
// by no operation, so its running evidence stays and keeps the context from
// being deleted; the binding it created is still released. The next destroy
// releases the reservations and publishes pristine evidence.
func TestAFailedRegistrationWithReservationsKeepsItsEvidence(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	h.capability.reservations = reservationOf("alpha")
	plan := "write " + path.Join("op-"+strings.Repeat("01", 16), "plan.json")
	h.workspace.area.fail[plan] = errors.New("interrupted")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	if !bytes.Equal(h.workspace.evidence, running) || len(h.workspace.reservations) == 0 || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("evidence %q, reservations %v, released %v", h.workspace.evidence, h.workspace.reservations, h.binder.released)
	}
	delete(h.workspace.area.fail, plan)
	h.service.options.Confirmer = nil
	result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	if err != nil || !result.Settled || result.Recovered != RecoveredRelease {
		t.Fatalf("the destroy = %+v (%v)", result, err)
	}
	if len(h.workspace.reservations) != 0 || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("the destroy left %v under evidence %q", h.workspace.reservations, h.workspace.evidence)
	}
}

// An index whose write failed after it landed may name the operation, which
// then owns its binding for its whole lifetime and needs its running evidence.
// An index that then cannot be read back may name it just the same. Both stay,
// nothing is collected, and the next apply continues that operation with the
// binding it registered.
func TestARegistrationThatMayHaveHappenedKeepsItsBinding(t *testing.T) {
	for name, unreadable := range map[string]bool{"an index that names it": false, "an index that cannot be read back": true} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t, "alpha")
			stranded := strand(t, h)
			h.workspace.area.failAfter["replace index.json"] = errors.New("the index could not be read back")
			if unreadable {
				h.workspace.area.landing = func(operation, target string, _ map[string][]byte) error {
					if operation == "replace" && target == "index.json" {
						h.workspace.area.landing = nil
						h.workspace.area.fail["read index.json"] = errors.New("the index cannot be read")
					}
					return nil
				}
			}
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("a registration that could not be read back reported success")
			}
			delete(h.workspace.area.fail, "read index.json")
			record, _ := durableOperation(t, h)
			if record.State != reconciliation.OperationRunning || !slices.Equal(record.Bindings, []string{"bind-2"}) {
				t.Fatalf("the index names %s (%s) bound to %v", record.ID, record.State, record.Bindings)
			}
			if len(h.binder.released) != 0 {
				t.Fatalf("released %v beside %s", h.binder.released, stranded)
			}
			if running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning); !bytes.Equal(h.workspace.evidence, running) {
				t.Fatalf("evidence = %q, want %q", h.workspace.evidence, running)
			}
			result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			if err != nil || result.Receipt.Operation != record.ID || result.Receipt.State != "done" {
				t.Fatalf("the continuation = %+v (%v)", result, err)
			}
			if shown := h.presenter.presented; !shown[len(shown)-1].Continuation || !slices.Equal(h.capability.applies, []string{"alpha"}) {
				t.Fatalf("the next apply did not continue the operation: applies %v", h.capability.applies)
			}
		})
	}
}

// interruptAtLocation interrupts an invocation right after its registration,
// where the operation's log location is named, before any block starts.
type interruptAtLocation struct{ cancel context.CancelFunc }

func (interruptAtLocation) ReportProgress(context.Context, ProgressEvent) {}

func (p interruptAtLocation) ReportLogLocation(context.Context, string) { p.cancel() }

// registeredAndInterrupted leaves a registered apply that started nothing
// running, as an invocation interrupted right after its registration does.
func registeredAndInterrupted(t *testing.T, h *harness) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.service.options.Progress = interruptAtLocation{cancel: cancel}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted apply reported success")
	}
	h.service.options.Progress = nil
	if record, states := durableOperation(t, h); record.State != reconciliation.OperationRunning || states["alpha"] != reconciliation.BlockPending {
		t.Fatalf("the interruption left %s with %v", record.State, states)
	}
}

// A continuation raises the running evidence before it starts anything, so a
// registered operation left under pristine evidence, as an interruption before
// X18 left it, never runs a block the evidence does not protect.
func TestAContinuationProjectsBeforeItsFirstEffect(t *testing.T) {
	h := newHarness(t, "alpha")
	registeredAndInterrupted(t, h)
	h.workspace.evidence = killPristine(t)
	var observed []byte
	h.capability.hold = func(string) { observed = slices.Clone(h.workspace.evidence) }
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the continuation = %+v (%v)", result, err)
	}
	if running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning); !bytes.Equal(observed, running) {
		t.Fatalf("the first effect ran under evidence %q, want %q", observed, running)
	}
}

// Evidence that already reads running is not published again.
func TestAProjectionThatAlreadyHoldsIsSkipped(t *testing.T) {
	h := newHarness(t, "alpha")
	registeredAndInterrupted(t, h)
	j := journaled(h)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	entries := j.read()
	effect := slices.Index(entries, "effect alpha")
	if effect < 0 || slices.ContainsFunc(entries[:effect], func(entry string) bool { return strings.HasPrefix(entry, "publish evidence") }) {
		t.Fatalf("the continuation published evidence it already held: %q", entries)
	}
}

// A binding that no operation names and that the context held before an
// invocation began is released once that invocation's registration moved the
// index, or once a completed removal published pristine evidence. A binding
// the invocation owns, and one issued after it read the bindings, are kept.
func TestTheNextRegistrationReleasesBindingsNoOperationNames(t *testing.T) {
	ctx := context.Background()
	t.Run("an apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		stranded := strand(t, h)
		j := journaled(h)
		start, late := h.workspace.mutations, ""
		h.workspace.beforeMutation = func() {
			if h.workspace.mutations != start+1 {
				return
			}
			h.workspace.beforeMutation = nil
			late = strand(t, h)
		}
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		record, _ := durableOperation(t, h)
		if late == "" || !slices.Equal(h.binder.released, []string{stranded}) {
			t.Fatalf("released %v; the apply owns %v and %s was issued after it read the bindings", h.binder.released, record.Bindings, late)
		}
		requireOrder(t, j.read(), "replace index.json", "release "+stranded)
	})
	t.Run("an apply interrupted once it registered", func(t *testing.T) {
		h := newHarness(t, "alpha")
		stranded := strand(t, h)
		registeredAndInterrupted(t, h)
		if !slices.Equal(h.binder.released, []string{stranded}) {
			t.Fatalf("released %v", h.binder.released)
		}
	})
	t.Run("a removal of a completed apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		stranded := strand(t, h)
		j := journaled(h)
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(h.binder.released, []string{"bind-1", stranded}) {
			t.Fatalf("released %v", h.binder.released)
		}
		requireOrder(t, j.read(), "release bind-1", "publish evidence none/none", "release "+stranded)
	})
	t.Run("a finalization of a completed removal", func(t *testing.T) {
		h := newHarness(t, "alpha")
		for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
			if err := killInvoke(ctx, h.service, verb); err != nil {
				t.Fatal(err)
			}
		}
		// An apply over the removal raised its evidence and bound, and was
		// killed before it registered.
		h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
		stranded := strand(t, h)
		j := journaled(h)
		h.service.options.Confirmer = nil
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName}); err != nil || !result.Settled {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		if !slices.Contains(h.binder.released, stranded) {
			t.Fatalf("released %v", h.binder.released)
		}
		requireOrder(t, j.read(), "publish evidence none/none", "release "+stranded)
	})
}

// A destroy of a context holding no operation, over what an interrupted
// registration left, releases its reservations, publishes pristine evidence
// and only then releases every binding the context held, without presenting a
// plan or asking for confirmation, and settles. Its pristine evidence reclaims
// the claim that registration left, which holds nothing.
func TestADestroyReleasesWhatAnInterruptedRegistrationLeft(t *testing.T) {
	pristine := killPristine(t)
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	for name, test := range map[string]struct {
		evidence           []byte
		reserved, stranded bool
	}{
		"running evidence alone":                                     {evidence: running},
		"running evidence with a reservation and a stranded binding": {evidence: running, reserved: true, stranded: true},
		"pristine evidence with a reservation":                       {evidence: pristine, reserved: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.workspace.evidence = slices.Clone(test.evidence)
			if test.reserved {
				h.workspace.reservations = reservationOf("alpha")
			}
			if err := operationstore.New(h.workspace.area, killClock).Claim(context.Background(), "op-"+strings.Repeat("0e", 16)); err != nil {
				t.Fatal(err)
			}
			stranded := ""
			if test.stranded {
				stranded = strand(t, h)
			}
			j := journaled(h)
			h.service.options.Confirmer = nil
			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName})
			if err != nil || !result.Settled || result.Recovered != RecoveredRelease || result.Receipt.Operation != "none" || result.Receipt.State != "done" {
				t.Fatalf("the destroy = %+v (%v)", result, err)
			}
			if len(h.presenter.presented) != 0 || len(h.workspace.reservations) != 0 || !bytes.Equal(h.workspace.evidence, pristine) {
				t.Fatalf("the destroy presented %d plans and left %v under evidence %q",
					len(h.presenter.presented), h.workspace.reservations, h.workspace.evidence)
			}
			if current, directories := operations(t, h.workspace); current != "" || len(directories) != 0 {
				t.Fatalf("the destroy registered %q %v", current, directories)
			}
			if test.stranded {
				if !slices.Equal(h.binder.released, []string{stranded}) {
					t.Fatalf("released %v", h.binder.released)
				}
				requireOrder(t, j.read(), "release reservations", "publish evidence none/none", "release "+stranded)
			}
		})
	}
}

// The release re-proves, under the exclusive lock, the evidence it was decided
// from: evidence a fresh apply raised in between belongs to that apply, so the
// destroy refuses and releases nothing.
func TestADestroyReleaseRefusesWhenTheEvidenceMovedBeforeIt(t *testing.T) {
	h := newHarness(t, "alpha")
	h.workspace.reservations = reservationOf("alpha")
	stranded := strand(t, h)
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		h.workspace.evidence = slices.Clone(running)
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "destroy", "it was planned from no operation, and the context now holds no operation with different mutation evidence")
	if len(h.workspace.reservations) == 0 || !bytes.Equal(h.workspace.evidence, running) || slices.Contains(h.binder.released, stranded) {
		t.Fatalf("the refused destroy left %v under evidence %q, released %v", h.workspace.reservations, h.workspace.evidence, h.binder.released)
	}
}

// The release also re-proves that the context still holds no operation: an
// apply registered in between and interrupted before any block started leaves
// the same running evidence and no block record, and that evidence is its
// operation's, so the destroy refuses and lowers nothing.
func TestADestroyReleaseRefusesWhenAnOperationRegisteredBeforeIt(t *testing.T) {
	h := newHarness(t, "alpha")
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	h.workspace.evidence = slices.Clone(running)
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		registeredAndInterrupted(t, h)
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	registered := currentOperation(t, h)
	requireContextChanged(t, err, "destroy", "it was planned from no operation, and the context now holds operation "+registered+" (running)")
	if record, _ := durableOperation(t, h); record.State != reconciliation.OperationRunning || !bytes.Equal(h.workspace.evidence, running) {
		t.Fatalf("the refused destroy left %s under evidence %q", record.State, h.workspace.evidence)
	}
}

// requireRefusedRegistration holds a fresh apply that refused at its
// registering transaction to having registered nothing and performed nothing:
// the index names what it named, no block ran beyond the applies before it,
// and only the operations recorded before it hold records.
func requireRefusedRegistration(t *testing.T, h *harness, current string, applies int, recorded ...string) {
	t.Helper()
	if now := currentOperation(t, h); now != current || len(h.capability.applies) != applies {
		t.Fatalf("the refused apply left %q after applies %v", now, h.capability.applies)
	}
	_, directories := operations(t, h.workspace)
	for _, directory := range directories {
		if h.workspace.area.written(directory) != slices.Contains(recorded, directory) {
			t.Fatalf("the refused apply left records in %s among %v", directory, directories)
		}
	}
}

// Two fresh applies in flight at once both fail before they register. The
// later claim makes the earlier apply refuse and leave the evidence to it, so
// the later one gives the evidence back although it found it already raised.
func TestConcurrentFreshAppliesThatBothFailLeaveNothingRaised(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	start := h.workspace.mutations
	h.workspace.beforeMutation = func() {
		if h.workspace.mutations != start+1 {
			return
		}
		h.workspace.beforeMutation = nil
		h.binder.bindErr = failure("secret.source", "binding requires current material matching every declaration", "")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an apply whose binding failed reported success")
		}
		h.binder.bindErr = nil
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, and the context now holds no operation with different mutation evidence")
	requireRefusedRegistration(t, h, "", 0)
	if !bytes.Equal(h.workspace.evidence, killPristine(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("both refusals left evidence %q, released %v", h.workspace.evidence, h.binder.released)
	}
}

var errClaimFailed = errors.New("the operation directory could not be created")

// claimFault fails the first creation of one operation directory before it
// creates anything, as a claim refused part way does, or with interrupt
// cancels the claim's context first, which the area then refuses as the real
// one does. Once it has, unlisted also fails the next listing of the operation
// directories.
type claimFault struct {
	mutex     sync.Mutex
	directory string
	interrupt context.CancelFunc
	unlisted  bool
	fired     bool
}

type claimFaultArea struct {
	operationstore.Area
	fault *claimFault
}

func (a claimFaultArea) EnsureDirectory(ctx context.Context, target string) error {
	a.fault.mutex.Lock()
	failed := !a.fault.fired && target == a.fault.directory
	a.fault.fired = a.fault.fired || failed
	a.fault.mutex.Unlock()
	switch {
	case failed && a.fault.interrupt != nil:
		a.fault.interrupt()
	case failed:
		return errClaimFailed
	}
	return a.Area.EnsureDirectory(ctx, target)
}

func (a claimFaultArea) Entries(ctx context.Context, target string) ([]operationstore.Entry, error) {
	a.fault.mutex.Lock()
	failed := target == "" && a.fault.fired && a.fault.unlisted
	a.fault.unlisted = a.fault.unlisted && !failed
	a.fault.mutex.Unlock()
	if failed {
		return nil, errors.New("the operation directories cannot be listed")
	}
	return a.Area.Entries(ctx, target)
}

// A fresh apply whose claim failed under the running evidence of an apply in
// flight may still have created its directory, and every earlier claimant then
// refuses at its re-proof, so the evidence is the failed apply's to give back
// unless its directory is provably absent. Nothing is left raised with nothing
// in flight, and the pristine evidence given back reclaims both claims, which
// hold nothing. An apply the failed claim provably left alone keeps its
// evidence and registers.
func TestAFreshApplyWhoseClaimFailedRestoresTheEvidenceItsDirectoryMayHold(t *testing.T) {
	ctx := context.Background()
	first, second := "op-"+strings.Repeat("01", 16), "op-"+strings.Repeat("02", 16)
	for _, row := range []struct {
		name, directory string
		interrupted     bool
		unlisted        bool
		registers       bool
		directories     []string
	}{
		{name: "after it created its directory", directory: path.Join(second, "blocks")},
		{name: "before it created anything", directory: second, registers: true, directories: []string{first}},
		{name: "interrupted after it created its directory", directory: path.Join(second, "blocks"), interrupted: true},
		{name: "interrupted before it created anything", directory: second, interrupted: true, registers: true, directories: []string{first}},
		{name: "where the directories cannot be listed again", directory: second, unlisted: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			inner, cancel := context.WithCancel(ctx)
			t.Cleanup(cancel)
			fault, want := &claimFault{directory: row.directory, unlisted: row.unlisted}, errClaimFailed
			if row.interrupted {
				fault.interrupt, want = cancel, context.Canceled
			}
			base := h.service.options.Operations
			h.service.options.Operations = func(area operationstore.Area) OperationStore {
				return base(claimFaultArea{Area: area, fault: fault})
			}
			start := h.workspace.mutations
			h.workspace.beforeMutation = func() {
				if h.workspace.mutations != start+1 {
					return
				}
				h.workspace.beforeMutation = nil
				if _, err := h.service.Apply(inner, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); !errors.Is(err, want) {
					t.Fatalf("the apply whose claim failed = %v, want %v", err, want)
				}
			}
			result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			fault.mutex.Lock()
			fired, unlisted := fault.fired, fault.unlisted
			fault.mutex.Unlock()
			if !fired || unlisted {
				t.Fatalf("the claim fault fired %t and still holds a listing fault %t", fired, unlisted)
			}
			if _, directories := operations(t, h.workspace); !slices.Equal(directories, row.directories) {
				t.Fatalf("the applies left directories %v, want %v", directories, row.directories)
			}
			if row.registers {
				if err != nil || result.Receipt.State != "done" || currentOperation(t, h) != first || len(h.binder.released) != 0 {
					t.Fatalf("the apply = %+v (%v), released %v", result, err, h.binder.released)
				}
				if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, applied) {
					t.Fatalf("evidence = %q, want %q", h.workspace.evidence, applied)
				}
				return
			}
			requireContextChanged(t, err, "apply", "it was planned from no operation, and the context now holds no operation with different mutation evidence")
			requireRefusedRegistration(t, h, "", 0)
			if !bytes.Equal(h.workspace.evidence, killPristine(t)) || h.binder.issued != 1 || !slices.Equal(h.binder.released, []string{"bind-1"}) ||
				len(h.workspace.reservations) != 0 {
				t.Fatalf("both failures left evidence %q and reservations %v, bound %d and released %v",
					h.workspace.evidence, h.workspace.reservations, h.binder.issued, h.binder.released)
			}
		})
	}
}

// Collecting bindings is housekeeping, so a custody store whose bindings
// cannot be listed refuses no transition: the removal completes and releases
// what its records name, and a destroy over what an interrupted registration
// left still releases its reservations and evidence.
func TestABindingListingThatFailsBlocksNothing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.binder.bindingsErr = errors.New("the custody store cannot be listed")
	result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("the destroy = %+v (%v), released %v", result, err, h.binder.released)
	}
	h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	h.workspace.reservations = reservationOf("alpha")
	settled, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || !settled.Settled || len(h.workspace.reservations) != 0 || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("the destroy = %+v (%v) left %v under evidence %q", settled, err, h.workspace.reservations, h.workspace.evidence)
	}
}

// A destroy's release of what it took for an interrupted registration takes a
// binding an apply still in flight bound, and lowers the evidence that apply
// raised. The apply refuses at its registration, registering nothing over the
// released binding.
func TestAnApplyWhoseBindingAnUnclaimedReleaseTookRefuses(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	start := h.workspace.mutations
	h.workspace.beforeMutation = func() {
		if h.workspace.mutations != start+1 {
			return
		}
		h.workspace.beforeMutation = nil
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || !result.Settled {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, and the context now holds no operation with different mutation evidence")
	requireRefusedRegistration(t, h, "", 0)
	if !slices.Contains(h.binder.released, "bind-1") || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("released %v under evidence %q", h.binder.released, h.workspace.evidence)
	}
}

// Evidence bytes are no token: another fresh apply's protection raises the
// same bytes the release lowered. Only the directory it claims tells the two
// apart, so the apply refuses on that claim and leaves the evidence to it.
func TestAnApplyRefusesWhenAnotherClaimRaisedTheEvidenceAgain(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	start := h.workspace.mutations
	h.workspace.beforeMutation = func() {
		if h.workspace.mutations != start+1 {
			return
		}
		h.workspace.beforeMutation = nil
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || !result.Settled {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		h.workspace.evidence = slices.Clone(running)
		if err := operationstore.New(h.workspace.area, killClock).Claim(ctx, "op-"+strings.Repeat("0f", 16)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, and the context now holds no operation with an operation claimed since it was read")
	requireRefusedRegistration(t, h, "", 0)
	if !slices.Contains(h.binder.released, "bind-1") || !bytes.Equal(h.workspace.evidence, running) {
		t.Fatalf("released %v under evidence %q", h.binder.released, h.workspace.evidence)
	}
}

// A reclaim of an older claim and a newer claim leave as many directories as
// an apply in flight listed after its own claim, although a release lowered
// the evidence and took its binding in between. The apply proves the
// directories themselves, so it refuses on the newer claims rather than
// register over the released binding.
func TestAnApplyRefusesWhenReclaimsAndNewerClaimsKeepTheCount(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	store := operationstore.New(h.workspace.area, killClock)
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	// An apply killed after its claim and its raise left both.
	h.workspace.evidence = slices.Clone(running)
	if err := store.Claim(ctx, "op-"+strings.Repeat("0e", 16)); err != nil {
		t.Fatal(err)
	}
	start := h.workspace.mutations
	h.workspace.beforeMutation = func() {
		if h.workspace.mutations != start+1 {
			return
		}
		h.workspace.beforeMutation = nil
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || result.Recovered != RecoveredRelease {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
		if _, directories := operations(t, h.workspace); len(directories) != 0 {
			t.Fatalf("the release kept the claims %v", directories)
		}
		h.workspace.evidence = slices.Clone(running)
		for _, claim := range []string{"0c", "0d"} {
			if err := store.Claim(ctx, "op-"+strings.Repeat(claim, 16)); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, and the context now holds no operation with an operation claimed since it was read")
	requireRefusedRegistration(t, h, "", 0)
	if !slices.Contains(h.binder.released, "bind-1") || !bytes.Equal(h.workspace.evidence, running) {
		t.Fatalf("released %v under evidence %q", h.binder.released, h.workspace.evidence)
	}
}

// A fresh apply refused after its claim restores pristine evidence, which
// reclaims that claim because it holds nothing. Refused retries therefore keep
// nothing toward the retained-operation bound: with the context two
// operations short of it, an apply and its removal still register after them.
func TestRefusedFreshAppliesReclaimTheirClaims(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		if err := killInvoke(ctx, h.service, verb); err != nil {
			t.Fatal(err)
		}
	}
	_, recorded := operations(t, h.workspace)
	for index := range operationstore.MaxOperations - len(recorded) - 2 {
		h.workspace.area.directories[fmt.Sprintf("retained-%d", index)] = true
	}
	h.binder.bindErr = failure("secret.source", "binding requires current material matching every declaration", "")
	for range 3 {
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an apply whose binding failed reported success")
		}
		if _, directories := operations(t, h.workspace); len(directories) != operationstore.MaxOperations-2 || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
			t.Fatalf("the refused apply left %d directories under evidence %q", len(directories), h.workspace.evidence)
		}
	}
	h.binder.bindErr = nil
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		if err := killInvoke(ctx, h.service, verb); err != nil {
			t.Fatalf("the %s after the refused applies failed: %v", verb, err)
		}
	}
	if record, _ := durableOperation(t, h); record.Verb != reconciliation.Destroy || record.State != reconciliation.OperationDone {
		t.Fatalf("the removal after the refused applies is %s %s", record.Verb, record.State)
	}
}

// A fresh apply refused beside a reservation no operation owns keeps the
// running evidence that protects the context, and with it the claim it left,
// so refused retries there add a claim each. The next registration moves the
// index past every apply planned before it, so it reclaims them once it lands:
// with the context four operations short of the bound before three refused
// retries, the removal of the apply that registers after them still registers.
func TestARegistrationReclaimsWhatRefusedAppliesKeptBesideAnUnownedReservation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	h.workspace.evidence = slices.Clone(running)
	h.workspace.reservations = reservationOf("alpha")
	const refused = 3
	retained := operationstore.MaxOperations - refused - 1
	for index := range retained {
		h.workspace.area.directories[fmt.Sprintf("retained-%d", index)] = true
	}
	h.binder.bindErr = failure("secret.source", "binding requires current material matching every declaration", "")
	for attempt := 1; attempt <= refused; attempt++ {
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an apply whose binding failed reported success")
		}
		if _, directories := operations(t, h.workspace); len(directories) != retained+attempt || !bytes.Equal(h.workspace.evidence, running) {
			t.Fatalf("refused apply %d left %d directories under evidence %q", attempt, len(directories), h.workspace.evidence)
		}
	}
	h.binder.bindErr = nil
	if err := killInvoke(ctx, h.service, reconciliation.Apply); err != nil {
		t.Fatalf("the apply after the refused applies failed: %v", err)
	}
	if current, directories := operations(t, h.workspace); len(directories) != retained+1 || !slices.Contains(directories, current) {
		t.Fatalf("the registration kept %d directories beside its operation %s", len(directories)-retained-1, current)
	}
	if err := killInvoke(ctx, h.service, reconciliation.Destroy); err != nil {
		t.Fatalf("the removal after the refused applies failed: %v", err)
	}
	if record, _ := durableOperation(t, h); record.Verb != reconciliation.Destroy || record.State != reconciliation.OperationDone {
		t.Fatalf("the removal after the refused applies is %s %s", record.Verb, record.State)
	}
}

// An apply over a completed removal raises the evidence that removal's
// finalization reads as unfinished, so another verb finalizes it back to
// pristine and takes the apply's binding. The apply refuses at its
// registration rather than register over that binding.
func TestAnApplyOverACompletedDestroyRefusesWhenAFinalizationTookItsBinding(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	_, recorded := operations(t, h.workspace)
	removal, start := currentOperation(t, h), h.workspace.mutations
	h.workspace.beforeMutation = func() {
		if h.workspace.mutations != start+1 {
			return
		}
		h.workspace.beforeMutation = nil
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || !result.Settled {
			t.Fatalf("the destroy = %+v (%v)", result, err)
		}
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from operation "+removal+" (done), "+
		"and the context now holds operation "+removal+" (done) with different mutation evidence")
	requireRefusedRegistration(t, h, removal, 1, recorded...)
	if !slices.Contains(h.binder.released, "bind-2") || !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("released %v under evidence %q", h.binder.released, h.workspace.evidence)
	}
}

// A completed removal collects only once its pristine publication landed.
// Until then the evidence an apply in flight raised still stands and its
// directories have not moved, so that apply registers over a finalization that
// failed there, and the binding it registers must still be held. A fresh
// removal whose publication failed keeps a stranded binding for the next one.
func TestARemovalWhosePublicationFailedCollectsNothing(t *testing.T) {
	ctx := context.Background()
	unpublished := errors.New("the evidence could not be published")
	t.Run("a finalization under an apply in flight", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		start := h.workspace.mutations
		h.workspace.beforeMutation = func() {
			if h.workspace.mutations != start+1 {
				return
			}
			h.workspace.beforeMutation = nil
			h.workspace.failPublish = unpublished
			_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			requireIncompleteRemoval(t, diagnostics.Of(err), err)
			h.workspace.failPublish = nil
		}
		result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || result.Receipt.State != "done" {
			t.Fatalf("the apply = %+v (%v)", result, err)
		}
		if record, _ := durableOperation(t, h); len(record.Bindings) != 1 || slices.Contains(h.binder.released, record.Bindings[0]) {
			t.Fatalf("the apply registered %v, released %v", record.Bindings, h.binder.released)
		}
	})
	t.Run("a fresh removal", func(t *testing.T) {
		h := newHarness(t, "alpha")
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		stranded := strand(t, h)
		pristine := killPristine(t)
		h.service.workspace = pristineFails{testWorkspace: h.workspace, pristine: pristine, err: unpublished}
		_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		requireIncompleteRemoval(t, diagnostics.Of(err), err)
		if bytes.Equal(h.workspace.evidence, pristine) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
			t.Fatalf("the removal left evidence %q, released %v beside %s", h.workspace.evidence, h.binder.released, stranded)
		}
	})
}

// pristineFails fails every publication of pristine evidence before it lands.
type pristineFails struct {
	*testWorkspace
	pristine []byte
	err      error
}

func (w pristineFails) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	return w.testWorkspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		return callback(pristineFailing{Transaction: tx, pristine: w.pristine, err: w.err})
	})
}

type pristineFailing struct {
	Transaction
	pristine []byte
	err      error
}

func (tx pristineFailing) PublishEvidence(ctx context.Context, data []byte) error {
	if bytes.Equal(data, tx.pristine) {
		return tx.err
	}
	return tx.Transaction.PublishEvidence(ctx, data)
}

// A context holding no operation beside evidence no interrupted registration
// leaves, or beside an operation directory with block records, holds state no
// index accounts for, so its destroy refuses before it opens a transaction,
// naming each, even beside pristine evidence and no reservation.
func TestADestroyOverUnindexedRecordsRefuses(t *testing.T) {
	lost := "op-" + strings.Repeat("0a", 16)
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		state    reconciliation.OperationState
		reserved bool
		started  bool
		named    string
	}{
		"running evidence beside an operation whose index was lost": {
			verb: reconciliation.Apply, state: reconciliation.OperationRunning, reserved: true, started: true,
			named: "the operation directory " + lost + " lists block records",
		},
		"pristine evidence beside an operation whose index was lost": {
			verb: reconciliation.Destroy, state: reconciliation.OperationDone, started: true,
			named: "the operation directory " + lost + " lists block records",
		},
		"failed evidence": {
			verb: reconciliation.Apply, state: reconciliation.OperationFailed, reserved: true,
			named: "the mutation evidence reads failed and retained",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.workspace.evidence = evidenceBytes(t, test.verb, test.state)
			if test.reserved {
				h.workspace.reservations = reservationOf("alpha")
			}
			if test.started {
				h.workspace.area.files[path.Join(lost, "blocks", "alpha", "state.json")] = []byte("{}")
			}
			stranded := strand(t, h)
			records, evidence, mutations := h.workspace.area.clone(), slices.Clone(h.workspace.evidence), h.workspace.mutations
			reservations := slices.Clone(h.workspace.reservations)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
				reported[0].Message != "the context holds operation records or evidence that no index names: "+test.named ||
				reported[0].Remediation != "review its durable state with bootwright status" {
				t.Fatalf("the destroy = %+v (%v)", reported, err)
			}
			if !sameFiles(h.workspace.area, records) || !bytes.Equal(h.workspace.evidence, evidence) || h.workspace.mutations != mutations ||
				len(h.workspace.reservations) != len(reservations) || slices.Contains(h.binder.released, stranded) {
				t.Fatal("the refused destroy wrote or released something")
			}
		})
	}
}

// A destroy that settles over pristine evidence and no reservation, over no
// operation or a completed removal, releases the binding stranded there, which
// no operation names, without a transaction, and reports only that it settled.
// It releases what it listed before it read that evidence, so a binding issued
// after the listing, as a bounded run's is, stays.
func TestADestroyOverOnlyAStrandedBindingSettlesAndReleasesIt(t *testing.T) {
	ctx := context.Background()
	for name, removed := range map[string]bool{"no operation": false, "a completed removal": true} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			if removed {
				for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
					if err := killInvoke(ctx, h.service, verb); err != nil {
						t.Fatal(err)
					}
				}
			}
			stranded, mutations, released := strand(t, h), h.workspace.mutations, len(h.binder.released)
			late := ""
			h.service.binder = &listedThen{testBinder: h.binder, then: func() { late = strand(t, h) }}
			result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			if err != nil || !result.Settled || result.Recovered != "" || h.workspace.mutations != mutations {
				t.Fatalf("the destroy = %+v (%v) after %d transactions", result, err, h.workspace.mutations-mutations)
			}
			if late == "" || !slices.Equal(h.binder.released[released:], []string{stranded}) {
				t.Fatalf("released %v; %s was stranded and %s issued after the listing", h.binder.released[released:], stranded, late)
			}
		})
	}
}

// A destroy that settles releases only the bindings it listed before it
// decided. A fresh apply that claims, binds, registers and completes after
// that decision and before the destroy settles names its own binding, which
// its removal reopens, so the settling destroy leaves that binding bound.
func TestADestroyThatSettlesKeepsTheBindingOfAnApplyRegisteredAfterItDecided(t *testing.T) {
	ctx := context.Background()
	for name, removed := range map[string]bool{"no operation": false, "a completed removal": true} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			if removed {
				for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
					if err := killInvoke(ctx, h.service, verb); err != nil {
						t.Fatal(err)
					}
				}
			}
			var registered operationstore.Operation
			h.service.workspace = &readThen{testWorkspace: h.workspace, then: func() {
				if err := killInvoke(ctx, h.service, reconciliation.Apply); err != nil {
					t.Fatalf("the apply after the destroy's decision failed: %v", err)
				}
				registered, _ = durableOperation(t, h)
			}}
			released := len(h.binder.released)
			result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			if err != nil || !result.Settled || result.Recovered != "" {
				t.Fatalf("the destroy = %+v (%v)", result, err)
			}
			if registered.Verb != reconciliation.Apply || registered.State != reconciliation.OperationDone || len(registered.Bindings) != 1 {
				t.Fatalf("the apply after the destroy's decision registered %+v", registered)
			}
			for _, binding := range registered.Bindings {
				if slices.Contains(h.binder.released[released:], binding) {
					t.Fatalf("the settling destroy released %s, which the registered apply %s names", binding, registered.ID)
				}
			}
		})
	}
}

// readThen runs then once, right after the first lifecycle read's callback
// returns, which for a destroy is right after its decision.
type readThen struct {
	*testWorkspace
	then func()
}

func (w *readThen) ReadLifecycle(ctx context.Context, name string, callback func(View) error) error {
	err := w.testWorkspace.ReadLifecycle(ctx, name, callback)
	if then := w.then; then != nil {
		w.then = nil
		then()
	}
	return err
}

// listedThen runs then once, right after the first listing of the context's
// bindings returns.
type listedThen struct {
	*testBinder
	then func()
}

func (b *listedThen) Bindings(ctx context.Context, request custody.BindingsRequest) ([]string, error) {
	listed, err := b.testBinder.Bindings(ctx, request)
	if then := b.then; then != nil {
		b.then = nil
		then()
	}
	return listed, err
}
