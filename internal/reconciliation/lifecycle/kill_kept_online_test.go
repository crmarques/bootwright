package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const killMachineImplementation = "kill-machine-v1"

// killResolverOf binds the kill host alone, or, for a journey that keeps a
// Machine running under a staged destroy, the host beside that Machine.
func killResolverOf(host *killHost) CapabilityResolver {
	if host.keptOnline == nil {
		return testResolver{capability: host}
	}
	return keptResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}:                                      killOnline{host},
		{Kind: "Machine", Implementation: killMachineImplementation, Family: reconciliation.ManagedMachineFamily}: killMachine{host},
	}
}

// killOnline is the kill host whose removal runs on the Machines keptOnline
// names while they stay running.
type killOnline struct{ *killHost }

func (killOnline) KeptHostsRefusal(reconciliation.Block, RemovalRecords, bool) string { return "" }

func (h killOnline) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (Removal, error) {
	return h.Removal(ctx, block)
}

func (h killOnline) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	return slices.Clone(h.keptOnline[block.ID]), nil
}

// killMachine is the kept Machine a kill journey's staged destroy proves
// running: absent until its apply realizes it, then on.
type killMachine struct{ *killHost }

func (m killMachine) Plan(context.Context, PlanInput) (CapabilityPlan, error) {
	definitions := slices.DeleteFunc(slices.Clone(m.definitions), func(d reconciliation.BlockDefinition) bool { return d.Kind != "Machine" })
	return CapabilityPlan{Definitions: definitions, Secrets: []string{"artifact-server-tls"}}, nil
}

func (killMachine) StartPowerSecrets(reconciliation.Block) ([]string, error) { return nil, nil }

func (m killMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.realized[probe.Block.ID] {
		return Quiescence{State: Live, Reason: "powered on"}, nil
	}
	return Quiescence{State: Quiescent, Reason: "absent"}, nil
}

// The kept-online journey's staged destroy freezes its Machine as a power
// target that must be on, proves it running and keeps it.
func TestTheKeptOnlineJourneyKeepsItsMachineRunning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	index := slices.IndexFunc(killJourneys(), func(journey killJourney) bool { return journey.name == "staged-kept-online" })
	journey := killJourneys()[index]
	rig := journey.begin(ctx, t)
	if err := killInvokeAt(ctx, rig.harness.service, reconciliation.Destroy, reconciliation.StageClusters); err != nil {
		t.Fatalf("%+v", diagnostics.Of(err))
	}
	removal, plan := currentRecord(t, rig.harness)
	want := []operationstore.KeptPower{{Block: "node", Machine: "node", Power: operationstore.PoweredOn}}
	if !trimmed(removal) || !slices.Equal(removal.Stage.KeptPower, want) || !slices.Equal(keptHostsOf(plan, "b"), []string{"node"}) || !rig.host.realized["node"] {
		t.Fatalf("the kept-online journey recorded %+v over %v", removal.Stage, planIDs(plan))
	}
}
