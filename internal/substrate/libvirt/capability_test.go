package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

// A Machine on a provider reached over SSH publishes no controller
// reservation, but its emulated BMC's socket is claimed on that provider's
// host Machine and the SSH address and port it is reached at, so its own
// context compares it with that host's other sockets.
func TestAMachineOnAnSSHHostClaimsItsBMCSocketThere(t *testing.T) {
	arm := provider().Spec().Get("libvirt")
	remote := provider(field("libvirt", arm.With("machineRef", api.StringValue("hypervisor")).
		With("bmcEmulationDefaults", arm.Get("bmcEmulationDefaults").With("bindAddress", api.StringValue("192.0.2.5")))))
	catalog := catalogOf(controller(), remoteHost(), remote, networkConfig(), guest("rhel-01"))
	plan, err := NewMachine(nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	if len(plan.Reservations) != 0 {
		t.Fatalf("a Machine on an SSH host published controller reservations: %+v", plan.Reservations)
	}
	if len(plan.SSHReservations) != 1 || plan.SSHReservations[0].Machine != "hypervisor" ||
		plan.SSHReservations[0].Address != "192.0.2.5" || plan.SSHReservations[0].Port != 22 ||
		plan.SSHReservations[0].Reservation.Service != "rhel-01" ||
		!slices.Contains(plan.SSHReservations[0].Reservation.Keys, "socket:192.0.2.5:8000") {
		t.Fatalf("SSH claims = %+v, want rhel-01's BMC socket qualified by the Machine hypervisor at 192.0.2.5:22", plan.SSHReservations)
	}
}

// An emulated BMC bound to the host address of another provider's managed
// bridge on its host cannot listen before that provider's host block creates
// the bridge, so its machine block waits for that block too, and its removal
// runs before the bridge's.
func TestAnEmulatedBMCBoundToABridgeRequiresTheProviderThatCreatesIt(t *testing.T) {
	bound := func(address string) api.Object {
		arm := provider().Spec().Get("libvirt")
		return provider(field("libvirt", arm.With("bmcEmulationDefaults", arm.Get("bmcEmulationDefaults").With("bindAddress", api.StringValue(address)))))
	}
	other := func(host, management, address string) api.Object {
		attachment := api.MapValue(text("bridge", "virbr-storage"), text("management", management))
		if address != "" {
			attachment = attachment.With("address", api.StringValue(address)).With("forward", api.StringValue("nat"))
		}
		spec := provider(
			field("libvirt", provider().Spec().Get("libvirt").With("machineRef", api.StringValue(host))),
			field("networkAttachments", api.ListValue(api.MapValue(text("name", "storage"), field("libvirt", attachment)))),
		).Spec()
		return api.NewObject(api.InfraProvider, "lab-storage", api.Value{}, spec)
	}
	own := reconciliation.ObjectRef{Kind: "InfraProvider", Object: "lab-libvirt"}
	storage := reconciliation.ObjectRef{Kind: "InfraProvider", Object: "lab-storage"}
	for name, test := range map[string]struct {
		bind  string
		other api.Object
		want  []reconciliation.ObjectRef
	}{
		"an address no bridge carries":              {"192.0.2.1", other("controller", "managed", "203.0.113.1/24"), []reconciliation.ObjectRef{own}},
		"its own provider's bridge":                 {"198.51.100.1", other("controller", "managed", "203.0.113.1/24"), []reconciliation.ObjectRef{own}},
		"another provider's bridge on its host":     {"203.0.113.1", other("controller", "managed", "203.0.113.1/24"), []reconciliation.ObjectRef{own, storage}},
		"another provider's IPv6 bridge":            {"fd00:7::1", other("controller", "managed", "fd00:7::1/64"), []reconciliation.ObjectRef{own, storage}},
		"another provider's bridge on another host": {"203.0.113.1", other("hypervisor", "managed", "203.0.113.1/24"), []reconciliation.ObjectRef{own}},
		"another provider's external bridge":        {"203.0.113.1", other("controller", "external", ""), []reconciliation.ObjectRef{own}},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := catalogOf(controller(), remoteHost(), bound(test.bind), test.other, networkConfig(), guest("rhel-01"))
			input := lifecycle.PlanInput{
				Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
				Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
			}
			machines, err := NewMachine(nil).Plan(context.Background(), input)
			if err != nil || len(machines.Definitions) != 1 {
				t.Fatalf("plan = %+v (%v)", machines, err)
			}
			if !slices.Equal(machines.Definitions[0].Requires, test.want) {
				t.Fatalf("requires = %v, want %v", machines.Definitions[0].Requires, test.want)
			}
			hosts, err := NewHost(nil).Plan(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			apply, err := reconciliation.NewPlan(reconciliation.Apply, append(hosts.Definitions, machines.Definitions...))
			if err != nil {
				t.Fatal(err)
			}
			block, _ := apply.Block("machine-rhel-01")
			want := []string{"substrate-host-lab-libvirt"}
			if len(test.want) == 2 {
				want = append(want, "substrate-host-lab-storage")
			}
			if !slices.Equal(block.Dependencies, want) {
				t.Fatalf("machine dependencies = %v, want %v", block.Dependencies, want)
			}
			removal, err := apply.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range want {
				if host, _ := removal.Block(id); !slices.Equal(host.Dependencies, []string{"machine-rhel-01"}) {
					t.Fatalf("removal of %s waits for %v", id, host.Dependencies)
				}
			}
		})
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
		Answered: true, Controller: request.Controller.Image, Disks: disks, Domain: request.Domain, Listener: observed(true),
		Owned: true, Postcondition: true, Power: "Off", Request: digest, System: request.UUID, Unit: "active",
	})
	return data
}

