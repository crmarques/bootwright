package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// keptPowerMachine is a managed Machine whose power reading presents bmc and
// records the version each reading was handed.
type keptPowerMachine struct {
	*testCapability
	power    sync.Mutex
	state    string
	readings []string
	removals []string
}

func (m *keptPowerMachine) Destroy(ctx context.Context, execution Execution) (Result, error) {
	m.power.Lock()
	m.removals = append(m.removals, presentedMaterial(execution.Material))
	m.power.Unlock()
	return m.testCapability.Destroy(ctx, execution)
}

func (m *keptPowerMachine) StartPowerSecrets(reconciliation.Block) ([]string, error) {
	return []string{"bmc"}, nil
}

func (m *keptPowerMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	m.power.Lock()
	defer m.power.Unlock()
	m.readings = append(m.readings, presentedMaterial(probe.Material))
	return Quiescence{State: m.state, Reason: "native state"}, nil
}

func (m *keptPowerMachine) read() []string {
	m.power.Lock()
	defer m.power.Unlock()
	taken := m.readings
	m.readings = nil
	return taken
}

func (m *keptPowerMachine) set(state string) {
	m.power.Lock()
	defer m.power.Unlock()
	m.state = state
}

func presentedMaterial(material map[string]secrets.Material) string {
	value, _ := material["bmc"].Part(secrets.PasswordPart)
	return string(value)
}

// kindResolver resolves each bound kind to its own capability.
type kindResolver struct {
	bindings     []CapabilityBinding
	capabilities map[string]Capability
}

func (r kindResolver) Bindings() []CapabilityBinding { return r.bindings }

func (r kindResolver) Resolve(kind, implementation string) (Capability, bool) {
	if !slices.ContainsFunc(r.bindings, func(bound CapabilityBinding) bool {
		return bound.Kind == kind && bound.Implementation == implementation
	}) {
		return nil, false
	}
	capability, found := r.capabilities[kind]
	return capability, found
}

