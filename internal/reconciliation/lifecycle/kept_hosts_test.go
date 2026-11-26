package lifecycle

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

type keptResolver map[CapabilityBinding]Capability

func (r keptResolver) Bindings() []CapabilityBinding {
	bindings := make([]CapabilityBinding, 0, len(r))
	for binding := range r {
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(x, y CapabilityBinding) int {
		return strings.Compare(x.Kind+x.Implementation, y.Kind+y.Implementation)
	})
	return bindings
}

func (r keptResolver) Resolve(kind, implementation string) (Capability, bool) {
	for binding, capability := range r {
		if binding.Kind == kind && binding.Implementation == implementation {
			return capability, true
		}
	}
	return nil, false
}

type keptNodeMachine struct {
	*testCapability
	physical bool
	on       map[string]bool
	powered  []string
	probed   []Probe
	access   []string
	read     map[string][]string
}

func (m *keptNodeMachine) DeletesMachine(reconciliation.Block) bool { return !m.physical }
func (m *keptNodeMachine) RemovesHost(reconciliation.Block) bool    { return !m.physical }
func (m *keptNodeMachine) StartPowerSecrets(reconciliation.Block) ([]string, error) {
	return slices.Clone(m.access), nil
}

func (m *keptNodeMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	m.record(&m.powered, probe.Block.Object)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.probed = append(m.probed, probe)
	if len(m.access) != 0 {
		if m.read == nil {
			m.read = map[string][]string{}
		}
		m.read[probe.Block.Object] = append(m.read[probe.Block.Object], presentedMaterial(probe.Material))
	}
	if m.on[probe.Block.Object] {
		return Quiescence{State: Live, Reason: "powered on", Stop: "bootwright machine stop " + probe.Block.Object + " --context lab"}, nil
	}
	return Quiescence{State: Quiescent}, nil
}

type keptNodeInstall struct {
	*testCapability
	nodes  []string
	boots  bool
	refuse string
}

func (c *keptNodeInstall) NodeMachines(reconciliation.Block) ([]string, error) { return c.nodes, nil }

func (c *keptNodeInstall) KeptHostsRefusal(_ reconciliation.Block, records RemovalRecords) string {
	switch {
	case c.refuse != "":
		return c.refuse
	case len(records.Preparations) != 0:
		return "its installation recorded a boot"
	}
	return ""
}

func (c *keptNodeInstall) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.Removal(ctx, block)
	removal.Description = "eject the media of " + block.Object
	return removal, err
}

func (c *keptNodeInstall) Apply(ctx context.Context, execution Execution) (Result, error) {
	if c.boots && execution.Prepare != nil {
		if err := execution.Prepare(ctx, json.RawMessage(`{"boot":true}`)); err != nil {
			return Result{Outcome: reconciliation.OutcomeFailed}, err
		}
	}
	return c.testCapability.Apply(ctx, execution)
}

type keptNodeMedia struct{ *testCapability }

func (keptNodeMedia) KeptHostsRefusal(reconciliation.Block, RemovalRecords) string { return "" }
func (c keptNodeMedia) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (Removal, error) {
	return c.Removal(ctx, block)
}

func (keptNodeMedia) Keeps(reconciliation.Block) (KeptEntry, bool, error) {
	return KeptEntry{Block: "install", Name: "kubeconfig"}, true, nil
}

func (keptNodeMedia) Keep(context.Context, Execution) ([]Produced, error) {
	return []Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("authority-a\nkept")})}}, nil
}

type keptNodeAddon struct{ *testCapability }

func (keptNodeAddon) RetiresOffline() bool { return true }

type keptCluster struct {
	h       *harness
	machine *keptNodeMachine
	install *keptNodeInstall
	media   *testCapability
	addon   *testCapability
}

func keptBlock(id, kind, implementation string, stage reconciliation.Stage, dependencies ...string) reconciliation.BlockDefinition {
	block := stagedDefinition(id, stage, dependencies...)
	block.Kind, block.Implementation = kind, implementation
	return block
}

