package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const headCreated = "2026-09-12T11:00:00Z"

var headID = "op-" + strings.Repeat("a5", 16)

type lineageFixture struct {
	h        *harness
	store    *operationstore.Store
	root     operationstore.Operation
	head     operationstore.Operation
	rootPlan reconciliation.Plan
	headPlan reconciliation.Plan
}

func (f lineageFixture) finish(t *testing.T, block, evidence string) {
	t.Helper()
	ctx := context.Background()
	definition, _ := f.headPlan.Block(block)
	number, err := f.store.StartAttempt(ctx, f.head.ID, block, f.h.service.ranUnder(definition))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteAttempt(ctx, f.head.ID, block, number, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(evidence)); err != nil {
		t.Fatal(err)
	}
}

func (f *lineageFixture) complete(t *testing.T) {
	t.Helper()
	f.head.State = reconciliation.OperationDone
	if err := f.store.UpdateOperation(context.Background(), f.head); err != nil {
		t.Fatal(err)
	}
}

func lineageReader(h *harness) *operationstore.Store {
	return operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) })
}

func newLineageFixture(t *testing.T, h *harness, added ...reconciliation.BlockDefinition) *lineageFixture {
	t.Helper()
	ctx := context.Background()
	completeApply(t, h)
	root, _ := durableOperation(t, h)
	store := lineageReader(h)
	rootPlan, err := store.ReadPlan(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	definitions := planDefinitions(rootPlan)
	fresh, err := reconciliation.NewPlan(reconciliation.Apply, append(slices.Clone(definitions), added...))
	if err != nil {
		t.Fatalf("planning: %v", diagnostics.Of(err))
	}
	headPlan, err := reconciliation.Additive(fresh, rootPlan)
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	digest, err := headPlan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	head := root
	head.ID, head.Predecessor, head.PlanDigest, head.State = headID, root.ID, digest, reconciliation.OperationRunning
	head.Created, head.Updated, head.Bindings = headCreated, headCreated, []string{root.Bindings[0] + "-additive"}
	if err := store.Register(ctx, head, headPlan); err != nil {
		t.Fatalf("registering the addition: %v", diagnostics.Of(err))
	}
	return &lineageFixture{h: h, store: store, root: root, head: head, rootPlan: rootPlan, headPlan: headPlan}
}

func producerAndConsumer(t *testing.T) (*harness, *lendingCapability) {
	t.Helper()
	h := newHarness(t, "producer-a")
	h.capability.definitions[0].Object = "producer-a"
	c := &lendingCapability{testCapability: h.capability, references: map[string][]ProducedInput{
		"consumer": {{Kind: "ArtifactServer", Object: "producer-a", Output: "access"}},
	}}
	h.service.capabilities = testResolver{capability: c}
	return h, c
}

func consumerDefinition() reconciliation.BlockDefinition {
	consumer := definition("consumer")
	consumer.Dependencies = []string{"producer-a"}
	return consumer
}

func requireStateRefusal(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %+v (%v), want lifecycle.state containing %q", reported, err, message)
	}
}