// keptMachineHarness plans a base step, a cluster step a staged destroy at
// clusters takes back, and a managed Machine node-a it keeps, whose step
// presents bmc as its access credential. Each drive records its direction and
// the version it was handed.
func keptMachineHarness(t *testing.T, base, cluster string) (*harness, *keptPowerMachine, func() []string) {
	t.Helper()
	baseStep, clusterStep := stagedDefinition("base", reconciliation.StageInfrastructureServices), stagedDefinition("cluster", reconciliation.StageClusters, "base")
	baseStep.Request, clusterStep.Request = json.RawMessage(base), json.RawMessage(cluster)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{baseStep, clusterStep})
	h.capability.secrets = []string{"pull"}
	node := stagedDefinition("node-a", reconciliation.StageMachines)
	node.Kind, node.Object, node.Implementation = "Machine", "node-a", "machine-v1"
	node.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"node-a"}`)
	machine := &keptPowerMachine{testCapability: &testCapability{definitions: []reconciliation.BlockDefinition{node}, secrets: []string{"bmc"}, access: map[string][]string{"node-a": {"bmc"}}}, state: Quiescent}
	h.service.capabilities = kindResolver{
		bindings: []CapabilityBinding{
			{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"},
			{Kind: "Machine", Implementation: "machine-v1", Family: reconciliation.ManagedMachineFamily},
		},
		capabilities: map[string]Capability{"ArtifactServer": h.capability, "Machine": machine},
	}
	h.binder.material = map[string]secrets.Material{"bmc": password("first"), "pull": password("pull"), "keep": password("keep")}
	h.binder.versions = map[string]string{"bmc": "ver-1", "pull": "pull-1", "keep": "keep-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second"), "pull-1": password("pull"), "keep-1": password("keep")}
	var mutex sync.Mutex
	var driven []string
	h.service.options.DriveMachine = func(_ context.Context, execution Execution, _ BlockEvidence, on bool) error {
		mutex.Lock()
		defer mutex.Unlock()
		direction := "off:"
		if on {
			direction = "on:"
		}
		driven = append(driven, direction+presentedMaterial(execution.Material))
		if !on {
			machine.set(Quiescent)
		}
		return nil
	}
	return h, machine, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		taken := driven
		driven = nil
		return taken
	}
}

func (m *keptPowerMachine) removed() []string {
	m.power.Lock()
	defer m.power.Unlock()
	return slices.Clone(m.removals)
}

func allOf(values []string, want string) bool {
	return len(values) != 0 && !slices.ContainsFunc(values, func(value string) bool { return value != want })
}

// B1868: a replacement whole destroy after a restore stops the kept Machine
// with the binding that presents that Machine, the lineage root's, although
// the first step it runs is the re-created cluster, whose restore binding
// names no BMC credential.
func TestAWholeDestroyAfterARestoreStopsAKeptMachineWithItsOwnBinding(t *testing.T) {
	t.Parallel()
	h, machine, driven := keptMachineHarness(t, `{"name":"base"}`, `{"name":"cluster","secretRef":"pull"}`)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	restoreWith(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if err := destroyWith(h); err == nil {
		t.Fatal("a whole destroy whose first removals failed reported success")
	}
	machine.read()
	machine.set(Live)
	if err := destroyWith(h); err != nil {
		t.Fatalf("the replacement destroy: %+v", diagnostics.Of(err))
	}
	if got := driven(); !slices.Equal(got, []string{"off:first"}) {
		t.Fatalf("the replacement drove %v, want one stop presenting the Machine's own version", got)
	}
	if got := machine.read(); !allOf(got, "first") {
		t.Fatalf("the replacement read power presenting %v", got)
	}
}

// B1868: a staged destroy that rebinds the rotated BMC credential moves the
// kept Machine onto the new version, so the restore's start-power reading of
// that Machine, which a changed cluster now runs on, the whole destroy's
// start-power reading and the Machine's removal all present it.
func TestAStagedDestroysRebindReachesTheKeptMachinesPowerThroughTheRestoreAndTheWholeDestroy(t *testing.T) {
	t.Parallel()
	h, machine, _ := keptMachineHarness(t, `{"name":"base"}`, `{"credentialsRef":"bmc","name":"cluster"}`)
	h.capability.access = map[string][]string{"cluster": {"bmc"}}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	h.capability.definitions[1].Dependencies = []string{"base", "node-a"}
	machine.read()
	restoreWith(t, h)
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the restore read the kept Machine's power presenting %v, want the rebound version", got)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the whole destroy read power presenting %v, want the rebound version", got)
	}
	if got := machine.removed(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the Machine's removal presented %v", got)
	}
}

// B1868: a BMC credential rotated after the staged destroy is rebound by the
// restore for the kept Machine it proves off, which records the rebind, so its
// start-power reading, the whole destroy's and the Machine's removal present
// the new version; the restore's own binding is untouched.
func TestARestoreRebindsTheAccessCredentialOfAKeptMachineItProvesOff(t *testing.T) {
	t.Parallel()
	h, machine, _ := keptMachineHarness(t, `{"name":"base"}`, `{"name":"cluster","secretRef":"pull"}`)
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.binder.versions["bmc"] = "ver-2"
	h.capability.definitions[1].Dependencies = []string{"base", "node-a"}
	machine.read()
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound restore: %+v", diagnostics.Of(err))
	}
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the restore read the kept Machine's power presenting %v", got)
	}
	restoring, _ := currentRecord(t, h)
	issued := restoring.Rebound[applied.Bindings[0]]
	if restoring.Restores == "" || issued == "" || len(restoring.Rebound) != 1 || !slices.Contains(restoring.Bindings, issued) || ownBinding(restoring) == issued ||
		h.binder.pins[issued]["bmc"] != "ver-2" || h.binder.pins[ownBinding(restoring)]["bmc"] != "" {
		t.Fatalf("the restore recorded %v and %v", restoring.Bindings, restoring.Rebound)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the whole destroy read power presenting %v", got)
	}
	if got := machine.removed(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the Machine's removal presented %v", got)
	}
	if !slices.Contains(h.binder.released, issued) {
		t.Fatalf("the whole destroy released %v, want the restore's rebind", h.binder.released)
	}
}

// B1868: a staged destroy's rebind replaces the binding that also presents
// the kept steps, so a Secret that is a cluster's and a kept Machine's access
// credential but effect material of the kept base step refuses the rebind,
// naming that kept step.
func TestAStagedRebindRefusesASecretAKeptStepRestsOn(t *testing.T) {
	t.Parallel()
	h, _, _ := keptMachineHarness(t, `{"credentialsRef":"bmc","name":"base"}`, `{"credentialsRef":"bmc","name":"cluster"}`)
	h.capability.access = map[string][]string{"cluster": {"bmc"}}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != authorizeRebindInvalid.Code || !strings.Contains(reported[0].Message, "kept step base (serve base)") {
		t.Fatalf("a staged rebind over a kept step's effect material returned %+v", reported)
	}
	if current, _ := currentRecord(t, h); current.Verb != reconciliation.Apply || len(h.binder.rebinds) != 0 {
		t.Fatalf("a refused rebind left %+v current after %d rebinds", current, len(h.binder.rebinds))
	}
}

// B1868: a binding that only a step taken back again presents is not
// reopened by the whole removal, so losing it refuses nothing.
func TestAWholeDestroyReopensNoBindingOnlyATakenBackStepPresents(t *testing.T) {
	t.Parallel()
	h := restoreBindingHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.binder.lost = []string{restoring.Bindings[0]}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy over a lost binding of a taken-back step: %+v", diagnostics.Of(err))
	}
	if removal, _ := currentRecord(t, h); slices.Contains(removal.Bindings, restoring.Bindings[0]) {
		t.Fatalf("the removal recorded %v", removal.Bindings)
	}
}

// B1868: diff --observe during an unfinished staged destroy presents each
// step it observes the binding that removal's rebind issued.
func TestAnObservedDiffDuringAnUnfinishedRemovalReadsItsRebind(t *testing.T) {
	t.Parallel()
	h := restoreBindingHarness(t)
	compiler := sourcesCompiler{states: map[string]*compilation.State{digestOf(h.workspace.inputs): diffState(servers("v1", "base", "cluster")...)}, errs: map[string]error{}}
	h.service.compiler = compiler
	observer := &presentingObserver{projectingCapability: &projectingCapability{testCapability: h.capability, answers: map[string]scriptedObservation{},
		paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}}, seen: map[string]string{}}
	h.service.capabilities = testResolver{capability: observer}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err == nil {
		t.Fatal("a staged destroy whose removal failed reported success")
	}
	if _, err := h.service.Diff(context.Background(), DiffRequest{ContextName: testContextName, Observe: true}); err != nil {
		t.Fatalf("diff --observe: %+v", diagnostics.Of(err))
	}
	observer.lock.Lock()
	defer observer.lock.Unlock()
	if observed := observer.seen; len(observed) != 1 || observed["cluster"] != "second" {
		t.Fatalf("diff --observe presented %v, want the unfinished step the rebound version", observed)
	}
}

type presentingObserver struct {
	*projectingCapability
	seen map[string]string
}

func (p *presentingObserver) Observe(ctx context.Context, execution Execution) (Observation, error) {
	p.lock.Lock()
	p.seen[execution.Block.ID] = presented(execution)
	p.lock.Unlock()
	return p.projectingCapability.Observe(ctx, execution)
}

type presentedSignOn struct {
	*signOnCapability
	presented []string
}

func (c *presentedSignOn) ResumeSignOn(ctx context.Context, execution Execution) (SignOnProof, error) {
	c.presented = append(c.presented, presented(execution))
	return c.signOnCapability.ResumeSignOn(ctx, execution)
}

// B1868: a sign-on resume of a kept cluster after its staged destroy rebound
// the BMC credential and a restore presents the rebound version.
func TestASignOnOfAKeptClusterReadsTheStagedRebind(t *testing.T) {
	t.Parallel()
	h, capability := signOnHarness(t, false)
	addition := stagedDefinition("edge-addon", reconciliation.StageAddOns, signOnBlock)
	addition.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"edge-addon"}`)
	added := &testCapability{definitions: []reconciliation.BlockDefinition{addition}, secrets: []string{"bmc"}, access: map[string][]string{"edge-addon": {"bmc"}}}
	h.capability.definitions[0].Request = json.RawMessage(`{"credentialsRef":"bmc","name":"edge"}`)
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{signOnBlock: {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	resumed := &presentedSignOn{signOnCapability: capability}
	h.service.capabilities = kindResolver{
		bindings:     []CapabilityBinding{{Kind: "CephCluster", Implementation: addition.Implementation}, {Kind: "ArtifactServer", Implementation: addition.Implementation}},
		capabilities: map[string]Capability{"CephCluster": resumed, "ArtifactServer": added},
	}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	restoreWith(t, h)
	if _, err := h.service.SignOn().Resume(context.Background(), signOnAsked()); err != nil {
		t.Fatalf("the resume: %+v", diagnostics.Of(err))
	}
	if !slices.Equal(resumed.presented, []string{"second"}) {
		t.Fatalf("the resume presented %v, want the rebound version", resumed.presented)
	}
}

// An additive apply over a lineage whose staged destroy rebound the BMC
// credential starts a predecessor cluster node with the rebound version, not
// the one its original apply bound.
func TestAnAdditiveApplyStartsAPredecessorNodeWithTheLineagesRebind(t *testing.T) {
	t.Parallel()
	h, _, driven := keptMachineHarness(t, `{"name":"base"}`, `{"credentialsRef":"bmc","name":"cluster"}`)
	h.capability.access = map[string][]string{"cluster": {"bmc"}}
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	h.capability.definitions[1].Dependencies = []string{"base", "node-a"}
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	driven()
	proof := provedMachine(applied.ID, "node-a", reconciliation.BlockDone)
	proof.Block.Implementation, proof.Implementation = "machine-v1", "machine-v1"
	execution := Execution{Context: testContextName, ApplyProved: []BlockEvidence{proof}}
	run := invocation{operation: restoring, store: h.service.store(h.workspace.view()), clusterStarts: &sync.Map{}}
	if err := h.service.machinePower(run, execution)(context.Background(), "node-a", true); err != nil {
		t.Fatalf("the predecessor node's start: %+v", diagnostics.Of(err))
	}
	if got := driven(); !slices.Equal(got, []string{"on:second"}) {
		t.Fatalf("the additive apply started the node presenting %v, want the rebound version", got)
	}
}

// A restore continued after the BMC credential rotated rebinds it for the kept
// Machine it proves off, which the continued restore records, so the whole
// destroy after it presents the new version.
func TestAContinuedRestoreRebindsTheAccessCredentialOfAKeptMachine(t *testing.T) {
	t.Parallel()
	h, machine, _ := keptMachineHarness(t, `{"name":"base"}`, `{"name":"cluster","secretRef":"pull"}`)
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.capability.definitions[1].Dependencies = []string{"base", "node-a"}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a restore whose added step failed reported success")
	}
	failed, _ := currentRecord(t, h)
	h.binder.versions["bmc"] = "ver-2"
	machine.read()
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound continuation: %+v", diagnostics.Of(err))
	}
	restoring, _ := currentRecord(t, h)
	issued := restoring.Rebound[applied.Bindings[0]]
	if restoring.ID != failed.ID || restoring.Restores == "" || issued == "" || len(restoring.Rebound) != 1 || !slices.Contains(restoring.Bindings, issued) || ownBinding(restoring) != ownBinding(failed) ||
		h.binder.pins[issued]["bmc"] != "ver-2" {
		t.Fatalf("the continuation recorded %v and %v beside %v", restoring.Bindings, restoring.Rebound, failed.Bindings)
	}
	machine.read()
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the whole destroy read power presenting %v", got)
	}
	if got := machine.removed(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the Machine's removal presented %v", got)
	}
}
