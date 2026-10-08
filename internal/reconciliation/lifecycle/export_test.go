package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// NewJourneyWorkspace is the journey double workspacecontract.Verify holds:
// its one ready context, with empty operation and run areas, on host, whose
// completed setup names its approved bundle, binds no context and retains no
// dependency.
func NewJourneyWorkspace(host controller.InstalledHostIdentity) (Workspace, string, error) {
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		return nil, "", err
	}
	return &testWorkspace{
		area: newArea(), runArea: newArea(), evidence: pristine, revision: testRevision,
		controller: prerequisites.StorageView{Exists: true, Initialized: true, State: prerequisites.HostState{
			Host: host, Receipt: prerequisites.SetupReceipt{Status: "complete", CatalogDigest: strings.Repeat("b", 64)},
		}},
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
	h := newPlannedHarness(t, plan.Definitions)
	h.service.capabilities = sharedResolver{capability: capability, plan: plan, bindings: bindings}
	return &CapabilityJourney{Service: h.service, harness: h}
}

// sharedResolver resolves every binding to one capability, which contributes
// the fixed plan through its first binding alone, so no block is planned
// twice.
type sharedResolver struct {
	capability Capability
	plan       CapabilityPlan
	bindings   []CapabilityBinding
}

func (r sharedResolver) Bindings() []CapabilityBinding { return r.bindings }

func (r sharedResolver) Resolve(kind, implementation string) (Capability, bool) {
	position := slices.Index(r.bindings, CapabilityBinding{Kind: kind, Implementation: implementation})
	switch {
	case position < 0:
		return nil, false
	case position == 0:
		return plannedCapability{Capability: r.capability, plan: r.plan}, true
	}
	return plannedCapability{Capability: r.capability, plan: CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}}, true
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

// FailPublication makes every later publication into custody fail with err,
// or succeed again when err is nil.
func (j *CapabilityJourney) FailPublication(err error) { j.harness.binder.produceErr = err }

// Kept is what custody holds now, one block/name=value entry per produced
// output.
func (j *CapabilityJourney) Kept() []string { return j.harness.binder.producedEntries() }

// CustodyJournal is every publication and withdrawal custody committed, in
// order.
func (j *CapabilityJourney) CustodyJournal() []string { return slices.Clone(j.harness.binder.journal) }