func TestALineageIsTheOperationAndThePredecessorsItsRecordsNameOldestFirst(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	headPlan, err := f.store.ReadPlan(ctx, f.head.ID)
	if err != nil {
		t.Fatal(err)
	}
	states, err := f.store.BlockStates(ctx, f.head.ID, headPlan)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lineageOf(ctx, f.store, lineageMember{operation: f.head, plan: headPlan, states: states})
	if err != nil {
		t.Fatalf("walking: %v", diagnostics.Of(err))
	}
	if len(held) != 2 || held[0].operation.ID != f.root.ID || held.head().operation.ID != f.head.ID {
		t.Fatalf("the lineage is %d members, first %q, head %q", len(held), held[0].operation.ID, held.head().operation.ID)
	}
	if _, found := held[0].plan.Block("producer-a"); !found || held[0].states["producer-a"] != reconciliation.BlockDone || held[1].states["consumer"] != reconciliation.BlockDone {
		t.Fatalf("the members carry plans %v and states %v / %v", held[0].plan.Blocks, held[0].states, held[1].states)
	}
	for block, want := range map[string]string{"producer-a": f.root.ID, "consumer": f.head.ID} {
		if owner, err := held.ownerFor(block); err != nil || owner != want {
			t.Fatalf("%s is owned by %q (%v), want %q", block, owner, err, want)
		}
	}
	merged, err := held.plan()
	if err != nil {
		t.Fatal(err)
	}
	want, err := reconciliation.Lineage(f.rootPlan, f.headPlan)
	if err != nil || mustDigestOf(t, merged) != mustDigestOf(t, want) {
		t.Fatalf("the merged plan is not the lineage of the plans (%v)", err)
	}
	if got := held.states(); got["producer-a"] != reconciliation.BlockDone || got["consumer"] != reconciliation.BlockDone || len(got) != 2 {
		t.Fatalf("states = %v", got)
	}
	if got := held.bindings(); !slices.Equal(got, slices.Concat(f.root.Bindings, f.head.Bindings)) {
		t.Fatalf("bindings = %v", got)
	}
}

func mustDigestOf(t *testing.T, plan reconciliation.Plan) string {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestAPredecessorNamingRecordWithoutPrerequisitesIsStillALineageMember(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, definition("loose"))
	if block, _ := f.headPlan.Block("loose"); len(block.Prerequisites) != 0 {
		t.Fatalf("the added block carries prerequisites %v", block.Prerequisites)
	}
	held, err := appliedLineage(ctx, f.store, f.head)
	if err != nil || len(held) != 2 || held[0].operation.ID != f.root.ID {
		t.Fatalf("the lineage is %d members (%v), want the root and the head", len(held), err)
	}
	if owner, err := ownerID(ctx, f.store, f.head, "producer-a"); err != nil || owner != f.root.ID {
		t.Fatalf("producer-a is routed to %q (%v), want the root", owner, err)
	}
	if unfinished, err := unfinishedOver(ctx, f.store, f.head, f.headPlan, map[string]reconciliation.BlockState{}); err != nil || !slices.Equal(unfinished, []string{"loose (pending)"}) {
		t.Fatalf("unfinished = %v (%v)", unfinished, err)
	}
}