// keptClusterHarness holds an OpenShift cluster on two virtual node Machines:
// its media reads the first node's volume, its installation runs on both, and
// an add-on that retires offline rides on the installation, unless addonOn
// binds the add-on to a node directly; through adds a service on a third
// Machine the installation reaches only through that service.
func keptClusterHarness(t *testing.T, physical bool, addonOn string, through ...bool) keptCluster {
	t.Helper()
	h := newHarness(t)
	host := keptBlock("host", "InfraProvider", "host-v1", reconciliation.StageSubstrates)
	var nodes []reconciliation.BlockDefinition
	for _, name := range []string{"node-1", "node-2"} {
		node := keptBlock(name, "Machine", "node-machine-v1", reconciliation.StageMachines, "host")
		node.Object = name
		nodes = append(nodes, node)
	}
	media := keptBlock("media", "ClusterMedia", "media-v1", reconciliation.StageClusters, "node-1")
	install := keptBlock("install", "OpenShiftCluster", "install-v1", reconciliation.StageClusters, "media", "node-1", "node-2")
	services := []reconciliation.BlockDefinition{}
	if len(through) != 0 && through[0] {
		server := keptBlock("server", "Machine", "node-machine-v1", reconciliation.StageMachines, "host")
		server.Object = "server"
		nodes = append(nodes, server)
		services = append(services, keptBlock("service", "ArtifactServer", "artifact-server-nginx-v1", reconciliation.StageMachines, "server"))
		install.Dependencies = append(install.Dependencies, "service")
	}
	media.Object, install.Object = "edge", "edge"
	addon := keptBlock("addon", "OpenShiftAddon", "addon-v1", reconciliation.StageAddOns, "install")
	if addonOn != "" {
		addon.Dependencies = []string{addonOn}
	}
	cluster := keptCluster{
		h:       h,
		machine: &keptNodeMachine{testCapability: &testCapability{definitions: nodes}, physical: physical, on: map[string]bool{}},
		install: &keptNodeInstall{testCapability: &testCapability{definitions: []reconciliation.BlockDefinition{install}}, nodes: []string{"node-1", "node-2"}},
		media:   &testCapability{definitions: []reconciliation.BlockDefinition{media}},
		addon:   &testCapability{definitions: []reconciliation.BlockDefinition{addon}},
	}
	h.service.capabilities = keptResolver{
		{Kind: "InfraProvider", Implementation: "host-v1"}:                                                &testCapability{definitions: []reconciliation.BlockDefinition{host}},
		{Kind: "Machine", Implementation: "node-machine-v1", Family: reconciliation.ManagedMachineFamily}: cluster.machine,
		{Kind: "ClusterMedia", Implementation: "media-v1"}:                                                keptNodeMedia{cluster.media},
		{Kind: "OpenShiftCluster", Implementation: "install-v1"}:                                          cluster.install,
		{Kind: "OpenShiftAddon", Implementation: "addon-v1"}:                                              keptNodeAddon{cluster.addon},
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}:                              &testCapability{definitions: services},
	}
	h.capability = cluster.install.testCapability
	return cluster
}

func (c keptCluster) powered() []string {
	c.machine.mutex.Lock()
	defer c.machine.mutex.Unlock()
	return slices.Clone(c.machine.powered)
}

func (c keptCluster) resetPower() {
	c.machine.mutex.Lock()
	defer c.machine.mutex.Unlock()
	c.machine.powered = nil
}

func (c keptCluster) power(node string, on bool) {
	c.machine.mutex.Lock()
	defer c.machine.mutex.Unlock()
	c.machine.on[node] = on
}

func keptHostsOf(plan reconciliation.Plan, id string) []string {
	block, found := plan.Block(id)
	if !found || block.Inverse == nil {
		return nil
	}
	return block.Inverse.KeptHosts
}

