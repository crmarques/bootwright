package lifecycle

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type keptOnlineStorage struct {
	*testCapability
	hosts    []string
	refuse   string
	contract string
	// residue names, by host Machine, the apply operation whose cluster
	// state the host holds: an apply's preflight refuses another's, and the
	// removal on kept hosts clears it, so a restore proves nothing foreign.
	residue map[string]string
}

func (c *keptOnlineStorage) HostMachines(reconciliation.Block) ([]string, error) {
	return slices.DeleteFunc(slices.Clone(c.hosts), func(machine string) bool { return machine == "provided-1" }), nil
}

func (c *keptOnlineStorage) KeptHostsRefusal(_ reconciliation.Block, records RemovalRecords, _ bool) string {
	switch records.State {
	case reconciliation.BlockFailed, reconciliation.BlockPending:
		return ""
	case reconciliation.BlockDone:
		return c.refuse
	}
	return "its outcome is " + string(records.State) + ", and resolving it may prove its dashboard credential handed out"
}

func (c *keptOnlineStorage) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.Removal(ctx, block)
	removal.Description = "tear down " + block.Object + " on its kept hosts"
	return removal, err
}

func (c *keptOnlineStorage) OnlineKeptHosts(reconciliation.Block) ([]string, error) {
	return slices.Clone(c.hosts), nil
}

func (c *keptOnlineStorage) KeptHostsContract() (string, string) {
	return "store-kept-v1", c.contract
}

func (c *keptOnlineStorage) Apply(ctx context.Context, execution Execution) (Result, error) {
	c.mutex.Lock()
	for _, host := range c.hosts {
		if owner := c.residue[host]; owner != "" && owner != execution.ApplyOperation {
			c.mutex.Unlock()
			return Result{Outcome: reconciliation.OutcomeFailed}, errors.New("Machine/" + host + " holds the cluster state of " + owner)
		}
	}
	for _, host := range c.hosts {
		c.residue[host] = execution.ApplyOperation
	}
	c.mutex.Unlock()
	return c.testCapability.Apply(ctx, execution)
}

func (c *keptOnlineStorage) Destroy(ctx context.Context, execution Execution) (Result, error) {
	if execution.Block.Inverse != nil {
		c.mutex.Lock()
		for _, host := range execution.Block.Inverse.KeptHosts {
			delete(c.residue, host)
		}
		c.mutex.Unlock()
	}
	return c.testCapability.Destroy(ctx, execution)
}

// keptOnlineMachine reads a Machine's power as unproved while unread names it.
type keptOnlineMachine struct {
	*keptNodeMachine
	unread map[string]bool
}

func (m *keptOnlineMachine) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	state, err := m.keptNodeMachine.StartPower(ctx, probe)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.unread[probe.Block.Object] {
		return Quiescence{State: Unproved, Reason: "its power controller did not answer"}, err
	}
	return state, err
}

type keptOnline struct {
	h       *harness
	machine *keptOnlineMachine
	storage *keptOnlineStorage
	install *keptNodeInstall
}

// keptOnlineOptions shapes the harness: install names the node an
// installation whose removal needs it off runs on (none when empty), and
// share adds a step that runs beside the storage cluster on its hosts and
// has no removal on kept hosts.
type keptOnlineOptions struct {
	install string
	share   bool
}

