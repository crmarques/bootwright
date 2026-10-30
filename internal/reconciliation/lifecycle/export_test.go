package lifecycle

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// NewJourneyWorkspace is the journey double workspacecontract.Verify holds:
// its one ready context, with empty operation and run areas, on host, whose
// completed setup binds no context and retains no tool source.
func NewJourneyWorkspace(host controller.InstalledHostIdentity) (Workspace, string, error) {
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		return nil, "", err
	}
	return &testWorkspace{
		area: newArea(), runArea: newArea(), evidence: pristine, revision: testRevision,
		controller: prerequisites.StorageView{Exists: true, Initialized: true, Prepared: true, State: prerequisites.HostState{Host: host}},
	}, testContextName, nil
}

// JourneyContext is the one context every journey double holds.
const JourneyContext = testContextName

// CapabilityJourney is the engine over the journey double's one context,
// planning exactly plan through one production capability, so a journey can
// drive that capability through its own adapter double.
type CapabilityJourney struct {
	Service Service
	harness *harness
}

func NewCapabilityJourney(t *testing.T, capability Capability, binding CapabilityBinding, plan CapabilityPlan) *CapabilityJourney {
	t.Helper()
	return NewCapabilitiesJourney(t, capability, []CapabilityBinding{binding}, plan)
}

// NewCapabilitiesJourney is NewCapabilityJourney over one capability that
// answers for every binding named, as a router over several production
// capabilities does.
func NewCapabilitiesJourney(t *testing.T, capability Capability, bindings []CapabilityBinding, plan CapabilityPlan) *CapabilityJourney {
	t.Helper()
	bound := make([]BoundCapability, 0, len(bindings))
	for _, binding := range bindings {
		bound = append(bound, BoundCapability{Binding: binding, Capability: capability})
	}
	return NewBoundJourney(t, bound, plan)
}

type BoundCapability struct {
	Binding    CapabilityBinding
	Capability Capability
}

func NewBoundJourney(t *testing.T, bound []BoundCapability, plan CapabilityPlan) *CapabilityJourney {
	t.Helper()
	h := newPlannedHarness(t, plan.Definitions)
	h.service.capabilities = sharedResolver{bound: bound, plan: plan}
	return &CapabilityJourney{Service: h.service, harness: h}
}

// sharedResolver resolves each binding to its capability, the first binding's
// alone contributing the fixed plan, so no block is planned twice.
type sharedResolver struct {
	bound []BoundCapability
	plan  CapabilityPlan
}

func (r sharedResolver) Bindings() []CapabilityBinding {
	bindings := make([]CapabilityBinding, 0, len(r.bound))
	for _, bound := range r.bound {
		bindings = append(bindings, bound.Binding)
	}
	return bindings
}

func (r sharedResolver) Resolve(kind, implementation string) (Capability, bool) {
	position := slices.IndexFunc(r.bound, func(bound BoundCapability) bool {
		return bound.Binding == CapabilityBinding{Kind: kind, Implementation: implementation}
	})
	switch {
	case position < 0:
		return nil, false
	case position == 0:
		return plannedCapability{Capability: r.bound[0].Capability, plan: r.plan}, true
	}
	return plannedCapability{Capability: r.bound[position].Capability, plan: CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}}, true
}

// Replan makes the next invocation another build that plans these definitions.
func (j *CapabilityJourney) Replan(plan CapabilityPlan) {
	resolver := j.Service.capabilities.(sharedResolver)
	resolver.plan = plan
	j.Service.capabilities = resolver
	j.Service.automation = testAutomation{digest: "9999999999999999999999999999999999999999999999999999999999999999"}
}

// Holding gives the journey's bound Secrets one more declaration's material.
func (j *CapabilityJourney) Holding(name string, material secrets.Material) {
	if j.harness.binder.material == nil {
		j.harness.binder.material = map[string]secrets.Material{}
	}
	j.harness.binder.material[name] = material
}

// Reservations are the host keys the context holds now.
func (j *CapabilityJourney) Reservations() []prerequisites.HostReservation {
	return slices.Clone(j.harness.workspace.reservations)
}

// plannedCapability plans a fixed contribution and otherwise is the capability
// it wraps, its explanation of an unresolved observation included.
type plannedCapability struct {
	Capability
	plan CapabilityPlan
}

func (p plannedCapability) Plan(context.Context, PlanInput) (CapabilityPlan, error) {
	return p.plan, nil
}

func (p plannedCapability) Keeps(block reconciliation.Block) (KeptEntry, bool, error) {
	if keeper, ok := p.Capability.(Keeper); ok {
		return keeper.Keeps(block)
	}
	return KeptEntry{}, false, nil
}

func (p plannedCapability) Keep(ctx context.Context, execution Execution) ([]Produced, error) {
	if keeper, ok := p.Capability.(Keeper); ok {
		return keeper.Keep(ctx, execution)
	}
	return nil, nil
}