func hostEvidence(request HostRequest, digest string) json.RawMessage {
	networks := make([]NetworkEvidence, 0, len(request.Networks))
	for _, network := range request.Networks {
		entry := NetworkEvidence{Bridge: true, Managed: network.Managed, Name: network.Name}
		if network.Managed {
			entry.Answered, entry.Definition, entry.Owned, entry.State = true, true, true, "active"
		}
		networks = append(networks, entry)
	}
	services := make([]ServiceEvidence, 0, len(request.Services))
	for _, service := range request.Services {
		services = append(services, ServiceEvidence{Enabled: true, Name: service, State: "active"})
	}
	data, _ := json.Marshal(HostEvidence{
		Directory: observed(true), Hypervisor: true, Networks: networks, Pool: "active", PoolAnswered: true,
		Postcondition: true, Request: digest, Services: services, URI: true,
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
	absent, _ := json.Marshal(MachineEvidence{Absent: true, Answered: true, Listener: observed(false), Postcondition: true, Request: "digest"})
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
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	hostCanonical, _ := hosts[0].Canonical()
	host := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "substrate-host-lab-libvirt", Request: hostCanonical},
		RequestDigest:   "digest",
	}}
	for name, test := range map[string]struct {
		evidence HostEvidence
		want     reconciliation.EffectState
	}{
		"host absent":         {HostEvidence{Absent: true, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "digest", URI: true}, reconciliation.EffectNoEffect},
		"host directory only": {HostEvidence{Directory: observed(true), Hypervisor: true, PoolAnswered: true, Request: "digest", URI: true}, reconciliation.EffectPartial},
		"host silent":         {HostEvidence{Directory: observed(false), Hypervisor: true, Request: "digest"}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, test.evidence)}}
			observation, err := NewHost(runner).Observe(context.Background(), host)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, test.want)
			}
		})
	}
}

