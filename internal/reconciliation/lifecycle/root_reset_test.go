package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// blankingNodeMachine is a virtual node Machine whose substrate blanks its
// root disk: marks holds, per Machine, the removal and step its reset names.
// lose loses the answer of a Machine's next reset, before its mark or after.
type blankingNodeMachine struct {
	*keptNodeMachine
	refusal    string
	fail       error
	marks      map[string]string
	resets     []RootReset
	presented  []string
	observed   []string
	lose       map[string]string
	installed  func() int
	mediaGone  func() int
	beforeMark []int
}

const testResetContract = "machine-test-root-reset-v1"

// testOperationStore is the operation store the harness composes: like the
// command's, it admits the root disk reset contracts of its node Machines.
func testOperationStore(records, logs operationstore.Area, clock func() time.Time) *operationstore.Store {
	return operationstore.New(records, logs, clock).KeptContracts(operationstore.BlanksRoots, testResetContract, killResetContract)
}

func (m *blankingNodeMachine) RootResetContract() (string, string) {
	return testResetContract, "test-root-reset"
}

func implementationsOf(plan reconciliation.Plan) map[string]bool {
	used := map[string]bool{}
	for _, block := range plan.Blocks {
		used[block.Implementation] = true
	}
	return used
}

func (m *blankingNodeMachine) RootResetSecrets(reconciliation.Block) ([]string, error) {
	return slices.Clone(m.access), nil
}

func (m *blankingNodeMachine) RootResetRefusal(reconciliation.Block) string { return m.refusal }

func (m *blankingNodeMachine) ResetRootDisk(_ context.Context, reset RootReset) (reconciliation.Outcome, reconciliation.EffectState, error) {
	installed, media := m.installed(), m.mediaGone()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.resets = append(m.resets, reset)
	m.presented = append(m.presented, reset.Block.Object+"="+presentedMaterial(reset.Material))
	m.beforeMark = append(m.beforeMark, installed, media)
	if m.fail != nil {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, m.fail
	}
	lose := m.lose[reset.Block.Object]
	delete(m.lose, reset.Block.Object)
	if lose == "before" {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, nil
	}
	if m.marks[reset.Block.Object] == reset.Removal+"/"+reset.Step {
		return reconciliation.OutcomeUnchanged, reconciliation.EffectCompleted, nil
	}
	m.marks[reset.Block.Object] = reset.Removal + "/" + reset.Step
	if lose == "after" {
		return reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, nil
	}
	return reconciliation.OutcomeChanged, reconciliation.EffectCompleted, nil
}

func (m *blankingNodeMachine) ObserveRootDisk(_ context.Context, reset RootReset) (reconciliation.EffectState, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.observed = append(m.observed, reset.Block.Object)
	if m.marks[reset.Block.Object] == reset.Removal+"/"+reset.Step {
		return reconciliation.EffectCompleted, nil
	}
	return reconciliation.EffectNoEffect, nil
}

func (m *blankingNodeMachine) resetsOf() []RootReset {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return slices.Clone(m.resets)
}

// blankingCluster is keptClusterHarness with node Machines whose substrate
// blanks a root disk, an installation that booted them, and extra, more
// blocks of the same lineage on node-1.
func blankingCluster(t *testing.T, extra ...func(*keptCluster)) (keptCluster, *blankingNodeMachine) {
	t.Helper()
	cluster := keptClusterHarness(t, false, "")
	cluster.install.boots = true
	machine := &blankingNodeMachine{keptNodeMachine: cluster.machine, marks: map[string]string{}, lose: map[string]string{},
		installed: func() int { return len(cluster.install.destroysOf()) }, mediaGone: func() int { return len(cluster.media.destroysOf()) }}
	resolver := cluster.h.service.capabilities.(keptResolver)
	for binding := range resolver {
		if binding.Kind == "Machine" {
			resolver[binding] = machine
		}
	}
	for _, change := range extra {
		change(&cluster)
	}
	return cluster, machine
}