func keptPower(nodes ...string) []operationstore.KeptPower {
	var targets []operationstore.KeptPower
	for _, node := range nodes {
		targets = append(targets, operationstore.KeptPower{Block: node, Machine: node, Power: operationstore.PoweredOff})
	}
	return targets
}

// B1860: a staged destroy at clusters takes back an installation whose apply
// recorded no boot, its media and the add-on that retires offline with it,
// while the node Machines stay: the removal freezes the kept nodes on the
// steps that run on them and as power targets, probes them off, and keeps
// them. The restore then installs on them under its own identity, with no
// earlier boot record, and proves them off first.
func TestAStagedDestroyTakesBackAnInstallationNoNodeBootedOnItsKeptNodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := keptClusterHarness(t, false, "")
	h := cluster.h
	cluster.install.outcomeFor = map[string]Result{"install": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an installation scripted to fail completed")
	}
	cluster.install.outcomeFor = nil
	failed, _ := currentRecord(t, h)
	cluster.resetPower()
	result := stagedDestroy(t, h, reconciliation.StageClusters)
	removal, plan := currentRecord(t, h)
	if !trimmed(removal) || !slices.Equal(planIDs(plan), []string{"install", "media"}) ||
		!slices.Equal(removal.Stage.TakenBack, []string{"addon", "install", "media"}) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2")) {
		t.Fatalf("the staged destroy recorded %+v over %v", removal, planIDs(plan))
	}
	if !slices.Equal(keptHostsOf(plan, "install"), []string{"node-1", "node-2"}) || !slices.Equal(keptHostsOf(plan, "media"), []string{"node-1"}) {
		t.Fatalf("the removal froze the kept hosts %v and %v", keptHostsOf(plan, "install"), keptHostsOf(plan, "media"))
	}
	if install, _ := plan.Block("install"); install.Description != "eject the media of edge" {
		t.Fatalf("the installation's removal reads %q, want its kept removal", install.Description)
	}
	if got := cluster.powered(); !slices.Contains(got, "node-1") || !slices.Contains(got, "node-2") {
		t.Fatalf("the staged destroy read the power of %v", got)
	}
	if result.Scope == nil || !slices.Equal(result.Scope.KeptPowerTargets, []string{"Machine/node-1 powered off", "Machine/node-2 powered off"}) {
		t.Fatalf("the staged destroy's scope reads %+v", result.Scope)
	}
	if destroyed := slices.Concat(cluster.install.destroys, cluster.machine.destroys); !slices.Equal(destroyed, []string{"install"}) {
		t.Fatalf("the staged destroy removed %v", destroyed)
	}

	cluster.resetPower()
	cluster.install.executions = nil
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	if restoring.Restores != removal.ID || restoring.Predecessor != failed.ID {
		t.Fatalf("the restore recorded %+v", restoring)
	}
	var installed []Execution
	for _, execution := range cluster.install.executions {
		if execution.Block.ID == "install" {
			installed = append(installed, execution)
		}
	}
	if len(installed) != 1 || installed[0].ApplyOperation != restoring.ID || installed[0].Attempt != 1 || len(installed[0].ApplyPreparations) != 0 || len(installed[0].PreviousEvidence) != 0 {
		t.Fatalf("the restore installed with %+v", installed)
	}
	if got := cluster.powered(); !slices.Equal(slices.Sorted(slices.Values(got)), []string{"node-1", "node-2"}) {
		t.Fatalf("the restore read the power of %v, want the kept nodes its installation acts on", got)
	}
}

