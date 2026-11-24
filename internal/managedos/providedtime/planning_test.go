package providedtime

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func named(kind api.Kind, name string, members ...api.FieldValue) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(members...))
}

func selection(members ...api.FieldValue) api.Value { return api.MapValue(members...) }

func witness(selections ...api.Value) api.Object {
	return named(api.Machine, "witness",
		field("os", api.MapValue(field("provided", api.MapValue(field("ntp", api.ListValue(selections...)))))),
		field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ssh"), text("address", "192.0.2.234")))))),
		field("access", api.MapValue(
			field("ssh", api.MapValue(text("addressRef", "ssh"), field("port", api.IntegerValue("2222")),
				field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))))),
			field("lifecycle", api.MapValue(text("user", "maintenance"), text("privilege", "sudo"),
				text("privateKeyRef", "update-key"), text("knownHostsRef", "update-pin"))),
		)))
}

func timeInput(verb reconciliation.Verb, machines ...api.Object) lifecycle.PlanInput {
	servers := []api.Object{
		named(api.NTPServer, "pool", text("management", "external"), text("address", "192.0.2.123")),
		named(api.NTPServer, "clock", text("management", "managed"), text("machineRef", "services"),
			field("endpoints", api.ListValue(api.MapValue(text("name", "lan"), text("addressRef", "lan"))))),
		named(api.Machine, "services", field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "lan"), text("address", "192.0.2.50"))))))),
	}
	catalog := api.NewCatalog(append(servers, machines...))
	return lifecycle.PlanInput{Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: "example"}}
}

func external() api.Value { return selection(text("serverRef", "pool")) }

func managed() api.Value { return selection(text("serverRef", "clock"), text("endpointRef", "lan")) }

func TestProvidedTimePlansTheFrozenAddressesInOrderForAnExternalAndAManagedServer(t *testing.T) {
	capability := New(nil)
	input := timeInput(reconciliation.Apply, witness(managed(), external()))
	plan, err := capability.Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("time definitions = %d, want one", len(plan.Definitions))
	}
	definition := plan.Definitions[0]
	requires := []reconciliation.ObjectRef{
		{Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{Kind: string(api.NTPServer), Object: "clock"},
	}
	if definition.ID != "os-time-witness" || definition.Stage != reconciliation.StageMachines ||
		definition.Family != machine.ProvidedTimeFamily || !slices.Equal(definition.Requires, requires) {
		t.Fatalf("time definition = %+v", definition)
	}
	if definition.Description != "synchronize the clock of witness with 192.0.2.50, 192.0.2.123" ||
		!slices.Equal(definition.Impacts, []string{"point the chrony time sources of witness at 192.0.2.50, 192.0.2.123 and prove its clock synchronized"}) {
		t.Fatalf("time description = %q %v", definition.Description, definition.Impacts)
	}
	block := reconciliation.Block{BlockDefinition: definition}
	request, err := decodeBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement := machine.Placement{Connection: machine.ConnectionSSH, Machine: "witness", Address: "192.0.2.234",
		Port: 2222, User: "maintenance", Privilege: "sudo", PrivateKeyRef: "update-key", KnownHostsRef: "update-pin"}
	if !slices.Equal(request.Servers, []string{"192.0.2.50", "192.0.2.123"}) || request.Identity.Context != input.Context.Name || request.Placement != wantPlacement {
		t.Fatalf("frozen time request = %+v", request)
	}
	if want := []string{"update-key", "update-pin"}; !slices.Equal(plan.Secrets, want) {
		t.Fatalf("time binding secrets = %v, want %v", plan.Secrets, want)
	}
	classes, err := capability.SecretClasses(block)
	if err != nil || classes.Of("update-key") != secrets.AccessCredential || classes.Of("update-pin") != secrets.TrustAnchor {
		t.Fatalf("time secret classes = %+v, %v", classes, err)
	}
	if claimed := capability.Claimed(input.State.Effective()); !slices.Equal(claimed, []string{"Machine/witness"}) {
		t.Fatalf("claimed Machines = %v", claimed)
	}
}

func TestProvidedTimeAnExternalOnlyListWaitsForNoService(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), timeInput(reconciliation.Apply, witness(external())))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if len(plan.Definitions[0].Requires) != 1 {
		t.Fatalf("requires = %v, want only the hardware claim", plan.Definitions[0].Requires)
	}
}

func TestProvidedTimeDestroyRetainsTheTimeConfiguration(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), timeInput(reconciliation.Destroy, witness(external())))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v, %v, want one time block", plan, err)
	}
	if definition := plan.Definitions[0]; definition.Description != "retain the time synchronization of witness" || len(definition.Impacts) != 0 {
		t.Fatalf("destroy definition = %q %v", definition.Description, definition.Impacts)
	}
}

func TestProvidedTimeDoesNotPlanWithoutServersOrDuringPreparation(t *testing.T) {
	preparation := timeInput(reconciliation.Apply, witness(external()))
	preparation.Preparation = &reconciliation.PreparationScope{Kind: machine.EnrollmentFamily, Targets: []string{"witness"}}
	controller := timeInput(reconciliation.Apply, witness(external()))
	controller.Controller = "witness"
	for name, input := range map[string]lifecycle.PlanInput{
		"enrollment preparation": preparation,
		"controller Machine":     controller,
		"empty list":             timeInput(reconciliation.Apply, witness()),
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := New(nil).Plan(t.Context(), input)
			if err != nil || len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
				t.Fatalf("plan = %+v, %v, want no time block or credentials", plan, err)
			}
		})
	}
}

func TestProvidedTimeRefusesAServerTheStateDoesNotHold(t *testing.T) {
	if _, err := New(nil).Plan(t.Context(), timeInput(reconciliation.Apply, witness(selection(text("serverRef", "absent"))))); err == nil {
		t.Fatal("an unknown server planned")
	}
}

func TestProvidedTimeRefusesAFrozenRequestWithoutValidServers(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), timeInput(reconciliation.Apply, witness(external())))
	if err != nil {
		t.Fatal(err)
	}
	request, err := decodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, servers := range [][]string{nil, {""}, {"bad server"}, {"-x"}, {"a.example.test", "a.example.test"}, {"line\nbreak"}} {
		request.Servers = servers
		data, err := reconciliation.Freeze(request, "provided time test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRequest(data); err == nil {
			t.Fatalf("servers %q decoded", servers)
		}
	}
}
