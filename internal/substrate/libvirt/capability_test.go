package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type fakeRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *fakeRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

func planInput(verb reconciliation.Verb) lifecycle.PlanInput {
	catalog := labCatalog()
	return lifecycle.PlanInput{
		Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	}
}

func TestHostPlanContributesOneSubstrateBlockPerProvider(t *testing.T) {
	plan, err := NewHost(nil).Plan(context.Background(), planInput(reconciliation.Apply))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "substrate-host-lab-libvirt" || definition.Stage != reconciliation.StageSubstrates {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.Kind != HostKind || definition.Implementation != HostImplementation {
		t.Fatalf("identity = %s/%s", definition.Kind, definition.Implementation)
	}
	if len(definition.Requires) != 0 || len(definition.Consumes) != 0 {
		t.Fatal("the provider host waits for or consumes something it should not", definition)
	}
	if len(plan.Reservations) != 1 || plan.Reservations[0].Context != testContext {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions); err != nil {
		t.Fatal("the provider host produced a block the plan model refuses:", err)
	}
}

// A machine block names the API object it waits for, never that object's block
// identity, so the engine alone knows which block realizes a provider.
func TestMachinePlanRequiresItsProviderByObject(t *testing.T) {
	plan, err := NewMachine(nil).Plan(context.Background(), planInput(reconciliation.Apply))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "machine-rhel-01" || definition.Stage != reconciliation.StageMachines {
		t.Fatalf("definition = %+v", definition)
	}
	want := []reconciliation.ObjectRef{{Kind: "InfraProvider", Object: "lab-libvirt"}}
	if !slices.Equal(definition.Requires, want) {
		t.Fatalf("requires = %v", definition.Requires)
	}
	if !slices.Equal(plan.Secrets, []string{"lab-bmc-credentials"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
}

// The disks a destroy deletes may hold an installed operating system, so the
// removal is acknowledged before the plan registers. An apply creates them and
// consumes nothing.
func TestOnlyTheMachineDestroyConsumesDataLoss(t *testing.T) {
	apply, _ := NewMachine(nil).Plan(context.Background(), planInput(reconciliation.Apply))
	destroy, _ := NewMachine(nil).Plan(context.Background(), planInput(reconciliation.Destroy))
	if len(apply.Definitions[0].Consumes) != 0 {
		t.Fatalf("apply consumes %v", apply.Definitions[0].Consumes)
	}
	if !slices.Equal(destroy.Definitions[0].Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("destroy consumes %v", destroy.Definitions[0].Consumes)
	}
}

// Both plans must reach the runner with the frozen request, its digest and no
// secret value: bound material is named, never carried.
func TestEveryInvocationCarriesTheFrozenRequestAndNoMaterial(t *testing.T) {
	requests, _ := MachineRequests(labCatalog(), "controller", testContext)
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: machineEvidence(requests[0], "digest")}}
	execution := lifecycle.Execution{
		Block:    reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical}, RequestDigest: "digest"},
		Material: map[string]secrets.Material{"lab-bmc-credentials": secrets.NewMaterial(nil)},
	}
	if _, err := NewMachine(runner).Apply(context.Background(), execution); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("invocations = %d", len(runner.requests))
	}
	invocation := runner.requests[0]
	if invocation.Implementation != MachineImplementation || invocation.Operation != "apply" || invocation.Digest != "digest" {
		t.Fatalf("invocation = %+v", invocation)
	}
	names := []string{}
	for _, file := range invocation.Materials {
		names = append(names, file.Name)
		if file.Secret != "lab-bmc-credentials" {
			t.Fatalf("material file %q names %q", file.Name, file.Secret)
		}
	}
	if !slices.Equal(names, []string{"bmc-user", "bmc-password"}) {
		t.Fatalf("material files = %v", names)
	}
}

// machineEvidence is what a correct adapter returns for the frozen request.
func machineEvidence(request MachineRequest, digest string) json.RawMessage {
	disks := make([]DiskEvidence, 0, len(request.Disks))
	for _, disk := range request.Disks {
		disks = append(disks, DiskEvidence{Name: disk.Name, Present: true, SizeGiB: disk.SizeGiB})
	}
	data, _ := json.Marshal(MachineEvidence{
		Controller: request.Controller.Image, Disks: disks, Domain: request.Domain, Owned: true,
		Postcondition: true, Power: "Off", Request: digest, System: request.UUID, Unit: "active",
	})
	return data
}