// A kept node that is not powered off refuses every invocation of the staged
// destroy before it takes anything back: the fresh one, the replacement of a
// failed one at its floor and the continuation of an unknown one, each naming
// the stop command and the destroy at its floor.
func TestAKeptNodeThatIsOnRefusesEveryInvocationOfTheStagedDestroy(t *testing.T) {
	t.Parallel()
	for name, arrange := range map[string]func(*testing.T, keptCluster){
		"fresh": func(*testing.T, keptCluster) {},
		"replacement": func(t *testing.T, cluster keptCluster) {
			cluster.install.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			if _, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("a staged destroy whose step failed completed")
			}
		},
		"continuation": func(t *testing.T, cluster keptCluster) {
			cluster.install.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if _, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("a staged destroy whose step is unknown completed")
			}
			cluster.install.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptClusterHarness(t, false, "")
			h := cluster.h
			completeApply(t, h)
			arrange(t, cluster)
			cluster.install.outcomes = nil
			if current, _ := currentRecord(t, h); name != "fresh" && (!staged(current) || current.State == reconciliation.OperationDone) {
				t.Fatalf("the arranged staged destroy reads %+v", current)
			}
			cluster.power("node-2", true)
			before := untouchedOf(h)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyInUse.Code || !strings.Contains(reported[0].Message, "Machine/node-2 (powered on)") ||
				!strings.Contains(reported[0].Remediation, "bootwright machine stop node-2 --context lab") || !strings.Contains(reported[0].Remediation, "bootwright destroy --context lab --stage clusters") {
				t.Fatalf("destroy --stage clusters over a running kept node returned %v", reported)
			}
			before.require(t, h)
			cluster.power("node-2", false)
			stagedDestroy(t, h, reconciliation.StageClusters)
			if current, _ := currentRecord(t, h); !trimmed(current) {
				t.Fatalf("the staged destroy over the stopped node left %+v", current)
			}
		})
	}
}

// A node that was handed the image keeps refusing: the installation's records
// hold a boot, so the staged destroy names why and what unlocks it, the
// root-disk blanking of a kept virtual node (B1861) or, for a physical node,
// the owner's decision on physical installs (B1864).
func TestAStagedDestroyRefusesAnInstallationThatRecordedABoot(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		physical       bool
		reason, remedy string
	}{
		"virtual nodes":  {reason: "(B1861)", remedy: "bootwright destroy --dry-run --context lab --stage machines"},
		"physical nodes": {physical: true, reason: "(B1864)", remedy: "take back the whole context with bootwright destroy --context lab"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptClusterHarness(t, test.physical, "")
			cluster.install.boots = true
			completeApply(t, cluster.h)
			before := untouchedOf(cluster.h)
			_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step install runs on Machine/node-1, Machine/node-2") ||
				!strings.Contains(reported[0].Message, "its installation recorded a boot") || !strings.Contains(reported[0].Message, test.reason) ||
				!strings.Contains(reported[0].Remediation, test.remedy) {
				t.Fatalf("destroy --stage clusters over a booted installation returned %v", reported)
			}
			before.require(t, cluster.h)
		})
	}
}

// A step that retires offline is taken back on kept nodes only beside the
// installation that is: an add-on bound to a node directly, with no admitted
// installation beneath it, refuses as any step on a kept Machine does.
func TestAnOfflineStepIsTakenBackOnKeptNodesOnlyWithItsInstallation(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "node-2")
	completeApply(t, cluster.h)
	before := untouchedOf(cluster.h)
	_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step addon runs on Machine/node-2") {
		t.Fatalf("destroy --stage add-ons over an add-on bound to a kept node returned %v", reported)
	}
	before.require(t, cluster.h)
}

// A frozen answer that the installation's records no longer prove refuses
// destroy.inverse-moved before any effect, at the replacement of a failed
// staged destroy and at the continuation of an unknown one.
func TestAKeptAnswerTheRecordsNoLongerProveRefusesBeforeAnyEffect(t *testing.T) {
	t.Parallel()
	for name, outcome := range map[string]reconciliation.Outcome{"replacement": reconciliation.OutcomeFailed, "continuation": reconciliation.OutcomeUnknown} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptClusterHarness(t, false, "")
			completeApply(t, cluster.h)
			cluster.install.outcomes = []Result{{Outcome: outcome}}
			if _, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("the arranged staged destroy completed")
			}
			cluster.install.outcomes, cluster.install.refuse = nil, "its installation recorded a boot"
			before := untouchedOf(cluster.h)
			_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
			if reported := diagnostics.Of(err); firstCode(err) != destroyInverseMoved.Code || !strings.Contains(reported[0].Message, "the step install is taken back while Machine/node-1, Machine/node-2 stay") {
				t.Fatalf("the %s over a moved answer returned %v", name, reported)
			}
			before.require(t, cluster.h)
		})
	}
}

