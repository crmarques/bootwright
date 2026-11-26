package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type physicalTestCapability struct {
	*testCapability
	unreadable bool
	reread     []reconciliation.BlockState
}

func (c *physicalTestCapability) ReleasesMachine(block reconciliation.Block) bool {
	return block.ID == "machine-node"
}

func (c *physicalTestCapability) RereadRefusal(block reconciliation.Block, records RemovalRecords) string {
	if block.ID != "discovery-node" {
		return ""
	}
	c.reread = append(c.reread, records.State)
	if c.unreadable {
		return "its apply recorded no completed inventory"
	}
	return ""
}

func (c *physicalTestCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	plan, err := c.testCapability.Plan(ctx, input)
	if err != nil {
		return plan, err
	}
	if input.Preparation != nil {
		plan.Definitions = slices.DeleteFunc(slices.Clone(plan.Definitions), func(d reconciliation.BlockDefinition) bool {
			return !slices.Contains([]string{"setup", "services", "machine-node", "discovery-node"}, d.ID)
		})
		return plan, nil
	}
	inventoried, err := c.inventoried(ctx, input)
	if err != nil || !inventoried {
		return plan, errors.Join(err, errors.New("fixture installation needs inventory"))
	}
	return plan, nil
}

func (c *physicalTestCapability) inventoried(ctx context.Context, input PlanInput) (bool, error) {
	records, err := input.AppliedEvidence(ctx, "Machine", "node")
	return slices.ContainsFunc(records, func(r BlockEvidence) bool {
		return r.Block.ID == "discovery-node" && r.State == reconciliation.BlockDone
	}), err
}

func physicalDefinition(id string, stage reconciliation.Stage, dependencies ...string) reconciliation.BlockDefinition {
	block := stagedDefinition(id, stage, dependencies...)
	block.Kind, block.Object = "Machine", "node"
	return block
}

// physicalHarness holds a physical Machine as the Managed OS arm plans it,
// beside an infrastructure service: at the machines stage the Machine's own
// claim step, its disk discovery, which an automatic preparation runs, and the
// installation that reads its inventory.
func physicalHarness(t *testing.T, extra ...reconciliation.BlockDefinition) (*harness, *physicalTestCapability) {
	t.Helper()
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("setup", reconciliation.StageController),
		stagedDefinition("services", reconciliation.StageInfrastructureServices, "setup"),
		physicalDefinition("machine-node", reconciliation.StageMachines, "services"),
		physicalDefinition("discovery-node", reconciliation.StageMachines, "machine-node"),
		physicalDefinition("os-install-node", reconciliation.StageMachines, "discovery-node"),
	}, extra...)
	h := newPlannedHarness(t, definitions)
	physical := &physicalTestCapability{testCapability: h.capability}
	h.service.capabilities = physicalResolver{testResolver{capability: physical}, &releasedNodeInstall{testCapability: h.capability}}
	h.service.options.Preparations = []PreparationDefinition{{Kind: "example-inventory", Command: "machine discover", Bindings: h.service.capabilities.Bindings(),
		Bearing:  func(api.Object) (Bearing, bool) { return Bearing{}, false },
		Validate: func(context.Context, PlanInput, reconciliation.PreparationScope) error { return nil },
		Advance:  func(context.Context, PreparationAdvance) error { return nil },
		Automatic: func(ctx context.Context, input PlanInput, _ reconciliation.Selection) ([]string, error) {
			inventoried, err := physical.inventoried(ctx, input)
			if err != nil || inventoried {
				return nil, err
			}
			return []string{"node"}, nil
		}}}
	return h, physical
}