func (c *testCapability) destroysOf() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return slices.Clone(c.destroys)
}

// blankingDestroy is the staged destroy at clusters with the data-loss its
// blanking consumes authorized.
func blankingDestroy(h *harness) (*OperationResult, error) {
	return h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters),
		Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true})
}

func blanksOf(plan reconciliation.Plan, id string) []string {
	block, found := plan.Block(id)
	if !found || block.Inverse == nil {
		return nil
	}
	return block.Inverse.Blanks
}

// B1861: a staged destroy at clusters takes back an installation that booted
// its virtual nodes while the node Machines stay, because the installation's
// removal blanks each node's root disk as its own inverse effect: frozen as
// the step's blanks, named in its impacts, consuming data-loss. Each reset
// runs through the node Machine's own substrate port, handed the block its
// apply froze, after the installation's eject and before the media's removal,
// and names the staged destroy and step. The restore then installs on the
// blank disks, under its own identity and with no earlier boot, while the
// node Machines are never applied again.
func TestAStagedDestroyBlanksTheRootDisksOfVirtualNodesAnInstallationBooted(t *testing.T) {
	t.Parallel()
	cluster, machine := blankingCluster(t)
	h := cluster.h
	applied := completeApply(t, h)
	nodeApplies := len(cluster.machine.applies)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if firstCode(err) != "authorize.missing" {
		t.Fatalf("a staged destroy that blanks root disks without data-loss returned %v", diagnostics.Of(err))
	}
	before.require(t, h)
	result, err := blankingDestroy(h)
	if err != nil {
		t.Fatalf("the staged destroy: %v", diagnostics.Of(err))
	}
	removal, plan := currentRecord(t, h)
	if !trimmed(removal) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2")) || result.Scope == nil {
		t.Fatalf("the staged destroy recorded %+v", removal)
	}
	install, _ := plan.Block("install")
	if !slices.Equal(blanksOf(plan, "install"), []string{"node-1", "node-2"}) || !slices.Equal(keptHostsOf(plan, "install"), []string{"node-1", "node-2"}) || len(blanksOf(plan, "media")) != 0 {
		t.Fatalf("the removal froze blanks %v over kept hosts %v, and %v for the media", blanksOf(plan, "install"), keptHostsOf(plan, "install"), blanksOf(plan, "media"))
	}
	if !slices.Contains(install.Consumes, reconciliation.AuthorizationDataLoss) ||
		!slices.Contains(install.Impacts, "blank the root disk of Machine/node-1, erasing the system installed on it") ||
		!slices.Contains(install.Impacts, "blank the root disk of Machine/node-2, erasing the system installed on it") ||
		!slices.Contains(install.Impacts, "keep the data disks and TPM state of Machine/node-2 with their contents") {
		t.Fatalf("the installation's removal reads %+v", install.BlockDefinition)
	}
	if removal.Contracts[testResetContract] != strings.Repeat("4", 64) || len(removal.Contracts) != len(implementationsOf(plan))+1 {
		t.Fatalf("the staged destroy recorded the contracts %v", removal.Contracts)
	}
	resets := machine.resetsOf()
	var reset []string
	for _, request := range resets {
		if request.Removal != removal.ID || request.Step != "install" || request.ApplyOperation != applied || request.Block.ID != request.Block.Object || len(request.PreviousEvidence) == 0 {
			t.Fatalf("a root disk was reset with %+v", request)
		}
		reset = append(reset, request.Block.Object)
	}
	if !slices.Equal(reset, []string{"node-1", "node-2"}) || !slices.Equal(machine.beforeMark, []int{1, 0, 1, 0}) {
		t.Fatalf("the staged destroy reset %v with the installation and media removals %v behind it", reset, machine.beforeMark)
	}
	if destroyed := slices.Concat(cluster.install.destroys, cluster.media.destroys, cluster.machine.destroys); !slices.Equal(destroyed, []string{"install", "media"}) {
		t.Fatalf("the staged destroy removed %v", destroyed)
	}

	cluster.install.executions = nil
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	var installed []Execution
	for _, execution := range cluster.install.executions {
		if execution.Block.ID == "install" {
			installed = append(installed, execution)
		}
	}
	if len(installed) != 1 || installed[0].ApplyOperation != restoring.ID || len(installed[0].ApplyPreparations) != 0 || len(installed[0].PreviousEvidence) != 0 {
		t.Fatalf("the restore installed with %+v", installed)
	}
	if len(cluster.machine.applies) != nodeApplies {
		t.Fatalf("the restore applied the kept nodes again: %v", cluster.machine.applies)
	}
}