// keptOnlineHarness holds a storage cluster on the virtual node-1 and the
// provided provided-1, whose removal runs on them while they run.
func keptOnlineHarness(t *testing.T, options keptOnlineOptions) keptOnline {
	t.Helper()
	h := newHarness(t)
	host := keptBlock("host", "InfraProvider", "host-v1", reconciliation.StageSubstrates)
	nodes := []reconciliation.BlockDefinition{keptBlock("node-1", "Machine", "node-machine-v1", reconciliation.StageMachines, "host")}
	if options.install == "node-2" {
		nodes = append(nodes, keptBlock("node-2", "Machine", "node-machine-v1", reconciliation.StageMachines, "host"))
	}
	for index := range nodes {
		nodes[index].Object = nodes[index].ID
	}
	provided := keptBlock("provided-1", "Machine", "provided-v1", reconciliation.StageMachines)
	provided.Object = "provided-1"
	stored := []reconciliation.BlockDefinition{keptBlock("storage", "StorageCluster", "store-v1", reconciliation.StageClusters, "node-1", "provided-1")}
	stored[0].Object = "store"
	if options.share {
		share := keptBlock("share", "StorageCluster", "store-v1", reconciliation.StageClusters, "storage")
		share.Object = "share"
		stored = append(stored, share)
	}
	cluster := keptOnline{
		h: h,
		machine: &keptOnlineMachine{keptNodeMachine: &keptNodeMachine{testCapability: &testCapability{definitions: nodes}, on: map[string]bool{}},
			unread: map[string]bool{}},
		storage: &keptOnlineStorage{testCapability: &testCapability{definitions: stored[:1]}, hosts: []string{"node-1", "provided-1"},
			refuse: "its dashboard credential was handed out (B491)", contract: strings.Repeat("c", 64), residue: map[string]string{}},
	}
	installs := []reconciliation.BlockDefinition{}
	if options.install != "" {
		install := keptBlock("install", "OpenShiftCluster", "install-v1", reconciliation.StageClusters, options.install)
		install.Object = "edge"
		installs = append(installs, install)
	}
	cluster.install = &keptNodeInstall{testCapability: &testCapability{definitions: installs}, nodes: []string{cmp.Or(options.install, "node-1")}}
	resolver := keptResolver{
		{Kind: "InfraProvider", Implementation: "host-v1"}:                                                &testCapability{definitions: []reconciliation.BlockDefinition{host}},
		{Kind: "Machine", Implementation: "node-machine-v1", Family: reconciliation.ManagedMachineFamily}: cluster.machine,
		{Kind: "Machine", Implementation: "provided-v1"}:                                                  &testCapability{definitions: []reconciliation.BlockDefinition{provided}},
		{Kind: "StorageCluster", Implementation: "store-v1"}:                                              cluster.storage,
		{Kind: "OpenShiftCluster", Implementation: "install-v1"}:                                          cluster.install,
	}
	if options.share {
		resolver[CapabilityBinding{Kind: "StorageShare", Implementation: "share-v1"}] = &testCapability{definitions: []reconciliation.BlockDefinition{shareBlock(stored[1])}}
	}
	h.service.capabilities = resolver
	h.capability = cluster.storage.testCapability
	operations := h.service.options.Operations
	h.service.options.Operations = func(records, logs operationstore.Area) OperationStore {
		return operations(records, logs).(*operationstore.Store).KeptContracts(operationstore.KeepsHosts, "store-kept-v1")
	}
	return cluster
}

func shareBlock(block reconciliation.BlockDefinition) reconciliation.BlockDefinition {
	block.Kind, block.Implementation = "StorageShare", "share-v1"
	return block
}

func (c keptOnline) failApply(t *testing.T) {
	t.Helper()
	c.storage.outcomeFor = map[string]Result{"storage": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := c.h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a storage cluster scripted to fail completed")
	}
	c.storage.outcomeFor = nil
	c.power("node-1", true)
	c.machine.powered = nil
}

func (c keptOnline) power(node string, on bool) {
	c.machine.mutex.Lock()
	defer c.machine.mutex.Unlock()
	c.machine.on[node] = on
}