func (p plannedCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (Unresolved, bool) {
	if reporter, ok := p.Capability.(UnresolvedReporter); ok {
		return reporter.Unresolved(verb, block, evidence)
	}
	return Unresolved{}, false
}

func (p plannedCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	if bound, ok := p.Capability.(HostBound); ok {
		return bound.HostMachines(block)
	}
	return nil, nil
}

func (p plannedCapability) RemovesHost(block reconciliation.Block) bool {
	removal, ok := p.Capability.(HostRemoval)
	return ok && removal.RemovesHost(block)
}

func (p plannedCapability) RequiresOnlineRemoval(block reconciliation.Block) bool {
	online, ok := p.Capability.(OnlineRemoval)
	return ok && online.RequiresOnlineRemoval(block)
}

func (p plannedCapability) ProbesQuiescence() bool {
	prober, ok := p.Capability.(QuiescenceProber)
	return ok && prober.ProbesQuiescence()
}

func (p plannedCapability) ProjectedPaths() []ProjectedPath {
	if projector, ok := p.Capability.(Projector); ok {
		return projector.ProjectedPaths()
	}
	return nil
}

func (p plannedCapability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]Projection, error) {
	if projector, ok := p.Capability.(Projector); ok {
		return projector.Project(applied, block, evidence)
	}
	return nil, nil
}

// FailPublication makes every later publication into custody fail with err,
// or succeed again when err is nil.
func (j *CapabilityJourney) FailPublication(err error) { j.harness.binder.produceErr = err }

// Kept is what custody holds now, one block/name=value entry per produced
// output.
func (j *CapabilityJourney) Kept() []string { return j.harness.binder.producedEntries() }

// CustodyJournal is every publication and withdrawal custody committed, in
// order.
func (j *CapabilityJourney) CustodyJournal() []string { return slices.Clone(j.harness.binder.journal) }

// Rebuild makes the journey's engine another build: one whose executable
// embeds automation and stamps executable, over the same context.
func (j *CapabilityJourney) Rebuild(automation string, executable Executable) {
	j.harness.service.automation = testAutomation{digest: automation}
	j.harness.service.options.Executable = executable
	j.Service.automation = j.harness.service.automation
	j.Service.options.Executable = executable
}

// Operation is the context's current operation record as the store holds it.
func (j *CapabilityJourney) Operation(t *testing.T) operationstore.Operation {
	t.Helper()
	operation, _ := durableOperation(t, j.harness)
	return operation
}

// Records are one block's attempts of the current operation in order, each
// followed by its last resolution when it has one.
func (j *CapabilityJourney) Records(t *testing.T, block string) []operationstore.Attempt {
	t.Helper()
	ctx := context.Background()
	operation, store := j.Operation(t), recordsOf(j.harness)
	record, err := store.Block(ctx, operation.ID, block)
	if err != nil {
		t.Fatal(err)
	}
	records := []operationstore.Attempt{}
	for number := 1; number <= record.Attempts; number++ {
		attempt, err := store.Attempt(ctx, operation.ID, block, number)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, attempt)
		resolution, found, err := store.LastResolution(ctx, operation.ID, block, number)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			records = append(records, resolution)
		}
	}
	return records
}

// Unstamp rewrites every attempt and resolution record of the current
// operation without the build it names, as the canonical bytes a build from
// before records named one wrote, and reports how many it rewrote.
func (j *CapabilityJourney) Unstamp(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	area, blocks := j.harness.workspace.area, path.Join(j.Operation(t).ID, "blocks")
	directories, err := area.Entries(ctx, blocks)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := 0
	for _, directory := range directories {
		entries, err := area.Entries(ctx, path.Join(blocks, directory.Name))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name, "attempt-") || !strings.HasSuffix(entry.Name, ".json") {
				continue
			}
			name := path.Join(blocks, directory.Name, entry.Name)
			stamped, _, err := area.Read(ctx, name, operationstore.MaxAttemptBytes)
			if err != nil {
				t.Fatal(err)
			}
			var record operationstore.Attempt
			if err := json.Unmarshal(stamped, &record); err != nil {
				t.Fatal(err)
			}
			record.Build = nil
			unstamped, err := canonicaljson.Encode(record, canonicaljson.Line)
			if err != nil {
				t.Fatal(err)
			}
			if err := area.Replace(ctx, name, unstamped, stamped); err != nil {
				t.Fatal(err)
			}
			rewritten++
		}
	}
	return rewritten
}

func (j *CapabilityJourney) Declaring(objects ...api.Object) {
	j.Service.compiler = testCompiler{state: diffState(objects...)}
}

func (p plannedCapability) ProducedInputs(block reconciliation.Block) ([]ProducedInput, error) {
	if consumer, ok := p.Capability.(ProducedInputConsumer); ok {
		return consumer.ProducedInputs(block)
	}
	return nil, nil
}
func (p plannedCapability) ProducedOutputs(block reconciliation.Block) ([]string, error) {
	if producer, ok := p.Capability.(ProducedOutputProvider); ok {
		return producer.ProducedOutputs(block)
	}
	return nil, nil
}
func (p plannedCapability) StageTail(block reconciliation.BlockDefinition) (reconciliation.ObjectRef, bool, error) {
	if tail, ok := p.Capability.(StageTailConsumer); ok {
		return tail.StageTail(block)
	}
	return reconciliation.ObjectRef{}, false, nil
}

func (p plannedCapability) RetiresOffline() bool { return retiresOffline(p.Capability) }