func hostEvidence(request HostRequest, digest string) json.RawMessage {
	networks := make([]NetworkEvidence, 0, len(request.Networks))
	for _, network := range request.Networks {
		entry := NetworkEvidence{Bridge: true, Managed: network.Managed, Name: network.Name}
		if network.Managed {
			entry.Owned, entry.State = true, "active"
		}
		networks = append(networks, entry)
	}
	data, _ := json.Marshal(HostEvidence{
		Hypervisor: true, Networks: networks, Pool: "active", Postcondition: true,
		Request: digest, Service: "active", URI: true,
	})
	return data
}

func TestApplyAcceptsOnlyEvidenceThatProvesTheFrozenRequest(t *testing.T) {
	requests, _ := HostRequests(labCatalog(), "controller", testContext)
	canonical, _ := requests[0].Canonical()
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "substrate-host-lab-libvirt", Request: canonical},
		RequestDigest:   "digest",
	}}
	complete := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: hostEvidence(requests[0], "digest")}}
	result, err := NewHost(complete).Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	for name, runner := range map[string]*fakeRunner{
		"another request": {result: lifecycle.RunResult{Outcome: "changed", Evidence: hostEvidence(requests[0], "other")}},
		"no outcome":      {result: lifecycle.RunResult{Outcome: "", Evidence: hostEvidence(requests[0], "digest")}},
		"no evidence":     {result: lifecycle.RunResult{Outcome: "changed"}},
		"adapter failed":  {err: errors.New("unreachable")},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := NewHost(runner).Apply(context.Background(), execution)
			if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("result = %+v (%v)", result, err)
			}
		})
	}
}

// An observation proves what it reads and nothing more: a failed observation is
// unknown without an error, because it is read-only either way.
func TestObservationMapsEvidenceToTheEffectItProves(t *testing.T) {
	requests, _ := MachineRequests(labCatalog(), "controller", testContext)
	canonical, _ := requests[0].Canonical()
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical},
		RequestDigest:   "digest",
	}}
	absent, _ := json.Marshal(MachineEvidence{Absent: true, Postcondition: true, Request: "digest"})
	partial, _ := json.Marshal(MachineEvidence{Domain: requests[0].Domain, Owned: true, Postcondition: true, Request: "digest"})
	for name, test := range map[string]struct {
		runner *fakeRunner
		want   reconciliation.EffectState
	}{
		"complete": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: machineEvidence(requests[0], "digest")}}, reconciliation.EffectCompleted},
		"absent":   {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: absent}}, reconciliation.EffectNoEffect},
		"partial":  {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: partial}}, reconciliation.EffectUnknown},
		"failed":   {&fakeRunner{err: errors.New("unreachable")}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := NewMachine(test.runner).Observe(context.Background(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v (%v)", observation, err)
			}
		})
	}
}

func TestAnUnconfiguredCapabilityRefusesBeforeAnyEffect(t *testing.T) {
	execution := lifecycle.Execution{Block: reconciliation.Block{RequestDigest: "digest"}}
	if _, err := NewHost(nil).Apply(context.Background(), execution); err == nil {
		t.Fatal("an unconfigured provider host capability ran")
	}
	if _, err := NewMachine(nil).Apply(context.Background(), execution); err == nil {
		t.Fatal("an unconfigured machine capability ran")
	}
}

func TestUnsupportedReadsTheCompiledStateOrNothing(t *testing.T) {
	if unsupported := NewHost(nil).Unsupported(nil); unsupported != nil {
		t.Fatalf("unsupported without state = %v", unsupported)
	}
	catalog := labCatalog()
	if unsupported := NewHost(nil).Unsupported(compilation.NewState(catalog, catalog, nil)); len(unsupported) != 0 {
		t.Fatalf("the lab graph reported %v", unsupported)
	}
}