// A removal's resolution reads the same observation for what the removal
// proves, so a host or machine still realized is a removal that had no effect
// rather than one that completed, and its absence, proved through a
// hypervisor that answered, is the removal's completion. A hypervisor that did
// not answer proves neither, so it stays unknown.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	machines, _ := MachineRequests(labCatalog(), "controller", testContext)
	hostCanonical, _ := hosts[0].Canonical()
	machineCanonical, _ := machines[0].Canonical()
	block := func(id string, canonical []byte) lifecycle.Execution {
		return lifecycle.Execution{Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: id, Request: canonical}, RequestDigest: "digest",
		}}
	}
	encode := func(evidence any) json.RawMessage {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, target := range []struct {
		name      string
		execution lifecycle.Execution
		observe   func(Runner) func(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
		absent    json.RawMessage
		present   json.RawMessage
		partial   json.RawMessage
		silent    json.RawMessage
		foreign   json.RawMessage
	}{
		{
			name: "host", execution: block("substrate-host-lab-libvirt", hostCanonical),
			observe: func(runner Runner) func(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
				return NewHost(runner).ObserveRemoval
			},
			absent:  encode(HostEvidence{Absent: true, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "digest", URI: true}),
			present: hostEvidence(hosts[0], "digest"),
			partial: encode(HostEvidence{Pool: "active", PoolAnswered: true, Request: "digest", URI: true}),
			silent:  encode(HostEvidence{Directory: observed(false), Hypervisor: true, Request: "digest"}),
			foreign: encode(HostEvidence{Absent: true, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "other", URI: true}),
		},
		{
			name: "machine", execution: block("machine-rhel-01", machineCanonical),
			observe: func(runner Runner) func(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
				return NewMachine(runner).ObserveRemoval
			},
			absent:  encode(MachineEvidence{Absent: true, Answered: true, Listener: observed(false), Postcondition: true, Request: "digest"}),
			present: machineEvidence(machines[0], "digest"),
			partial: encode(MachineEvidence{Domain: machines[0].Domain, Owned: true, Request: "digest"}),
			silent:  encode(MachineEvidence{Listener: observed(false), Power: "Off", Request: "digest"}),
			foreign: encode(MachineEvidence{Absent: true, Answered: true, Listener: observed(false), Postcondition: true, Request: "other"}),
		},
	} {
		for name, test := range map[string]struct {
			runner *fakeRunner
			want   reconciliation.EffectState
		}{
			"absent":          {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: target.absent}}, reconciliation.EffectCompleted},
			"present":         {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: target.present}}, reconciliation.EffectNoEffect},
			"partial":         {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: target.partial}}, reconciliation.EffectPartial},
			"silent":          {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: target.silent}}, reconciliation.EffectUnknown},
			"another request": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: target.foreign}}, reconciliation.EffectUnknown},
			"failed":          {&fakeRunner{err: errors.New("unreachable")}, reconciliation.EffectUnknown},
		} {
			t.Run(target.name+"/"+name, func(t *testing.T) {
				observation, err := target.observe(test.runner)(context.Background(), target.execution)
				if err != nil || observation.Effect != test.want {
					t.Fatalf("removal observation = %+v (%v), want %s", observation, err, test.want)
				}
				if len(test.runner.requests) != 1 || test.runner.requests[0].Operation != "observe" {
					t.Fatalf("adapter invocation = %+v", test.runner.requests)
				}
			})
		}
	}
}