// B1864: a staged destroy whose scope holds a physical Machine's own claim
// step releases that Machine and takes back its discovery and installation as
// the whole destroy does, keeping what is below the floor, once the discovery's
// records prove the disks read. The restore runs the automatic preparation
// first, a restoring preparation that discovers the disks again under its own
// identity, and the apply after it installs from that new inventory; it
// presents the installation its own binding.
func TestAStagedDestroyReleasesAPhysicalMachineAndTheRestoreDiscoversItsDisksAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, physical := physicalHarness(t)
	completeApply(t, h)
	if !slices.Equal(h.capability.applies, []string{"setup", "services", "machine-node", "discovery-node", "os-install-node"}) {
		t.Fatalf("the apply ran %v", h.capability.applies)
	}
	stagedDestroy(t, h, reconciliation.StageMachines)
	removal, plan := currentRecord(t, h)
	if !trimmed(removal) || !slices.Equal(slices.Sorted(slices.Values(planIDs(plan))), []string{"discovery-node", "machine-node", "os-install-node"}) || len(removal.Stage.KeptPower) != 0 {
		t.Fatalf("the staged destroy recorded %+v over %v", removal, planIDs(plan))
	}
	if !slices.Equal(physical.reread, []reconciliation.BlockState{reconciliation.BlockDone}) {
		t.Fatalf("the staged destroy read the discovery's records %v times", physical.reread)
	}
	for _, block := range plan.Blocks {
		if block.Inverse != nil && len(block.Inverse.KeptHosts) != 0 {
			t.Fatalf("the released Machine is a kept host of %s", block.ID)
		}
	}
	h.capability.applies = nil
	restoreWith(t, h)
	if !slices.Equal(h.capability.applies, []string{"machine-node", "discovery-node", "os-install-node"}) {
		t.Fatalf("the restore ran %v", h.capability.applies)
	}
	installed, installedPlan := currentRecord(t, h)
	store := h.service.store(h.workspace.view())
	prepared, err := store.ReadOperation(ctx, installed.Predecessor)
	if err != nil {
		t.Fatal(err)
	}
	preparedPlan, err := store.ReadPlan(ctx, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Restores != removal.ID || prepared.Predecessor != removal.Source || preparedPlan.Preparation == nil || !slices.Equal(preparedPlan.Preparation.Targets, []string{"node"}) ||
		!slices.Equal(slices.Sorted(slices.Values(planIDs(preparedPlan))), []string{"discovery-node", "machine-node"}) {
		t.Fatalf("the restoring preparation is %+v over %v", prepared, planIDs(preparedPlan))
	}
	if installed.Restores != "" || installed.State != reconciliation.OperationDone || !slices.Equal(planIDs(installedPlan), []string{"os-install-node"}) {
		t.Fatalf("the apply after the restoring preparation is %+v over %v", installed, planIDs(installedPlan))
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: installed, plan: installedPlan})
	if err != nil {
		t.Fatal(err)
	}
	presents := chain.presenting()
	if own := ownBinding(installed); own == "" || presents.of("os-install-node") != own || presents.of("discovery-node") != ownBinding(prepared) || presents.of("services") == own {
		t.Fatalf("the restored lineage presents %+v, the installation's own binding is %q", presents, own)
	}
	h.capability.destroys = nil
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the whole destroy after the restore: %+v", diagnostics.Of(err))
	}
	if len(h.capability.destroys) != 5 {
		t.Fatalf("the whole destroy after the restore removed %v", h.capability.destroys)
	}
}

// A discovery whose records prove no disk read refuses before anything
// registers: the restore could not read the disks again.
func TestAStagedDestroyRefusesAPhysicalMachineWhoseDisksNoDiscoveryRead(t *testing.T) {
	t.Parallel()
	h, physical := physicalHarness(t)
	completeApply(t, h)
	physical.unreadable = true
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step discovery-node read the disks of Machine/node") ||
		!strings.Contains(reported[0].Message, "nothing proves the restore can read them again") || !strings.Contains(reported[0].Remediation, "bootwright destroy --context lab") {
		t.Fatalf("destroy --stage machines over an unread discovery returned %v", reported)
	}
	before.require(t, h)
}

// A floor above the machines stage keeps the physical Machine's own step, so a
// step on it still refuses, naming the floor that releases the Machine.
func TestAStagedDestroyAboveTheMachinesStageKeepsAPhysicalMachine(t *testing.T) {
	t.Parallel()
	h, _ := physicalHarness(t, physicalDefinition("service", reconciliation.StageClusters, "os-install-node"))
	completeApply(t, h)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "a physical Machine stays below the clusters stage") ||
		!strings.Contains(reported[0].Remediation, "--stage machines") {
		t.Fatalf("destroy --stage clusters over a physical Machine returned %v", reported)
	}
	before.require(t, h)
}

type physicalResolver struct {
	testResolver
	install *releasedNodeInstall
}

func (r physicalResolver) Resolve(_, implementation string) (Capability, bool) {
	if implementation == "agent-install-v1" {
		return r.install, true
	}
	return r.capability, true
}

type releasedNodeInstall struct {
	*testCapability
	booted bool
	kept   int
}

func (c *releasedNodeInstall) KeptHostsRefusal(reconciliation.Block, RemovalRecords, bool) string {
	if c.booted {
		return "its installation recorded a boot"
	}
	return ""
}

