package baremetal

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for index := 0; index < len(kv); index += 2 {
		var value api.Value
		switch typed := kv[index+1].(type) {
		case api.Value:
			value = typed
		case string:
			value = api.StringValue(typed)
		case bool:
			value = api.BoolValue(typed)
		}
		fields = append(fields, api.FieldValue{Name: kv[index].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func obj(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}

func list(values ...api.Value) api.Value { return api.ListValue(values...) }

// catalogOf is one bare-metal provider with one server on it, reached from the
// Environment's controller Machine.
func catalogOf() api.Catalog {
	return api.NewCatalog([]api.Object{
		obj(api.Environment, "env", m("controller", m("machineRef", "controller"))),
		obj(api.Machine, "controller", m("os", m("provided", true), "access", m("local", true))),
		obj(api.InfraProvider, "floor", m("baremetal", m("boot", m("method", "redfishVirtualMedia")))),
		obj(api.Machine, "server", m(
			"substrate", m("providerRef", "floor"),
			"os", m("provided", false, "installProfileRef", "rhel", "install",
				m("hostKeyRef", "server-host-key", "rootDeviceHints", m("deviceName", "/dev/sda"))),
			"hardware", m(
				"nics", list(m("name", "eno2", "macAddress", "AA:BB:CC:DD:EE:02"),
					m("name", "eno1", "macAddress", "aa:bb:cc:dd:ee:01")),
				"management", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1",
					"credentialsRef", "server-bmc"))))),
	})
}

func planOf(t *testing.T, verb reconciliation.Verb) lifecycle.CapabilityPlan {
	t.Helper()
	catalog := catalogOf()
	state := compilation.NewState(catalog, catalog, nil)
	plan, err := NewMachine(nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: verb, Context: lifecycle.ContextIdentity{Name: "lab"}, State: state, Controller: "controller",
	})
	if err != nil {
		t.Fatalf("planning: %v", diagnostics.Of(err))
	}
	return plan
}