// The completion discards the cluster's custody entries with its
// installation, among them the copy the media's removal keeps under the
// installation of the kubeconfig its work area held, so the restore's capture
// of the new cluster's identity is no recapture of another one.
func TestATakenBackInstallationsCustodyGivesWayToTheRestoresNewCluster(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "")
	h := cluster.h
	cluster.install.identity = func(_ string, material secrets.Material) string {
		value, _ := material.Part(secrets.ValuePart)
		return strings.SplitN(string(value), "\n", 2)[0]
	}
	cluster.install.outcomeFor = map[string]Result{"install": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an installation scripted to fail completed")
	}
	h.binder.kill = func(point string) error {
		if point == "withdraw produced material" && !slices.Contains(kept(h), "install/kubeconfig (unproved)=authority-a\nkept") {
			t.Errorf("the completion withdrew custody %v, want the kept copy among it", kept(h))
		}
		return nil
	}
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.binder.kill = nil
	if held := kept(h); len(held) != 0 {
		t.Fatalf("the trimmed context keeps custody %v", held)
	}
	cluster.install.outcomeFor = map[string]Result{"install": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: []Produced{
		{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("authority-b\nsecond")})},
		{Name: "console-password", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("authority-b\nsecond")})},
	}}}
	restoreWith(t, h)
	if held := kept(h); !slices.Equal(held, []string{"install/console-password=authority-b\nsecond", "install/kubeconfig=authority-b\nsecond"}) {
		t.Fatalf("the restore keeps custody %v", held)
	}
}

// A step is taken back on kept Machines only where it names them itself: an
// installation that also reaches a kept Machine through a service it depends
// on refuses as any step on a kept Machine does, and no kept power target
// demands that service's host off.
func TestAnInstallationReachingAKeptMachineThroughAServiceRefuses(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "", true)
	completeApply(t, cluster.h)
	before := untouchedOf(cluster.h)
	_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step install runs on Machine/node-1, Machine/node-2, Machine/server") {
		t.Fatalf("destroy --stage clusters over an installation reaching a service host returned %v", reported)
	}
	before.require(t, cluster.h)
}

// A physical node is kept and its installation taken back while no node was
// handed the image: the staged destroy admits it, freezes it as a power target
// and reads its power through its Machine's start-power port, handed the
// identity that Machine's apply recorded, as the bare-metal port proves the
// same hardware is off.
func TestAStagedDestroyAdmitsAPhysicalNodeBeforeAnyBootAndReadsItsPower(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, true, "")
	applied := completeApply(t, cluster.h)
	cluster.machine.mutex.Lock()
	cluster.machine.probed = nil
	cluster.machine.mutex.Unlock()
	stagedDestroy(t, cluster.h, reconciliation.StageClusters)
	removal, plan := currentRecord(t, cluster.h)
	if !trimmed(removal) || !slices.Equal(keptHostsOf(plan, "install"), []string{"node-1", "node-2"}) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2")) {
		t.Fatalf("the staged destroy over physical nodes recorded %+v over %v", removal, planIDs(plan))
	}
	cluster.machine.mutex.Lock()
	probed := slices.Clone(cluster.machine.probed)
	cluster.machine.mutex.Unlock()
	var read []string
	for _, probe := range probed {
		if probe.ApplyOperation != applied || probe.Context != testContextName || len(probe.PreviousEvidence) == 0 {
			t.Fatalf("the power of Machine/%s was read with %+v, want its apply's recorded identity", probe.Block.Object, probe)
		}
		read = append(read, probe.Block.Object)
	}
	if slices.Sort(read); !slices.Equal(slices.Compact(read), []string{"node-1", "node-2"}) {
		t.Fatalf("the staged destroy read the power of %v", read)
	}
	if destroyed := cluster.machine.destroys; len(destroyed) != 0 {
		t.Fatalf("the staged destroy removed the physical nodes %v", destroyed)
	}
}