// B1862: a staged destroy takes back a storage cluster that never completed
// while its hosts stay running: its removal freezes the kept hosts, a managed
// Machine as a power target that must be on, a provided one, which has no
// power control, as reachable, and proves the managed one running.
func TestAStagedDestroyTakesBackAStorageClusterOnKeptHostsThatStayRunning(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.failApply(t)
	result := stagedDestroy(t, h, reconciliation.StageClusters)
	removal, plan := currentRecord(t, h)
	want := []operationstore.KeptPower{{Block: "node-1", Machine: "node-1", Power: operationstore.PoweredOn}, {Block: "provided-1", Machine: "provided-1", Power: operationstore.Reachable}}
	if !trimmed(removal) || !slices.Equal(planIDs(plan), []string{"storage"}) || !slices.Equal(removal.Stage.KeptPower, want) {
		t.Fatalf("the staged destroy recorded %+v over %v", removal.Stage, planIDs(plan))
	}
	if !slices.Equal(keptHostsOf(plan, "storage"), []string{"node-1", "provided-1"}) {
		t.Fatalf("the removal froze the kept hosts %v", keptHostsOf(plan, "storage"))
	}
	if result.Scope == nil || !slices.Equal(result.Scope.KeptPowerTargets, []string{"Machine/node-1 powered on", "Machine/provided-1 reachable over its own connection"}) {
		t.Fatalf("the staged destroy's scope reads %+v", result.Scope)
	}
	if got := cluster.machine.powered; !slices.Equal(got, []string{"node-1"}) {
		t.Fatalf("the staged destroy read the power of %v, want only the managed kept host", got)
	}
	if !slices.Equal(cluster.storage.destroys, []string{"storage"}) || len(cluster.machine.destroys) != 0 {
		t.Fatalf("the staged destroy removed %v and %v", cluster.storage.destroys, cluster.machine.destroys)
	}
}

// A kept host the removal runs on that is not proved running refuses every
// invocation before any effect, naming machine start and the destroy at its
// floor: the fresh one and the continuation of an unknown one.
func TestAKeptHostThatMustRunRefusesWhileItIsOff(t *testing.T) {
	t.Parallel()
	for name, arrange := range map[string]func(*testing.T, keptOnline){
		"fresh": func(*testing.T, keptOnline) {},
		"continuation": func(t *testing.T, cluster keptOnline) {
			cluster.storage.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if _, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("a staged destroy whose step is unknown completed")
			}
			cluster.storage.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptOnlineHarness(t, keptOnlineOptions{})
			h := cluster.h
			cluster.failApply(t)
			arrange(t, cluster)
			cluster.storage.outcomes = nil
			cluster.power("node-1", false)
			before := untouchedOf(h)
			destroys := len(cluster.storage.destroys)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyKeptHostStopped.Code || !strings.Contains(reported[0].Message, "Machine/node-1") ||
				!strings.Contains(reported[0].Remediation, "bootwright machine start --context lab --name node-1") ||
				!strings.Contains(reported[0].Remediation, "bootwright destroy --context lab --stage clusters") {
				t.Fatalf("destroy --stage clusters over a stopped kept host returned %v", reported)
			}
			before.require(t, h)
			if len(cluster.storage.destroys) != destroys {
				t.Fatalf("the refused staged destroy removed %v", cluster.storage.destroys)
			}
			cluster.power("node-1", true)
			stagedDestroy(t, h, reconciliation.StageClusters)
			if current, _ := currentRecord(t, h); !trimmed(current) {
				t.Fatalf("the staged destroy over the running host left %+v", current)
			}
		})
	}
}

// A storage cluster whose capability refuses, as one that handed out access,
// refuses the staged destroy before registration with its reason; a provided
// host, which no floor deletes, leaves only the whole destroy.
func TestAStorageClusterThatHandedOutAccessRefusesItsKeptHostRemoval(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	completeApply(t, h)
	cluster.power("node-1", true)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "its dashboard credential was handed out (B491)") ||
		reported[0].Remediation != "take back the whole context with bootwright destroy --context lab" {
		t.Fatalf("destroy --stage clusters over a used storage cluster returned %v", reported)
	}
	before.require(t, h)
}