// One block per physical Machine, in the machines stage, claiming exactly the
// machine it names and nothing else on the host.
func TestOneBlockClaimsExactlyTheMachineItNames(t *testing.T) {
	plan := planOf(t, reconciliation.Apply)
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d", len(plan.Definitions))
	}
	block := plan.Definitions[0]
	if block.ID != "machine-server" || block.Stage != reconciliation.StageMachines {
		t.Fatalf("block = %+v", block)
	}
	if block.Kind != Kind || block.Implementation != Implementation {
		t.Fatalf("block resolves as %s/%s", block.Kind, block.Implementation)
	}
	if len(plan.Reservations) != 1 {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
	if keys := plan.Reservations[0].Keys; !slices.Equal(keys, []string{"bmc:bmc.example.test:443/1"}) {
		t.Fatalf("keys = %v", keys)
	}
	if !slices.Equal(plan.Secrets, []string{"server-bmc"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
}

// An apply claims the machine and says so; a removal takes back only that
// claim, so it consumes no authorization and lists no impact at all.
func TestARemovalRetainsTheMachineAndChangesNothing(t *testing.T) {
	apply := planOf(t, reconciliation.Apply).Definitions[0]
	if len(apply.Impacts) != 1 || apply.Consumes != nil {
		t.Fatalf("apply = %+v", apply)
	}
	destroy := planOf(t, reconciliation.Destroy).Definitions[0]
	if len(destroy.Impacts) != 0 {
		t.Fatalf("a removal that retains the machine lists impacts: %v", destroy.Impacts)
	}
	if len(destroy.Consumes) != 0 {
		t.Fatalf("a removal that destroys nothing consumes %v", destroy.Consumes)
	}
	if destroy.Description != "release the claim on server and retain the machine" {
		t.Fatalf("description = %q", destroy.Description)
	}
}

// The frozen request carries every declared address in one settled order, so
// two compilations of one revision freeze the same request.
func TestTheFrozenRequestCarriesEveryDeclaredAddress(t *testing.T) {
	var request Request
	if err := json.Unmarshal(planOf(t, reconciliation.Apply).Definitions[0].Request, &request); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(request.Addresses(), []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"}) {
		t.Fatalf("addresses = %v", request.Addresses())
	}
	if !request.Placement.Local() {
		t.Fatal("a physical machine is reached from the controller")
	}
	if request.Controller.Endpoint != "https://bmc.example.test/redfish/v1/Systems/1" {
		t.Fatalf("endpoint = %q", request.Controller.Endpoint)
	}
}

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureRequest(t *testing.T) Request {
	t.Helper()
	var request Request
	if err := json.Unmarshal(planOf(t, reconciliation.Apply).Definitions[0].Request, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

// Completion requires the exact machine: its identity recorded, a power state
// read, and every declared address present. Anything less proves nothing,
// because a partial inventory cannot tell one server from another.
func TestCompletionRequiresTheExactMachine(t *testing.T) {
	request := fixtureRequest(t)
	complete := Evidence{
		Addresses:     []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:09"},
		Postcondition: true, Power: "Off", Request: "digest", Serial: "SN1", UUID: "uuid-1",
	}
	if err := ValidatePresence(encode(t, complete), request, "digest"); err != nil {
		t.Fatalf("a proved machine was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"one address missing": {Addresses: []string{"aa:bb:cc:dd:ee:01"}, Postcondition: true, Power: "Off", Request: "digest", UUID: "uuid-1"},
		"no inventory":        {Postcondition: true, Power: "Off", Request: "digest", UUID: "uuid-1"},
		"no identity":         {Addresses: request.Addresses(), Postcondition: true, Power: "Off", Request: "digest"},
		"no power state":      {Addresses: request.Addresses(), Postcondition: true, Request: "digest", UUID: "uuid-1"},
		"unproved":            {Addresses: request.Addresses(), Power: "Off", Request: "digest", UUID: "uuid-1"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePresence(encode(t, evidence), request, "digest"); err == nil {
				t.Fatal("evidence that proves no machine was accepted")
			}
		})
	}
}

// A removal proves it took back only its claim, so it reports nothing about
// the machine. Evidence that still describes one has not proved a retention.
func TestRemovalEvidenceReportsNothingAboutTheMachine(t *testing.T) {
	if err := ValidateAbsence(encode(t, Evidence{Absent: true, Postcondition: true, Request: "digest"}), "digest"); err != nil {
		t.Fatalf("a retaining removal was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"still reports addresses": {Absent: true, Addresses: []string{"aa:bb:cc:dd:ee:01"}, Postcondition: true, Request: "digest"},
		"still reports identity":  {Absent: true, Postcondition: true, Request: "digest", UUID: "uuid-1"},
		"not absent":              {Postcondition: true, Request: "digest"},
		"unproved":                {Absent: true, Request: "digest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("evidence that proves no retention was accepted")
			}
		})
	}
}

// Evidence that names another request proves nothing about this one.
func TestEvidenceMustNameItsOwnRequest(t *testing.T) {
	request := fixtureRequest(t)
	evidence := Evidence{Addresses: request.Addresses(), Postcondition: true, Power: "Off", Request: "another", UUID: "u"}
	if err := ValidatePresence(encode(t, evidence), request, "digest"); err == nil {
		t.Fatal("evidence naming another request was accepted")
	}
}

type scriptedRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *scriptedRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

// A removal releases only the claim and never contacts the machine, so its
// resolution runs nothing: whatever the machine would answer, it is the
// removal's completion, with the evidence the removal publishes. Only a
// request it cannot read, or a resolution already cancelled, stays unknown.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, planOf(t, reconciliation.Apply).Definitions)
	if err != nil {
		t.Fatal(err)
	}
	call := lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0]}
	digest := call.Block.RequestDigest
	addresses := fixtureRequest(t).Addresses()
	for name, runner := range map[string]*scriptedRunner{
		"the machine answering": {result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, Evidence{
			Addresses: addresses, Postcondition: true, Power: "On", Request: digest, UUID: "uuid-1",
		})}},
		"the machine unproved": {result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, Evidence{
			Addresses: addresses, Power: "On", Request: digest, UUID: "uuid-1",
		})}},
		"the claim released": {result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, Evidence{
			Absent: true, Addresses: []string{}, Postcondition: true, Request: digest,
		})}},
		"another request": {result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, Evidence{
			Absent: true, Addresses: []string{}, Postcondition: true, Request: "another",
		})}},
		"the adapter failing": {err: errors.New("unreachable")},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := NewMachine(runner).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != reconciliation.EffectCompleted {
				t.Fatalf("removal observation = %+v (%v), want completed", observation, err)
			}
			if len(runner.requests) != 0 {
				t.Fatalf("a removal's resolution contacted the machine: %+v", runner.requests)
			}
			if err := ValidateAbsence(observation.Evidence, digest); err != nil {
				t.Fatalf("the resolution's evidence %s is not what the removal publishes: %v", observation.Evidence, err)
			}
			if ValidateAbsence(observation.Evidence, "another") == nil {
				t.Fatal("the resolution's evidence proves another request")
			}
		})
	}
	malformed := call
	malformed.Block.Request = json.RawMessage(`{}`)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, refused := range map[string]struct {
		ctx       context.Context
		runner    Runner
		execution lifecycle.Execution
	}{
		"a malformed request":    {context.Background(), &scriptedRunner{}, malformed},
		"a cancelled resolution": {cancelled, &scriptedRunner{}, call},
		"no adapter configured":  {context.Background(), nil, call},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := NewMachine(refused.runner).ObserveRemoval(refused.ctx, refused.execution)
			if err == nil || observation.Effect != reconciliation.EffectUnknown {
				t.Fatalf("removal observation = %+v (%v), want unknown with its reason", observation, err)
			}
		})
	}
}

// The removal takes back a claim nothing reads and leaves the machine running
// exactly as it was, so it can never interrupt anything.
func TestARemovalHereInterruptsNothing(t *testing.T) {
	quiescence, err := NewMachine(nil).Quiescent(context.Background(), lifecycle.Probe{})
	if err != nil || !quiescence.Settled() {
		t.Fatalf("quiescence = %+v, err = %v", quiescence, err)
	}
	if quiescence.Stop != "" {
		t.Fatal("nothing has to be stopped for a removal that retains the machine")
	}
}