// A removal on kept nodes that the installation stops leaves the media's
// removal unrun, so the work area that proved a node was handed the image is
// not discarded.
func TestAKeptInstallationThatStopsLeavesItsMediaUnremoved(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "")
	completeApply(t, cluster.h)
	cluster.install.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose installation stopped completed")
	}
	if !slices.Equal(cluster.install.destroys, []string{"install"}) || len(cluster.media.destroys) != 0 {
		t.Fatalf("the staged destroy removed %v then %v", cluster.install.destroys, cluster.media.destroys)
	}
}

// Completion discards only the custody entries of the steps a staged destroy
// takes back: a kept node's own entry stays.
func TestAStagedDestroyKeepsTheCustodyOfAKeptStep(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "")
	h := cluster.h
	cluster.machine.outcomeFor = map[string]Result{"node-1": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: []Produced{
		{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("node")})},
	}}}
	cluster.install.outcomeFor = map[string]Result{"install": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: []Produced{
		{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("cluster")})},
	}}}
	completeApply(t, h)
	if held := kept(h); !slices.Equal(held, []string{"install/kubeconfig=cluster", "node-1/kubeconfig=node"}) {
		t.Fatalf("the apply keeps custody %v", held)
	}
	stagedDestroy(t, h, reconciliation.StageClusters)
	if held := kept(h); !slices.Equal(held, []string{"node-1/kubeconfig=node"}) {
		t.Fatalf("the trimmed context keeps custody %v, want only the kept node's", held)
	}
}

// B1868 with B1860: the restore after a staged destroy at machines re-created
// the nodes under its own binding, and the staged destroy at clusters rebinds
// the rotated BMC credential, so the kept-power reading of each kept node
// presents the rebound version, never the lineage root's nor the restore's.
func TestAStagedDestroyProvesKeptNodesOffPresentingTheirRebind(t *testing.T) {
	t.Parallel()
	cluster := bmcKeptCluster(t)
	h := cluster.h
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	h.binder.versions["bmc"] = "ver-2"
	restoreWith(t, h)
	h.binder.versions["bmc"] = "ver-3"
	cluster.takeReadings()
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	removal, _ := currentRecord(t, h)
	if !trimmed(removal) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2")) || len(removal.Rebound) != 1 {
		t.Fatalf("the staged destroy recorded %+v", removal)
	}
	if read := cluster.takeReadings(); !allOf(read["node-1"], "third") || !allOf(read["node-2"], "third") {
		t.Fatalf("the kept-power readings presented %v, want the rebound version", read)
	}
}

// bmcKeptCluster is keptClusterHarness whose nodes' power reading and
// installation present bmc as an access credential, at version ver-1.
func bmcKeptCluster(t *testing.T) keptCluster {
	t.Helper()
	cluster := keptClusterHarness(t, false, "")
	for index, definition := range cluster.machine.definitions {
		cluster.machine.definitions[index].Request = json.RawMessage(`{"credentialsRef":"bmc","name":"` + definition.ID + `"}`)
	}
	cluster.install.definitions[0].Request = json.RawMessage(`{"credentialsRef":"bmc","name":"install"}`)
	cluster.machine.access, cluster.machine.secrets, cluster.install.secrets = []string{"bmc"}, []string{"bmc"}, []string{"bmc"}
	cluster.machine.testCapability.access = map[string][]string{"node-1": {"bmc"}, "node-2": {"bmc"}}
	cluster.install.testCapability.access = map[string][]string{"install": {"bmc"}}
	cluster.h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	cluster.h.binder.versions = map[string]string{"bmc": "ver-1"}
	cluster.h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second"), "ver-3": password("third")}
	return cluster
}