// A kept Machine one taken-back step needs running and another needs off
// refuses before registration, naming both steps.
func TestAKeptHostNeededBothRunningAndOffRefuses(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{install: "node-1"})
	h := cluster.h
	cluster.storage.hosts = []string{"node-1"}
	cluster.failApply(t)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "Machine/node-1 is powered off") ||
		!strings.Contains(reported[0].Message, "the step storage only while it is powered on") {
		t.Fatalf("destroy --stage clusters over a host needed both ways returned %v", reported)
	}
	before.require(t, h)
}

// H-2: a staged destroy that takes a step back on kept hosts records the
// contract of that effect beside its plan's, and a continuation under an
// executable whose kept-host contract moved refuses before any attempt.
func TestAStagedDestroyRecordsItsKeptHostContractAndRefusesItMoved(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.failApply(t)
	cluster.storage.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose step is unknown completed")
	}
	removal, _ := currentRecord(t, h)
	if removal.Contracts["store-kept-v1"] != strings.Repeat("4", 64) || removal.Contracts["store-v1"] == "" {
		t.Fatalf("the staged destroy recorded the contracts %v", removal.Contracts)
	}
	current := h.service.options.Contracts
	h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		if block.Implementation == "store-kept-v1" {
			contract.Fingerprint = strings.Repeat("6", 64)
		}
		return contract, ok
	}
	cluster.storage.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != engineOtherBuild.Code || !strings.Contains(reported[0].Message, "kept-host contract of block storage") {
		t.Fatalf("a continuation under a moved kept-host contract returned %v", reported)
	}
	before.require(t, h)
}

// H-1: a storage cluster whose apply outcome is unknown is not taken back
// while its hosts stay, since resolving it may prove it handed out access;
// the refusal names bootwright apply, which resolves it, and once resolved
// completed the step refuses as one that handed out access.
func TestAnUnresolvedStorageClusterRefusesItsKeptHostRemovalNamingApply(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.storage.outcomeFor = map[string]Result{"storage": {Outcome: reconciliation.OutcomeUnknown}}
	cluster.storage.observeErr = errors.New("the cluster did not answer")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a storage cluster scripted unknown completed")
	}
	cluster.storage.outcomeFor, cluster.storage.observeErr = nil, nil
	cluster.power("node-1", true)
	cluster.storage.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "its outcome is unknown") ||
		!strings.Contains(reported[0].Remediation, "resolve the step with bootwright apply --context lab, then repeat bootwright destroy --context lab --stage clusters") {
		t.Fatalf("destroy --stage clusters over an unknown storage cluster returned %v", reported)
	}
	before.require(t, h)
	completeApply(t, h)
	_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "B491") {
		t.Fatalf("destroy --stage clusters over the resolved storage cluster returned %v", reported)
	}
}

// L-2: a step that runs beside a storage cluster on the hosts the cluster is
// taken back from while they run, and has no removal of its own there,
// refuses naming B1885 and the whole destroy.
func TestAStepBesideAStorageClusterOnRunningKeptHostsNamesB1885(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{share: true})
	h := cluster.h
	cluster.failApply(t)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step share runs on") ||
		!strings.Contains(reported[0].Message, "the step storage is taken back from these Machines while they run") || !strings.Contains(reported[0].Message, "(B1885)") ||
		strings.Contains(reported[0].Message, "B1861") || reported[0].Remediation != "take back the whole context with bootwright destroy --context lab" {
		t.Fatalf("destroy --stage clusters over a step beside the storage cluster returned %v", reported)
	}
	before.require(t, h)
}