// A host removal takes back the owned networks, the pool and its directory and
// nothing else, so a host that still holds all of them, active, is a removal
// that had no effect however far it has drifted from what its apply proves: a
// network running another definition, a driver daemon disabled or stopped, the
// closure or a bridge gone. The apply's resolution still reads the same
// evidence as partial, because the apply converges it. A network or the pool
// stopped may be the removal's own first effect, so it reads partial, and a
// same-named network without this context's ownership stays unknown. The pool
// directory gone is part of what the removal takes back, so it reads partial
// too, although the adapter's postcondition, the apply's, leaves it out and
// the apply reads the same evidence as complete. Each row carries the
// postcondition the adapter publishes for its damage.
func TestAHostRemovalReadsWhatItTakesBackNotWhatTheApplyProves(t *testing.T) {
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	canonical, _ := hosts[0].Canonical()
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "substrate-host-lab-libvirt", Request: canonical},
		RequestDigest:   "digest",
	}}
	for name, test := range map[string]struct {
		damage  func(*HostEvidence)
		removal reconciliation.EffectState
		apply   reconciliation.EffectState
	}{
		"network drifted":   {func(e *HostEvidence) { e.Networks[0].Definition = false }, reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		"daemon disabled":   {func(e *HostEvidence) { e.Services[0].Enabled = false }, reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		"daemon stopped":    {func(e *HostEvidence) { e.Services[0].State = "inactive" }, reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		"closure missing":   {func(e *HostEvidence) { e.Hypervisor = false }, reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		"bridge missing":    {func(e *HostEvidence) { e.Networks[0].Bridge = false }, reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		"network stopped":   {func(e *HostEvidence) { e.Networks[0].State = "inactive" }, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"network undefined": {func(e *HostEvidence) { e.Networks[0].State, e.Networks[0].Owned = "", false }, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"pool stopped":      {func(e *HostEvidence) { e.Pool = "inactive" }, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"directory gone":    {func(e *HostEvidence) { e.Directory = observed(false) }, reconciliation.EffectPartial, reconciliation.EffectCompleted},
		"network foreign":   {func(e *HostEvidence) { e.Networks[0].Owned = false }, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			var evidence HostEvidence
			if err := json.Unmarshal(hostEvidence(hosts[0], "digest"), &evidence); err != nil {
				t.Fatal(err)
			}
			test.damage(&evidence)
			evidence.Postcondition = publishedPostcondition(evidence)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, evidence)}}
			for verb, resolution := range map[string]struct {
				observe func(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
				want    reconciliation.EffectState
			}{
				"destroy": {NewHost(runner).ObserveRemoval, test.removal},
				"apply":   {NewHost(runner).Observe, test.apply},
			} {
				observation, err := resolution.observe(context.Background(), execution)
				if err != nil || observation.Effect != resolution.want {
					t.Fatalf("the %s resolution read %s (%v), want %s", verb, observation.Effect, err, resolution.want)
				}
			}
		})
	}
}

// publishedPostcondition is the postcondition the adapter's presence form
// carries, as presence() in
// ansible/collections/ansible_collections/bootwright/core/plugins/action/substrate_host_protocol.py
// computes it from an observation: the closure, the URI, the pool answered for
// and active, every driver daemon active and enabled, and every declared
// bridge present with each managed network answered for, owned, active and
// carrying its frozen definition. The pool directory plays no part in it.
func publishedPostcondition(evidence HostEvidence) bool {
	complete := evidence.Hypervisor && evidence.URI && evidence.PoolAnswered && evidence.Pool == "active"
	for _, service := range evidence.Services {
		complete = complete && service.State == "active" && service.Enabled
	}
	for _, network := range evidence.Networks {
		realized := network.Answered && network.Owned && network.State == "active" && network.Definition
		complete = complete && network.Bridge && (!network.Managed || realized)
	}
	return complete
}

// A machine removal takes back the controller unit and its container, the
// domain and its disks, so a machine that still holds all of them, its unit
// active, is a removal that had no effect however far it has drifted from what
// its apply proves: a controller on another image, an emulated BMC whose
// ComputerSystem reports no power state, a domain with another UUID, a disk at
// another size or nothing listening on the socket. The removal stops the unit
// first, so a unit that is not active may be its first effect and reads
// partial, as any part gone does; a hypervisor that did not answer never reads
// no effect, and a same-named domain without this context's ownership stays
// unknown. Each row carries the postcondition the adapter publishes for its
// damage.
func TestAMachineRemovalReadsWhatItTakesBackNotWhatTheApplyProves(t *testing.T) {
	machines, _ := MachineRequests(labCatalog(), "controller", testContext)
	canonical, _ := machines[0].Canonical()
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical},
		RequestDigest:   "digest",
	}}
	for name, test := range map[string]struct {
		damage func(*MachineEvidence)
		want   reconciliation.EffectState
	}{
		"image drifted":     {func(e *MachineEvidence) { e.Controller = "docker.io/other@sha256:0" }, reconciliation.EffectNoEffect},
		"bmc silent":        {func(e *MachineEvidence) { e.Power = "" }, reconciliation.EffectNoEffect},
		"another system":    {func(e *MachineEvidence) { e.System = "00000000-0000-0000-0000-000000000000" }, reconciliation.EffectNoEffect},
		"disk resized":      {func(e *MachineEvidence) { e.Disks[0].SizeGiB += 10 }, reconciliation.EffectNoEffect},
		"socket free":       {func(e *MachineEvidence) { e.Listener = observed(false) }, reconciliation.EffectNoEffect},
		"unit stopped":      {func(e *MachineEvidence) { e.Unit = "inactive" }, reconciliation.EffectPartial},
		"container gone":    {func(e *MachineEvidence) { e.Controller = "" }, reconciliation.EffectPartial},
		"unit gone":         {func(e *MachineEvidence) { e.Unit, e.Controller, e.Power = "", "", "" }, reconciliation.EffectPartial},
		"domain undefined":  {func(e *MachineEvidence) { e.Domain, e.Owned, e.System = "", false, "" }, reconciliation.EffectPartial},
		"disk deleted":      {func(e *MachineEvidence) { e.Disks[0].Present, e.Disks[0].SizeGiB = false, 0 }, reconciliation.EffectPartial},
		"hypervisor silent": {func(e *MachineEvidence) { e.Answered, e.Domain, e.Owned, e.System = false, "", false, "" }, reconciliation.EffectPartial},
		"domain foreign":    {func(e *MachineEvidence) { e.Owned = false }, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			var evidence MachineEvidence
			if err := json.Unmarshal(machineEvidence(machines[0], "digest"), &evidence); err != nil {
				t.Fatal(err)
			}
			test.damage(&evidence)
			evidence.Postcondition = publishedMachinePostcondition(evidence)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, evidence)}}
			observation, err := NewMachine(runner).ObserveRemoval(context.Background(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("the removal's resolution read %s (%v), want %s", observation.Effect, err, test.want)
			}
		})
	}
}

