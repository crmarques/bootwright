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
	h := newPlannedHarness(t, plan.Definitions)
	h.service.capabilities = testResolver{capability: plannedCapability{Capability: capability, plan: plan}, bindings: []CapabilityBinding{binding}}
	return &CapabilityJourney{Service: h.service, harness: h}
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

func (p plannedCapability) Unresolved(block reconciliation.Block, evidence json.RawMessage) (Unresolved, bool) {
	if reporter, ok := p.Capability.(UnresolvedReporter); ok {
		return reporter.Unresolved(block, evidence)
	}
	return Unresolved{}, false
}