// B1861 with B1868: each reset presents the kept node Machine's own binding
// with the staged destroy's rebind, never the binding of the step it runs
// under. After a restore re-installed the cluster under its own binding, which
// names no Secret, the rotated BMC credential only the nodes present is
// rebound by the next staged destroy, and every reset presents the new version.
func TestARootDiskResetPresentsTheRebindOfTheKeptNodesAccessCredential(t *testing.T) {
	t.Parallel()
	cluster, machine := blankingCluster(t, presentBMC, installNamesNoSecret)
	h := cluster.h
	completeApply(t, h)
	if _, err := blankingDestroy(h); err != nil {
		t.Fatalf("the first staged destroy: %v", diagnostics.Of(err))
	}
	restoreWith(t, h)
	h.binder.versions["bmc"] = "ver-2"
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true,
		Authorizations: []string{reconciliation.AuthorizationDataLoss, RebindAuthorization + "bmc"}})
	if err != nil {
		t.Fatalf("the rebound staged destroy: %v", diagnostics.Of(err))
	}
	removal, plan := currentRecord(t, h)
	if !trimmed(removal) || len(removal.Rebound) != 1 || !slices.Equal(blanksOf(plan, "install"), []string{"node-1", "node-2"}) {
		t.Fatalf("the rebound staged destroy recorded %+v", removal)
	}
	if want := []string{"node-1=first", "node-2=first", "node-1=second", "node-2=second"}; !slices.Equal(machine.presented, want) {
		t.Fatalf("the resets presented %v, want %v", machine.presented, want)
	}
}

// The blanking is admitted only where nothing kept lives on the disk and the
// substrate can blank it: a kept step on a node refuses naming it, a substrate
// without the port names the item that brings it, and its own refusal is
// named; a physical node still refuses (decision 8 of D288).
func TestABootedInstallationIsKeptOnlyWhereItsNodesRootDisksCanBeBlanked(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		arrange func(*keptCluster, *blankingNodeMachine)
		reason  string
	}{
		"a kept step on the disk": {func(cluster *keptCluster, _ *blankingNodeMachine) {
			os := keptBlock("os", "Machine", "os-v1", reconciliation.StageMachines, "node-1")
			os.Object = "node-1"
			cluster.h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: "Machine", Implementation: "os-v1"}] = &testCapability{definitions: []reconciliation.BlockDefinition{os}}
		}, "the step os, which stays, lives on Machine/node-1"},
		"a kept step of the node Machine itself": {func(cluster *keptCluster, _ *blankingNodeMachine) {
			own := keptBlock("own", "Machine", "own-v1", reconciliation.StageMachines)
			own.Object = "node-1"
			cluster.h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: "Machine", Implementation: "own-v1"}] = &testCapability{definitions: []reconciliation.BlockDefinition{own}}
		}, "the step own, which stays, lives on Machine/node-1"},
		"a substrate refusal": {func(_ *keptCluster, machine *blankingNodeMachine) {
			machine.refusal = "was realized by another build"
		}, "Machine/node-1 was realized by another build"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster, machine := blankingCluster(t)
			test.arrange(&cluster, machine)
			completeApply(t, cluster.h)
			before := untouchedOf(cluster.h)
			_, err := blankingDestroy(cluster.h)
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "its installation recorded a boot") ||
				!strings.Contains(reported[0].Message, "cannot blank their root disks: "+test.reason) {
				t.Fatalf("destroy --stage clusters returned %v", reported)
			}
			before.require(t, cluster.h)
			if len(machine.resetsOf()) != 0 {
				t.Fatalf("a refused staged destroy reset %v", machine.resetsOf())
			}
		})
	}
	cluster := keptClusterHarness(t, false, "")
	cluster.install.boots = true
	completeApply(t, cluster.h)
	_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "blanks no root disk yet (B1880)") {
		t.Fatalf("destroy --stage clusters over nodes whose substrate blanks nothing returned %v", reported)
	}
}