func (c keptCluster) takeReadings() map[string][]string {
	c.machine.mutex.Lock()
	defer c.machine.mutex.Unlock()
	read := maps.Clone(c.machine.read)
	c.machine.read = nil
	return read
}

// A replacement of a staged destroy proves each kept node off with the binding
// that presents it, here a restore's that no step it takes back presents.
func TestAReplacedStagedDestroyProvesKeptNodesOffWithTheirOwnBinding(t *testing.T) {
	t.Parallel()
	cluster := bmcKeptCluster(t)
	h := cluster.h
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	h.binder.versions["bmc"] = "ver-2"
	restoreWith(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.binder.versions["bmc"] = "ver-3"
	restoreWith(t, h)
	cluster.install.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose step failed completed")
	}
	cluster.install.outcomes = nil
	cluster.takeReadings()
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err != nil {
		t.Fatalf("the replacement staged destroy: %+v", diagnostics.Of(err))
	}
	if read := cluster.takeReadings(); !allOf(read["node-1"], "second") || !allOf(read["node-2"], "second") {
		t.Fatalf("the replacement's kept-power readings presented %v, want the version the nodes' restore bound", read)
	}
}

// nodeOnlyBMC is bmcKeptCluster whose installation names no Secret, so only
// the kept nodes present bmc.
func nodeOnlyBMC(t *testing.T) keptCluster {
	t.Helper()
	cluster := bmcKeptCluster(t)
	cluster.install.definitions[0].Request = json.RawMessage(`{"name":"install"}`)
	cluster.install.secrets, cluster.install.testCapability.access = nil, nil
	return cluster
}

// A staged destroy rebinds the rotated BMC credential only its kept nodes
// present, for the kept Machines it proves off, and records the rebind, so
// its kept-power reading and the restore's present the new version.
func TestAStagedDestroyRebindsTheAccessCredentialOfAKeptNodeItProvesOff(t *testing.T) {
	t.Parallel()
	cluster := nodeOnlyBMC(t)
	h := cluster.h
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	cluster.takeReadings()
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	removal, _ := currentRecord(t, h)
	if !trimmed(removal) || len(removal.Rebound) != 1 {
		t.Fatalf("the staged destroy recorded %+v", removal)
	}
	if read := cluster.takeReadings(); !allOf(read["node-1"], "second") || !allOf(read["node-2"], "second") {
		t.Fatalf("the kept-power readings presented %v, want the rebound version", read)
	}
	restoreWith(t, h)
	if read := cluster.takeReadings(); !allOf(read["node-1"], "second") || !allOf(read["node-2"], "second") {
		t.Fatalf("the restore read the kept nodes' power presenting %v, want the rebound version", read)
	}
}

// A continued staged destroy rebinds the BMC credential of its kept nodes,
// rotated after it started, and proves them off presenting the new version.
func TestAContinuedStagedDestroyRebindsTheAccessCredentialOfAKeptNode(t *testing.T) {
	t.Parallel()
	cluster := nodeOnlyBMC(t)
	h := cluster.h
	completeApply(t, h)
	cluster.install.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose step is unknown completed")
	}
	cluster.install.outcomes = nil
	cluster.install.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	started, _ := currentRecord(t, h)
	h.binder.versions["bmc"] = "ver-2"
	cluster.takeReadings()
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound continuation: %+v", diagnostics.Of(err))
	}
	removal, _ := currentRecord(t, h)
	if removal.ID != started.ID || !trimmed(removal) || len(removal.Rebound) != 1 {
		t.Fatalf("the continuation recorded %+v after %+v", removal, started)
	}
	if read := cluster.takeReadings(); !allOf(read["node-1"], "second") || !allOf(read["node-2"], "second") {
		t.Fatalf("the continuation's kept-power readings presented %v, want the rebound version", read)
	}
}