func (c *releasedNodeInstall) KeptHostsRemoval(context.Context, reconciliation.Block) (Removal, error) {
	c.kept++
	return Removal{Description: "take back the installation on kept nodes", Retains: []string{"the kept nodes"}}, nil
}

func agentInstall() reconciliation.BlockDefinition {
	install := physicalDefinition("agent-install", reconciliation.StageClusters, "machine-node")
	install.Implementation = "agent-install-v1"
	return install
}

// M1: an installation that answers lifecycle.KeptHostRemoval on a released
// Machine is planned as the whole destroy plans it: before a boot it is
// taken back with no kept host and no kept power target, and after a boot it
// still refuses, since no step blanks a physical node's disks.
func TestAKeptHostRemovalOnAReleasedMachineIsPlannedAsTheWholeDestroyPlansIt(t *testing.T) {
	t.Parallel()
	for name, booted := range map[string]bool{"before a boot": false, "after a boot": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, _ := physicalHarness(t, agentInstall())
			install := h.service.capabilities.(physicalResolver).install
			completeApply(t, h)
			install.booted = booted
			before := untouchedOf(h)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
			if booted {
				reported := diagnostics.Of(err)
				if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step agent-install runs on Machine/node") ||
					!strings.Contains(reported[0].Message, "no step of this context blanks a node's disks") {
					t.Fatalf("destroy --stage machines over a booted installation returned %v", reported)
				}
				before.require(t, h)
				return
			}
			if err != nil {
				t.Fatalf("destroy --stage machines: %+v", diagnostics.Of(err))
			}
			removal, plan := currentRecord(t, h)
			if len(removal.Stage.KeptPower) != 0 || install.kept != 0 {
				t.Fatalf("the released Machine froze kept power %+v and %d kept removals", removal.Stage.KeptPower, install.kept)
			}
			for _, block := range plan.Blocks {
				if block.Inverse != nil && len(block.Inverse.KeptHosts) != 0 {
					t.Fatalf("the released Machine is a kept host of %s", block.ID)
				}
			}
		})
	}
}

// M2: a restore that needs a preparation judges the whole input, planned over
// what the lineage held before the take-back, before any effect: a step no
// staged destroy took back and a changed kept step both refuse with nothing run.
func TestARestoreThatNeedsAPreparationRefusesBeforeAnyEffect(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		change  func(*harness)
		code    string
		message string
	}{
		"a newly declared step": {func(h *harness) {
			h.capability.definitions = append(h.capability.definitions, physicalDefinition("tuning-node", reconciliation.StageMachines, "os-install-node"))
		}, applyRestoreAddsNew.Code, "the step tuning-node, which no staged destroy"},
		"a changed kept step": {func(h *harness) {
			h.capability.definitions[1].Request = json.RawMessage(`{"name":"changed"}`)
		}, applyKeptBlockChanged.Code, "kept the step services"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, _ := physicalHarness(t)
			completeApply(t, h)
			stagedDestroy(t, h, reconciliation.StageMachines)
			test.change(h)
			before := untouchedOf(h)
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != test.code || !strings.Contains(reported[0].Message, test.message) {
				t.Fatalf("the restore returned %v", reported)
			}
			before.require(t, h)
		})
	}
}

// M3: an input that changes after the restoring preparation ran is refused by
// the apply after it, and the whole destroy then takes back what the context
// holds, the restoring preparation's steps among them.
func TestAWholeDestroyAfterARefusedAdditionTakesBackTheRestoringPreparation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _ := physicalHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	h.capability.applies = nil
	h.capability.planned = func() {
		if slices.Contains(h.capability.applies, "discovery-node") && !slices.ContainsFunc(h.capability.definitions, func(d reconciliation.BlockDefinition) bool { return d.ID == "tuning-node" }) {
			h.capability.definitions = append(h.capability.definitions, physicalDefinition("tuning-node", reconciliation.StageMachines, "os-install-node"))
		}
	}
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if firstCode(err) != applyRestoreAddsNew.Code || !slices.Equal(h.capability.applies, []string{"machine-node", "discovery-node"}) {
		t.Fatalf("the apply after the restoring preparation returned %v after %v", diagnostics.Of(err), h.capability.applies)
	}
	h.capability.planned = nil
	h.capability.destroys = nil
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the whole destroy after a refused addition: %+v", diagnostics.Of(err))
	}
	if !slices.Equal(slices.Sorted(slices.Values(h.capability.destroys)), []string{"discovery-node", "machine-node", "services", "setup"}) {
		t.Fatalf("the whole destroy removed %v", h.capability.destroys)
	}
}