// publishedMachinePostcondition is the postcondition the adapter's presence
// form carries, as presence() in
// ansible/collections/ansible_collections/bootwright/core/plugins/action/substrate_machine_protocol.py
// computes it from an observation: the domain owned, its controller unit
// active, a controller image, a system and a power state reported, and at
// least one disk with every disk present. The image, the system and each
// disk's size are never compared with the frozen request there.
func publishedMachinePostcondition(evidence MachineEvidence) bool {
	complete := evidence.Domain != "" && evidence.Owned && evidence.Unit == "active" && evidence.Controller != "" &&
		evidence.System != "" && evidence.Power != "" && len(evidence.Disks) > 0
	for _, disk := range evidence.Disks {
		complete = complete && disk.Present
	}
	return complete
}

// silentDrivers is the absence form an adapter publishes over a host whose
// hypervisor answered while the driver that owns a managed network or the pool
// did not: each reports no state and no ownership, exactly as one that driver
// removed would, and only its answer tells the two apart.
func silentDrivers(request HostRequest, network, pool bool) HostEvidence {
	networks := make([]NetworkEvidence, 0, len(request.Networks))
	for _, entry := range request.Networks {
		networks = append(networks, NetworkEvidence{Answered: entry.Managed && !network, Bridge: true, Managed: entry.Managed, Name: entry.Name})
	}
	services := make([]ServiceEvidence, 0, len(request.Services))
	for _, service := range request.Services {
		services = append(services, ServiceEvidence{Enabled: true, Name: service, State: "inactive"})
	}
	return HostEvidence{
		Absent: true, Directory: observed(false), Hypervisor: true, Networks: networks, PoolAnswered: !pool,
		Postcondition: true, Request: "digest", Services: services, URI: true,
	}
}