// A reset whose answer was lost is resolved by the root disks' marks: lost
// after its mark, the next staged destroy reads every disk marked and records
// the installation removed without resetting again; lost before it, a disk
// unmarked reads partial, so the same invocation removes it again, resets the
// disk and only then takes back the media.
func TestALostRootDiskResetIsResolvedByItsMark(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		machine, lose string
		resets        []string
	}{
		"after its mark":  {"node-2", "after", []string{"node-1", "node-2"}},
		"before its mark": {"node-1", "before", []string{"node-1", "node-1", "node-2"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster, machine := blankingCluster(t)
			h := cluster.h
			completeApply(t, h)
			machine.lose[test.machine] = test.lose
			if _, err := blankingDestroy(h); err == nil {
				t.Fatal("a staged destroy whose reset lost its answer completed")
			}
			if len(cluster.media.destroys) != 0 {
				t.Fatalf("the media was taken back past an unproved reset: %v", cluster.media.destroys)
			}
			cluster.install.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
			if _, err := blankingDestroy(h); err != nil {
				t.Fatalf("the continuation: %v", diagnostics.Of(err))
			}
			removal, _ := currentRecord(t, h)
			var reset []string
			for _, request := range machine.resetsOf() {
				reset = append(reset, request.Block.Object)
			}
			if !trimmed(removal) || !slices.Equal(reset, test.resets) || !slices.Equal(cluster.media.destroys, []string{"media"}) {
				t.Fatalf("the continuation left %+v after resets %v and media removals %v", removal, reset, cluster.media.destroys)
			}
		})
	}
}

// A staged destroy continued under another build re-proves each kept
// Machine's reset before any effect: a node Machine block whose contract this
// build no longer supports refuses as another build's, a reset contract other
// than the one the removal recorded refuses, and a substrate that now refuses
// the reset moves the frozen inverse. None resets a disk or takes back a step.
func TestAContinuedRootDiskResetIsReprovedBeforeAnyEffect(t *testing.T) {
	t.Parallel()
	moved := func(h *harness, moves func(reconciliation.Block) bool) {
		current := h.service.options.Contracts
		h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
			contract, ok := current(block)
			if moves(block) {
				contract.Fingerprint = strings.Repeat("8", 64)
			}
			return contract, ok
		}
	}
	for name, test := range map[string]struct {
		arrange       func(*harness, *blankingNodeMachine)
		code, message string
	}{
		"another Machine contract": {func(h *harness, _ *blankingNodeMachine) {
			moved(h, func(block reconciliation.Block) bool {
				return block.Kind == "Machine" && block.Implementation != testResetContract
			})
		}, engineOtherBuild.Code, "does not support the recorded execution contract of block node-1"},
		"another reset contract": {func(h *harness, _ *blankingNodeMachine) {
			moved(h, func(block reconciliation.Block) bool { return block.Implementation == testResetContract })
		}, engineOtherBuild.Code, "does not support the recorded root reset contract of Machine/node-1"},
		"a substrate refusal": {func(_ *harness, machine *blankingNodeMachine) {
			machine.refusal = "was realized under another machine contract"
		}, destroyInverseMoved.Code, "blanks the root disk of Machine/node-1, and now Machine/node-1 was realized under another machine contract"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster, machine := blankingCluster(t)
			h := cluster.h
			completeApply(t, h)
			machine.lose["node-1"] = "before"
			if _, err := blankingDestroy(h); err == nil {
				t.Fatal("a staged destroy whose reset lost its answer completed")
			}
			test.arrange(h, machine)
			resets, removed := len(machine.resetsOf()), len(cluster.install.destroysOf())
			before := untouchedOf(h)
			_, err := blankingDestroy(h)
			if reported := diagnostics.Of(err); firstCode(err) != test.code || !strings.Contains(reported[0].Message, test.message) {
				t.Fatalf("the continuation returned %v", reported)
			}
			before.require(t, h)
			if len(machine.resetsOf()) != resets || len(cluster.install.destroysOf()) != removed || len(cluster.media.destroysOf()) != 0 {
				t.Fatalf("a refused continuation reset %v and removed %v, %v", machine.resetsOf(), cluster.install.destroysOf(), cluster.media.destroysOf())
			}
		})
	}
}