// L-3: a kept Machine that must run and whose power cannot be read is
// reported unreadable, never off, in the one refusal that also names a kept
// Machine that must stay off and was found running.
func TestAKeptHostThatMustRunAndCannotBeReadIsReportedUnreadable(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{install: "node-2"})
	h := cluster.h
	cluster.failApply(t)
	cluster.machine.mutex.Lock()
	cluster.machine.unread["node-1"] = true
	cluster.machine.mutex.Unlock()
	cluster.power("node-2", true)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyKeptHostStopped.Code || len(reported) == 0 ||
		!strings.Contains(reported[0].Message, "could not be read: Machine/node-1 (its power controller did not answer)") ||
		strings.Contains(reported[0].Message, "powered off: Machine/node-1") ||
		!strings.Contains(reported[0].Message, "must stay powered off were not proved off: Machine/node-2 (powered on)") ||
		!strings.Contains(reported[0].Remediation, "bootwright machine stop node-2 --context lab") {
		t.Fatalf("destroy --stage clusters over an unreadable and a running kept host returned %v", reported)
	}
	before.require(t, h)
}

// L-5: a never-used storage cluster taken back from its kept hosts is
// restored by the next apply under a new owner, from which its FSID derives,
// and that apply's preflight finds no cluster state of the old owner on any
// kept host.
func TestATakenBackStorageClusterIsRestoredUnderANewOwnerOnCleanHosts(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.failApply(t)
	original := cluster.storage.executions[len(cluster.storage.executions)-1].ApplyOperation
	stagedDestroy(t, h, reconciliation.StageClusters)
	if len(cluster.storage.residue) != 0 {
		t.Fatalf("the teardown left the cluster state %v on the kept hosts", cluster.storage.residue)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the restore returned %+v", diagnostics.Of(err))
	}
	restored := cluster.storage.executions[len(cluster.storage.executions)-1]
	if restored.Block.ID != "storage" || restored.ApplyOperation == original || restored.ApplyOperation == "" {
		t.Fatalf("the restore applied %s under %q, the original was %q", restored.Block.ID, restored.ApplyOperation, original)
	}
	want := map[string]string{"node-1": restored.ApplyOperation, "provided-1": restored.ApplyOperation}
	if !maps.Equal(cluster.storage.residue, want) {
		t.Fatalf("the restored cluster holds %v, want %v", cluster.storage.residue, want)
	}
}

// H-2: the attempt proves the recorded kept-host contract again, so an
// executable whose contract moved after registration removes nothing.
func TestAnAttemptOnKeptHostsProvesItsKeptHostContract(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.failApply(t)
	current, reads := h.service.options.Contracts, 0
	h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		if block.Implementation == "store-kept-v1" {
			if reads++; reads > 1 {
				contract.Fingerprint = strings.Repeat("6", 64)
			}
		}
		return contract, ok
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != engineOtherBuild.Code || !strings.Contains(reported[0].Message, "kept-host contract of block storage") {
		t.Fatalf("an attempt under a moved kept-host contract returned %v", reported)
	}
	if reads < 2 || len(cluster.storage.destroys) != 0 {
		t.Fatalf("the attempt read the kept-host contract %d times and removed %v", reads, cluster.storage.destroys)
	}
}