// The URI answering proves only that the hypervisor driver did. A managed
// network and the pool each live in a driver of their own, and one that is
// silent reports them as one that removed them does, so neither a removal nor
// either resolution reads that silence as the network or pool gone.
func TestAHostItsDriversDidNotAnswerForProvesNoRemoval(t *testing.T) {
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	canonical, _ := hosts[0].Canonical()
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "substrate-host-lab-libvirt", Request: canonical},
		RequestDigest:   "digest",
	}}
	if err := ValidateHostAbsence(encode(t, silentDrivers(hosts[0], false, false)), "digest"); err != nil {
		t.Fatalf("the absence both drivers answered for was refused: %v", err)
	}
	for name, evidence := range map[string]HostEvidence{
		"network driver silent": silentDrivers(hosts[0], true, false),
		"storage driver silent": silentDrivers(hosts[0], false, true),
	} {
		t.Run(name, func(t *testing.T) {
			data := encode(t, evidence)
			if err := ValidateHostAbsence(data, "digest"); err == nil {
				t.Fatal("removal evidence a driver never answered for was accepted")
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: data}}
			if result, err := NewHost(runner).Destroy(context.Background(), execution); err == nil {
				t.Fatalf("a removal a driver never answered for resolved %s", result.Outcome)
			}
			for verb, observe := range map[string]func(context.Context, lifecycle.Execution) (lifecycle.Observation, error){
				"apply": NewHost(runner).Observe, "destroy": NewHost(runner).ObserveRemoval,
			} {
				observation, err := observe(context.Background(), execution)
				if err != nil || observation.Effect != reconciliation.EffectUnknown {
					t.Fatalf("the %s resolution read %s (%v), want unknown", verb, observation.Effect, err)
				}
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

// Removing a machine takes its memory and disks out from under whatever is
// using them, so only a domain the hypervisor reports shut off admits removal.
// A paused or suspended domain still holds both. A hypervisor that did not
// answer reports no domain either, so then only the controller's power state
// decides, whatever the domain fields say.
func TestMachineQuiescenceAdmitsOnlyAShutOffDomain(t *testing.T) {
	requests, _ := MachineRequests(labCatalog(), "controller", testContext)
	request := requests[0]
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	probe := lifecycle.Probe{Context: "lab-b", Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical},
		RequestDigest:   "digest",
	}}
	for name, tc := range map[string]struct {
		evidence MachineEvidence
		want     string
	}{
		"shut off":         {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, State: "shut off"}, lifecycle.Quiescent},
		"never defined":    {MachineEvidence{Request: "digest", Answered: true}, lifecycle.Quiescent},
		"controller off":   {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, Power: "Off"}, lifecycle.Quiescent},
		"running":          {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, State: "running"}, lifecycle.Live},
		"paused":           {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, State: "paused"}, lifecycle.Live},
		"suspended":        {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, State: "pmsuspended"}, lifecycle.Live},
		"controller on":    {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, Power: "On"}, lifecycle.Live},
		"nothing readable": {MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain}, lifecycle.Unproved},
		"silent, off":      {MachineEvidence{Request: "digest", Power: "Off"}, lifecycle.Quiescent},
		"silent, on":       {MachineEvidence{Request: "digest", Power: "On"}, lifecycle.Live},
		"silent, unknown":  {MachineEvidence{Request: "digest"}, lifecycle.Unproved},
		"silent, shut off": {MachineEvidence{Request: "digest", Domain: request.Domain, State: "shut off", Power: "On"}, lifecycle.Live},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, tc.evidence)}}
			state, err := NewMachine(runner).Quiescent(context.Background(), probe)
			if err != nil || state.State != tc.want {
				t.Fatalf("quiescence = %+v (%v), want %s", state, err, tc.want)
			}
			if tc.want == lifecycle.Quiescent {
				return
			}
			if state.Stop != "bootwright machine stop --context lab-b --name "+request.Identity.Object {
				t.Fatalf("a machine that is not idle did not name the command that stops it: %q", state.Stop)
			}
		})
	}
	runner := &fakeRunner{err: errors.New("unreachable")}
	state, err := NewMachine(runner).Quiescent(context.Background(), probe)
	if err != nil || state.State != lifecycle.Unproved {
		t.Fatalf("an unreachable host = %+v (%v), want unproved", state, err)
	}
}

// A provider host is quiescent whichever way its networks are being used: what
// a removal has to prove idle is the Machines, and the same removal probes
// every one of them.
func TestHostQuiescenceIsDerivedFromItsMachines(t *testing.T) {
	runner := &fakeRunner{err: errors.New("the host must not be reached")}
	state, err := NewHost(runner).Quiescent(context.Background(), lifecycle.Probe{})
	if err != nil || state.State != lifecycle.Quiescent {
		t.Fatalf("provider host quiescence = %+v (%v)", state, err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("a derived quiescence ran %d adapter invocations", len(runner.requests))
	}
}