// settledRemoval is a step whose removal settles with outcome and evidence, counting its runs.
type settledRemoval struct {
	*keptNodeInstall
	result Result
	runs   *int
}

func (r settledRemoval) Destroy(context.Context, Execution) (Result, error) {
	*r.runs++
	return r.result, nil
}

// A step's removal keeps its own outcome and evidence beside its resets: a
// removal unchanged over disks already marked stays unchanged, a reset that
// fails keeps the removal's evidence, and an operation log that cannot record
// a reset refuses it before the reset runs. Under a node Machine contract this
// build does not support, the attempt refuses before the step's own removal.
func TestARemovalThatBlanksKeepsItsOwnOutcomeAndEvidence(t *testing.T) {
	t.Parallel()
	cluster, machine := blankingCluster(t)
	h := cluster.h
	completeApply(t, h)
	if _, err := blankingDestroy(h); err != nil {
		t.Fatalf("the staged destroy: %v", diagnostics.Of(err))
	}
	removal, plan := currentRecord(t, h)
	install, _ := plan.Block("install")
	evidence := json.RawMessage(`{"removed":true}`)
	runs := 0
	removed := settledRemoval{keptNodeInstall: cluster.install, result: Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, runs: &runs}
	run := invocation{store: h.service.store(h.workspace.view()), operation: removal}
	execution := Execution{Context: testContextName, Block: install, Attempt: 2}
	result, err := h.service.removeBlanking(context.Background(), run, removed, execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || string(result.Evidence) != string(evidence) {
		t.Fatalf("a removal over disks already marked read %+v (%v)", result, err)
	}
	machine.fail = errors.New("the hypervisor went away")
	result, err = h.service.removeBlanking(context.Background(), run, removed, execution)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown || string(result.Evidence) != string(evidence) {
		t.Fatalf("a removal whose reset failed read %+v (%v)", result, err)
	}
	machine.fail = nil
	resets := len(machine.resetsOf())
	execution.Log = func(context.Context, operationstore.LogRecord) error { return errors.New("the log is full") }
	result, err = h.service.removeBlanking(context.Background(), run, removed, execution)
	if err == nil || len(machine.resetsOf()) != resets || string(result.Evidence) != string(evidence) {
		t.Fatalf("a reset the log could not record ran %d resets and read %+v (%v)", len(machine.resetsOf())-resets, result, err)
	}
	execution.Log, runs = nil, 0
	current := h.service.options.Contracts
	h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		if block.ID == "node-2" {
			contract.Fingerprint = strings.Repeat("8", 64)
		}
		return contract, ok
	}
	_, err = h.service.removeBlanking(context.Background(), run, removed, execution)
	if firstCode(err) != engineOtherBuild.Code || runs != 0 || len(machine.resetsOf()) != resets {
		t.Fatalf("a removal under another node Machine contract returned %v after %d removals and %d resets", diagnostics.Of(err), runs, len(machine.resetsOf())-resets)
	}
}