// M3: an interrupted restoring preparation continues with its frozen scope, an
// interrupted apply after it continues too, and the restored lineage presents
// the installation the binding of the apply that created it.
func TestAnInterruptedRestoreContinuesThroughBothMembers(t *testing.T) {
	t.Parallel()
	for _, interrupted := range []string{"discovery-node", "os-install-node"} {
		t.Run(interrupted, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			h, _ := physicalHarness(t)
			completeApply(t, h)
			stagedDestroy(t, h, reconciliation.StageMachines)
			removal, _ := currentRecord(t, h)
			h.capability.applies = nil
			h.capability.errorFor = map[string]error{interrupted: errors.New("fixture interruption")}
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("the fixture did not interrupt the restore")
			}
			stopped, _ := currentRecord(t, h)
			if stopped.State == reconciliation.OperationDone || (interrupted == "discovery-node") != (stopped.Restores == removal.ID) {
				t.Fatalf("the interrupted restore left %+v current", stopped)
			}
			h.capability.errorFor = nil
			restoreWith(t, h)
			installed, plan := currentRecord(t, h)
			store := h.service.store(h.workspace.view())
			prepared, err := store.ReadOperation(ctx, installed.Predecessor)
			if err != nil || installed.State != reconciliation.OperationDone || prepared.Restores != removal.ID || !slices.Contains(h.capability.applies, "os-install-node") {
				t.Fatalf("the continued restore is %+v after %+v (%v), ran %v", installed, prepared, err, h.capability.applies)
			}
			chain, err := lineageOf(ctx, store, lineageMember{operation: installed, plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			if own := ownBinding(installed); own == "" || chain.presenting().of("os-install-node") != own {
				t.Fatalf("the continued lineage presents the installation %q, its creator binds %q", chain.presenting().of("os-install-node"), own)
			}
		})
	}
}

// M3: an explicit discovery over a trimmed context keeps refusing; only the
// restore runs the preparation.
func TestAnExplicitDiscoveryOverATrimmedContextRefuses(t *testing.T) {
	t.Parallel()
	h, _ := physicalHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	before := untouchedOf(h)
	_, err := h.service.Prepare(context.Background(), PrepareRequest{ContextName: testContextName, Kind: "example-inventory", Targets: []string{"node"}, SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != prepareRefused.Code || !strings.Contains(reported[0].Remediation, "bootwright apply --context lab") {
		t.Fatalf("machine discover over a trimmed context returned %v", reported)
	}
	before.require(t, h)
}

// releasedStorage is a storage cluster on the physical node whose removal runs
// on its hosts while they run and which refuses physical hosts, as Ceph does.
type releasedStorage struct{ *testCapability }

func (releasedStorage) KeptHostsRefusal(reconciliation.Block, RemovalRecords, bool) string {
	return "its hosts are physical, which no staged destroy takes a Ceph cluster back from (B1885)"
}

func (c releasedStorage) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (Removal, error) {
	return c.Removal(ctx, block)
}

func (releasedStorage) OnlineKeptHosts(reconciliation.Block) ([]string, error) {
	return []string{"node"}, nil
}

type releasedStorageResolver struct {
	physicalResolver
	storage releasedStorage
}

func (r releasedStorageResolver) Resolve(kind, implementation string) (Capability, bool) {
	if implementation == "store-v1" {
		return r.storage, true
	}
	return r.physicalResolver.Resolve(kind, implementation)
}

// B1864, B1885: a storage step on a physical Machine the staged destroy
// releases is not planned as the whole destroy plans it while its capability
// refuses physical hosts: the staged destroy refuses with that reason and the
// whole destroy as its remedy, registering nothing.
func TestAStorageStepOnAReleasedPhysicalMachineRefusesTheStagedDestroy(t *testing.T) {
	t.Parallel()
	storage := physicalDefinition("storage", reconciliation.StageClusters, "os-install-node")
	storage.Implementation = "store-v1"
	h, _ := physicalHarness(t, storage)
	h.service.capabilities = releasedStorageResolver{h.service.capabilities.(physicalResolver), releasedStorage{h.capability}}
	completeApply(t, h)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step storage runs on Machine/node") ||
		!strings.Contains(reported[0].Message, "(B1885)") || !strings.Contains(reported[0].Remediation, "take back the whole context with bootwright destroy --context lab") {
		t.Fatalf("destroy --stage machines over storage on a released physical Machine returned %v", reported)
	}
	before.require(t, h)
}