func TestALineageOfOneReadsNothingBeyondItsHead(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	completeApply(t, h)
	root, states := durableOperation(t, h)
	store := lineageReader(h)
	plan, err := store.ReadPlan(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	held, err := appliedLineage(ctx, store, root)
	if err != nil || len(held) != 1 || held.head().operation.ID != root.ID {
		t.Fatalf("lineage = %d (%v)", len(held), err)
	}
	if owner, err := ownerID(ctx, store, root, "anything"); err != nil || owner != root.ID {
		t.Fatalf("owner = %q (%v)", owner, err)
	}
	merged, reached, err := appliedView(ctx, store, root, plan, states)
	if err != nil || mustDigestOf(t, merged) != mustDigestOf(t, plan) || len(reached) != len(states) {
		t.Fatalf("view = %v / %v (%v)", merged.Blocks, reached, err)
	}
	if kept, err := lineageBindings(ctx, store, root.ID); err != nil || kept != nil {
		t.Fatalf("kept = %v (%v)", kept, err)
	}
	if owner, err := ownerID(ctx, store, operationstore.Operation{Verb: reconciliation.Destroy}, "any"); err != nil || owner != "" {
		t.Fatalf("a removal naming no apply is routed to %q (%v)", owner, err)
	}
}

type chainReader struct {
	recordReader
	operations map[string]operationstore.Operation
	reads      int
}

func (c *chainReader) ReadOperation(_ context.Context, id string) (operationstore.Operation, error) {
	c.reads++
	operation, found := c.operations[id]
	if !found {
		return operationstore.Operation{}, failure("lifecycle.state", "the named lifecycle operation has no durable record", "")
	}
	return operation, nil
}

func (c *chainReader) ReadPlan(context.Context, string) (reconciliation.Plan, error) {
	return reconciliation.Plan{Verb: reconciliation.Apply}, nil
}

func (c *chainReader) BlockStates(context.Context, string, reconciliation.Plan) (map[string]reconciliation.BlockState, error) {
	return map[string]reconciliation.BlockState{}, nil
}

func chainID(index int) string { return fmt.Sprintf("op-%032x", index+1) }

func chainOfApplies(length int) *chainReader {
	reader := &chainReader{operations: map[string]operationstore.Operation{}}
	for index := range length {
		operation := operationstore.Operation{ID: chainID(index), Verb: reconciliation.Apply}
		if index > 0 {
			operation.Predecessor = chainID(index - 1)
		}
		reader.operations[operation.ID] = operation
	}
	return reader
}

func TestALineageWalkRefusesWhatIsNotOneBoundedChainOfApplies(t *testing.T) {
	ctx := context.Background()
	headOf := func(reader *chainReader, index int) lineageMember {
		return lineageMember{operation: reader.operations[chainID(index)]}
	}
	t.Run("a bounded chain", func(t *testing.T) {
		reader := chainOfApplies(operationstore.MaxOperations)
		held, err := lineageOf(ctx, reader, headOf(reader, operationstore.MaxOperations-1))
		if err != nil || len(held) != operationstore.MaxOperations || held[0].operation.ID != chainID(0) {
			t.Fatalf("a chain of the retained maximum read as %d members (%v)", len(held), err)
		}
	})
	t.Run("an over-long chain", func(t *testing.T) {
		reader := chainOfApplies(operationstore.MaxOperations + 1)
		_, err := lineageOf(ctx, reader, headOf(reader, operationstore.MaxOperations))
		requireStateRefusal(t, err, "exceed the operations a context retains")
		if reader.reads > operationstore.MaxOperations {
			t.Fatalf("the walk read %d records past its bound", reader.reads)
		}
	})
	t.Run("a cycle", func(t *testing.T) {
		reader := chainOfApplies(3)
		first := reader.operations[chainID(0)]
		first.Predecessor = chainID(2)
		reader.operations[first.ID] = first
		_, err := lineageOf(ctx, reader, headOf(reader, 2))
		requireStateRefusal(t, err, "name one another in a cycle")
	})
	t.Run("a head naming itself", func(t *testing.T) {
		reader := chainOfApplies(1)
		self := reader.operations[chainID(0)]
		self.Predecessor = self.ID
		_, err := lineageOf(ctx, reader, lineageMember{operation: self})
		requireStateRefusal(t, err, "name one another in a cycle")
	})
	t.Run("a missing predecessor record", func(t *testing.T) {
		reader := chainOfApplies(3)
		delete(reader.operations, chainID(1))
		_, err := lineageOf(ctx, reader, headOf(reader, 2))
		requireStateRefusal(t, err, "has no durable record")
	})
	t.Run("a removal as predecessor", func(t *testing.T) {
		reader := chainOfApplies(2)
		removal := reader.operations[chainID(0)]
		removal.Verb = reconciliation.Destroy
		reader.operations[removal.ID] = removal
		_, err := lineageOf(ctx, reader, headOf(reader, 1))
		requireStateRefusal(t, err, "is no apply")
	})
}

func TestEveryReaderOfALineageRefusesAMissingPredecessorRecord(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	orphan := f.head
	orphan.Predecessor = "op-" + strings.Repeat("9f", 16)
	destroyed := operationstore.Operation{Verb: reconciliation.Destroy, Source: f.head.ID}
	if err := f.store.UpdateOperation(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{}
	for name, read := range map[string]func() error{
		"the lineage":            func() error { _, err := appliedLineage(ctx, f.store, orphan); return err },
		"the owner":              func() error { _, err := ownerID(ctx, f.store, orphan, "consumer"); return err },
		"the owner of a removal": func() error { _, err := ownerID(ctx, f.store, destroyed, "consumer"); return err },
		"the applied view":       func() error { _, _, err := appliedView(ctx, f.store, orphan, f.headPlan, states); return err },
		"the unfinished":         func() error { _, err := unfinishedOver(ctx, f.store, orphan, f.headPlan, states); return err },
		"the kept bindings":      func() error { _, err := lineageBindings(ctx, f.store, orphan.ID); return err },
		"the dependencies":       func() error { _, err := applyDependencies(ctx, f.store, orphan.ID, "consumer"); return err },
		"the removed apply":      func() error { _, _, _, err := removedApply(ctx, f.store, orphan.ID); return err },
		"the produced lender": func() error {
			_, err := h.service.prepareProducedExecution(ctx, nil, f.store, orphan, f.headPlan, &Execution{Block: reconciliation.Block{BlockDefinition: consumerDefinition()}})
			return err
		},
		"the proved prerequisites": func() error {
			block, _ := f.headPlan.Block("consumer")
			_, err := provedDependencies(ctx, f.store, orphan, f.headPlan, block)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { requireStateRefusal(t, read(), "has no durable record") })
	}
}

func TestEachBlockRoutesToTheApplyWhoseFrozenPlanHoldsIt(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	removal := operationstore.Operation{Verb: reconciliation.Destroy, Source: f.head.ID}
	for _, row := range []struct {
		operation operationstore.Operation
		block     string
		want      string
	}{
		{f.head, "consumer", f.head.ID}, {f.head, "producer-a", f.root.ID},
		{removal, "consumer", f.head.ID}, {removal, "producer-a", f.root.ID},
		{f.root, "producer-a", f.root.ID},
	} {
		if owner, err := ownerID(ctx, f.store, row.operation, row.block); err != nil || owner != row.want {
			t.Fatalf("%s of a %s is routed to %q (%v), want %q", row.block, row.operation.Verb, owner, err, row.want)
		}
	}
	_, err := ownerID(ctx, f.store, f.head, "ghost")
	requireStateRefusal(t, err, "holds the block ghost")
	if owner, err := routedIn(ctx, f.store, f.head, "producer-a"); err != nil || owner.ID != f.root.ID || owner.Created != f.root.Created || owner.Created == headCreated {
		t.Fatalf("owner = %+v (%v)", owner, err)
	}
}

func TestAnExecutionIsHandedItsOwnersRegistrationTimeEvidenceAndProducers(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	consumer, _ := f.headPlan.Block("consumer")
	producer, _ := f.rootPlan.Block("producer-a")
	removal := operationstore.Operation{Verb: reconciliation.Destroy, Source: f.head.ID, ID: "op-" + strings.Repeat("d1", 16), Created: "2026-09-12T12:00:00Z"}
	for _, row := range []struct {
		name      string
		operation operationstore.Operation
		plan      reconciliation.Plan
		block     reconciliation.Block
		created   string
		previous  string
	}{
		{"the head's own block", f.head, f.headPlan, consumer, headCreated, `{"head":true}`},
		{"a removal of the head's block", removal, f.rootPlan, consumer, headCreated, `{"head":true}`},
		{"a removal of the predecessor's block", removal, f.rootPlan, producer, f.root.Created, `{"ok":true}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			execution := Execution{Block: row.block}
			release, err := h.service.prepareProducedExecution(ctx, nil, f.store, row.operation, row.plan, &execution)
			if err != nil {
				t.Fatalf("preparing: %v", diagnostics.Of(err))
			}
			release()
			if execution.ApplyRegistered != row.created || string(execution.PreviousEvidence) != row.previous {
				t.Fatalf("registered %q with previous evidence %s, want %q and %s", execution.ApplyRegistered, execution.PreviousEvidence, row.created, row.previous)
			}
		})
	}
	t.Run("a producer only the predecessor's plan holds", func(t *testing.T) {
		execution := Execution{Block: consumer}
		if _, _, err := h.service.producedLender(consumer, planDefinitions(f.headPlan), false, nil); err == nil {
			t.Fatal("the head's own plan resolved a producer it does not hold")
		}
		release, err := h.service.prepareProducedExecution(ctx, nil, f.store, f.head, f.headPlan, &execution)
		if err != nil {
			t.Fatalf("the lineage's definitions did not resolve the producer: %v", diagnostics.Of(err))
		}
		release()
	})
	t.Run("a custody entry per context, borrowed", func(t *testing.T) {
		var asked []string
		read := func(_ context.Context, block, output string) (secretstore.ProducedMaterial, bool, error) {
			asked = append(asked, block+"/"+output)
			return secretstore.ProducedMaterial{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kept")})}, true, nil
		}
		held, err := appliedLineage(ctx, f.store, f.head)
		if err != nil {
			t.Fatal(err)
		}
		lend, release, err := h.service.producedLender(consumer, held.lending(f.headPlan), false, read)
		if err != nil {
			t.Fatal(err)
		}
		material, err := lend(ctx, ProducedInput{Kind: "ArtifactServer", Object: "producer-a", Output: "access"})
		if err != nil || materialText(material) != "kept" || !slices.Equal(asked, []string{"producer-a/access"}) {
			t.Fatalf("lent %q after asking %v (%v)", materialText(material), asked, err)
		}
		release()
	})
}

func TestADependencyClosureReadsEachBlocksEvidenceFromItsOwner(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	proved, err := applyDependencies(ctx, f.store, f.head.ID, "consumer")
	if err != nil || len(proved) != 1 || proved[0].Block.ID != "producer-a" || string(proved[0].Evidence) != `{"ok":true}` || proved[0].State != reconciliation.BlockDone {
		t.Fatalf("the closure read %+v (%v), want the predecessor's block with its own evidence", proved, err)
	}
	consumer, _ := f.headPlan.Block("consumer")
	direct, err := provedDependencies(ctx, f.store, f.head, f.headPlan, consumer)
	if err != nil || len(direct) != 1 || direct[0].Object != "producer-a" || string(direct[0].Evidence) != `{"ok":true}` {
		t.Fatalf("an attempt of the addition is handed %+v (%v), want its prerequisite's proof", direct, err)
	}
	before, err := applyDependencies(ctx, f.store, f.root.ID, "producer-a")
	if err != nil || len(before) != 0 {
		t.Fatalf("the root's closure read %+v (%v)", before, err)
	}
}

func TestAControllerStageThePredecessorHoldsAnswersTheAdditionsLookup(t *testing.T) {
	ctx := context.Background()
	h, locator := stagedToolHarness(t)
	f := newLineageFixture(t, h, stagedDefinition("gateway", reconciliation.StageInfrastructureServices, "artifacts"))
	if _, held := f.headPlan.Block("controller-prerequisites"); held {
		t.Fatal("the addition holds the controller stage")
	}
	for name, operation := range map[string]operationstore.Operation{
		"an apply": f.head, "a removal": {Verb: reconciliation.Destroy, Source: f.head.ID},
	} {
		located, err := h.service.locateTool(ctx, f.store, prerequisites.StorageView{Prepared: true}, "lab", operation, f.headPlan, installer)
		if err != nil || located != locatedFromTheStageProof {
			t.Fatalf("%s located %q (%v), want %q", name, located, err, locatedFromTheStageProof)
		}
	}
	_ = locator
	root, err := h.service.locateTool(ctx, f.store, prerequisites.StorageView{Prepared: true}, "lab", f.root, f.rootPlan, installer)
	if err != nil || root != locatedFromTheStageProof {
		t.Fatalf("the root located %q (%v)", root, err)
	}
}

func TestSettlingSeesEveryPlanOfTheLineage(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	f.complete(t)
	headStates, err := f.store.BlockStates(ctx, f.head.ID, f.headPlan)
	if err != nil {
		t.Fatal(err)
	}
	if unfinished, err := unfinishedOver(ctx, f.store, f.head, f.headPlan, headStates); err != nil || len(unfinished) != 0 {
		t.Fatalf("unfinished = %v (%v), want none", unfinished, err)
	}
	rewriteState(t, h, path.Join(f.root.ID, "blocks", "producer-a", "state.json"), string(reconciliation.BlockRunning))
	unfinished, err := unfinishedOver(ctx, f.store, f.head, f.headPlan, headStates)
	if err != nil || !slices.Equal(unfinished, []string{"producer-a (running)"}) {
		t.Fatalf("a predecessor's unfinished block read as %v (%v)", unfinished, err)
	}
	if own := unfinishedBlocks(f.headPlan, headStates); len(own) != 0 {
		t.Fatalf("the head's own plan alone reads %v, which is what a lineage-blind settle saw", own)
	}
	var decided transition
	err = h.workspace.ReadLifecycle(ctx, testContextName, func(view View) (settleErr error) {
		decided, settleErr = h.service.settleApply(ctx, view, f.head, f.headPlan, headStates)
		return settleErr
	})
	requireStateRefusal(t, err, "producer-a (running)")
	if decided.noop {
		t.Fatal("an apply settled over a predecessor that is not done")
	}
	contradicted, err := recordContradictions(ctx, f.store, f.head, f.headPlan, headStates, map[string]int{})
	if err != nil || !slices.Equal(contradicted, []string{"producer-a (running)"}) {
		t.Fatalf("the records contradict %v (%v)", contradicted, err)
	}
}

func TestEveryBindingTheLineageHoldsIsKept(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	kept, err := lineageBindings(ctx, f.store, f.head.ID)
	if err != nil || !slices.Equal(kept, slices.Concat(f.root.Bindings, f.head.Bindings)) {
		t.Fatalf("kept = %v (%v), want the root's and the addition's", kept, err)
	}
	h.service.collect(ctx, testContextName, slices.Concat(f.root.Bindings, f.head.Bindings, []string{"stray"}), joinBindings(slices.Clone(f.head.Bindings), kept...))
	if !slices.Equal(h.binder.released, []string{"stray"}) {
		t.Fatalf("collecting released %v, want only the binding no operation holds", h.binder.released)
	}
}

func TestStatusViewsReportTheWholeLineageOfTheContextsApply(t *testing.T) {
	ctx := context.Background()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	f.complete(t)
	owned, err := h.service.Ownership(ctx, testContextName)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 2 || owned["ArtifactServer/producer-a"].State == "" || owned["ArtifactServer/consumer"].State == "" {
		t.Fatalf("ownership = %+v, want both the predecessor's and the addition's object", owned)
	}
	headStates, err := f.store.BlockStates(ctx, f.head.ID, f.headPlan)
	if err != nil {
		t.Fatal(err)
	}
	realized, err := realizations(ctx, f.store, f.head, f.headPlan, headStates)
	if err != nil || len(realized) != 2 || realized["ArtifactServer/producer-a"] != RealizationDone || realized["ArtifactServer/consumer"] != RealizationDone {
		t.Fatalf("realizations = %v (%v)", realized, err)
	}
	removal := operationstore.Operation{ID: "op-" + strings.Repeat("d1", 16), Verb: reconciliation.Destroy, Source: f.head.ID}
	applied, plan, states, err := h.service.owningApply(ctx, f.store, removal, f.rootPlan, nil)
	if err != nil || applied.ID != f.head.ID || len(plan.Blocks) != 2 || len(states) != 2 {
		t.Fatalf("diff reads %s with %d blocks and %d states (%v)", applied.ID, len(plan.Blocks), len(states), err)
	}
	applied, plan, states, err = h.service.owningApply(ctx, f.store, f.head, f.headPlan, headStates)
	if err != nil || applied.ID != f.head.ID || len(plan.Blocks) != 2 || len(states) != 2 {
		t.Fatalf("diff of the apply reads %s with %d blocks and %d states (%v)", applied.ID, len(plan.Blocks), len(states), err)
	}
	if covered := coveredBy(applied, plan, states); len(covered.Blocks) != 2 {
		t.Fatalf("a completed head covers %d blocks, want its whole lineage", len(covered.Blocks))
	}
}