// B1861, B1862: one staged destroy blanks the root disk of a kept node an
// installation booted, kept off, and tears a never-used storage cluster down
// on other kept hosts that stay running, recording both kept-host contracts;
// a node that both would need refuses before any effect.
func TestAStagedDestroyBlanksOneKeptNodeAndTearsDownStorageOnHostsThatRun(t *testing.T) {
	t.Parallel()
	blanking := func(t *testing.T, node string) (keptOnline, *blankingNodeMachine) {
		t.Helper()
		cluster := keptOnlineHarness(t, keptOnlineOptions{install: node})
		cluster.install.boots = true
		machine := &blankingNodeMachine{keptNodeMachine: cluster.machine.keptNodeMachine, marks: map[string]string{}, lose: map[string]string{},
			installed: func() int { return len(cluster.install.destroysOf()) }, mediaGone: func() int { return 0 }}
		cluster.h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: "Machine", Implementation: "node-machine-v1", Family: reconciliation.ManagedMachineFamily}] = machine
		cluster.failApply(t)
		return cluster, machine
	}
	t.Run("on other hosts", func(t *testing.T) {
		t.Parallel()
		cluster, machine := blanking(t, "node-2")
		h := cluster.h
		if _, err := blankingDestroy(h); err != nil {
			t.Fatalf("the staged destroy: %+v", diagnostics.Of(err))
		}
		removal, plan := currentRecord(t, h)
		want := []operationstore.KeptPower{{Block: "node-1", Machine: "node-1", Power: operationstore.PoweredOn},
			{Block: "node-2", Machine: "node-2", Power: operationstore.PoweredOff}, {Block: "provided-1", Machine: "provided-1", Power: operationstore.Reachable}}
		if !trimmed(removal) || !slices.Equal(removal.Stage.KeptPower, want) {
			t.Fatalf("the staged destroy recorded %+v over %v", removal.Stage, planIDs(plan))
		}
		if !slices.Equal(blanksOf(plan, "install"), []string{"node-2"}) || !slices.Equal(keptHostsOf(plan, "storage"), []string{"node-1", "provided-1"}) || len(blanksOf(plan, "storage")) != 0 {
			t.Fatalf("the removal froze blanks %v and storage kept hosts %v", blanksOf(plan, "install"), keptHostsOf(plan, "storage"))
		}
		if removal.Contracts[testResetContract] == "" || removal.Contracts["store-kept-v1"] == "" || len(removal.Contracts) != len(implementationsOf(plan))+2 {
			t.Fatalf("the staged destroy recorded the contracts %v", removal.Contracts)
		}
		if resets := machine.resetsOf(); len(resets) != 1 || resets[0].Block.Object != "node-2" || resets[0].Step != "install" {
			t.Fatalf("the staged destroy reset %+v", resets)
		}
		if !slices.Equal(cluster.storage.destroys, []string{"storage"}) || !slices.Equal(cluster.install.destroysOf(), []string{"install"}) || len(cluster.storage.residue) != 0 {
			t.Fatalf("the staged destroy removed %v and %v, leaving %v", cluster.storage.destroys, cluster.install.destroysOf(), cluster.storage.residue)
		}
	})
	t.Run("on the same host", func(t *testing.T) {
		t.Parallel()
		cluster, machine := blanking(t, "node-1")
		h := cluster.h
		cluster.storage.hosts = []string{"node-1"}
		before := untouchedOf(h)
		_, err := blankingDestroy(h)
		if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "Machine/node-1 is powered off") ||
			!strings.Contains(reported[0].Message, "the step storage only while it is powered on") {
			t.Fatalf("a staged destroy blanking a host the storage teardown needs running returned %v", reported)
		}
		before.require(t, h)
		if resets := machine.resetsOf(); len(resets) != 0 {
			t.Fatalf("the refused staged destroy reset %+v", resets)
		}
	})
}

// B1865, B1862: a staged destroy narrowed to a storage cluster on kept hosts
// that must run repeats its clusters in the kept-host-stopped remedy, and a
// destroy narrowed so takes the cluster back once the host runs.
func TestANarrowedStagedDestroyOnAStoppedKeptHostRepeatsItsClusters(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	cluster.storage.definitions[0].ClusterOwners = []string{"store"}
	h := cluster.h
	cluster.failApply(t)
	cluster.power("node-1", false)
	before := untouchedOf(h)
	_, err := clusterDestroy(h, reconciliation.StageClusters, "store")
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyKeptHostStopped.Code || !strings.Contains(reported[0].Remediation, "bootwright machine start --context lab --name node-1") ||
		!strings.Contains(reported[0].Remediation, "bootwright destroy --context lab --stage clusters --clusters store") {
		t.Fatalf("a narrowed staged destroy over a stopped kept host returned %v", reported)
	}
	before.require(t, h)
	cluster.power("node-1", true)
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "store"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters store: %+v", diagnostics.Of(err))
	}
	if current, _ := currentRecord(t, h); !trimmed(current) || !slices.Equal(current.Stage.Clusters, []string{"store"}) {
		t.Fatalf("the narrowed staged destroy recorded %+v", current.Stage)
	}
}
