package providedhostname

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

func TestProvidedHostnamePlansTheMachineWithItsFQDNAndLifecyclePin(t *testing.T) {
	capability := New(nil)
	input := providedHostnameInput(reconciliation.Apply, true)
	plan, err := capability.Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("hostname definitions = %d, want one", len(plan.Definitions))
	}
	definition := plan.Definitions[0]
	hardware := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily}
	if definition.ID != "os-hostname-witness" || definition.Stage != reconciliation.StageMachines ||
		definition.Family != machine.ProvidedHostnameFamily || !slices.Equal(definition.Requires, []reconciliation.ObjectRef{hardware}) {
		t.Fatalf("hostname definition = %+v", definition)
	}
	if definition.Description != "set the hostname of witness to witness.example.test" ||
		!slices.Equal(definition.Impacts, []string{"set the static and kernel hostname of witness to witness.example.test"}) {
		t.Fatalf("hostname description = %q %v", definition.Description, definition.Impacts)
	}
	block := reconciliation.Block{BlockDefinition: definition}
	request, err := decodeBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement := machine.Placement{Connection: machine.ConnectionSSH, Machine: "witness", Address: "192.0.2.234",
		Port: 2222, User: "maintenance", Privilege: "sudo", PrivateKeyRef: "update-key", KnownHostsRef: "update-pin"}
	if request.Hostname != "witness.example.test" || request.Identity.Context != input.Context.Name || request.Placement != wantPlacement {
		t.Fatalf("frozen hostname request = %+v", request)
	}
	if want := []string{"update-key", "update-pin"}; !slices.Equal(plan.Secrets, want) {
		t.Fatalf("hostname binding secrets = %v, want %v", plan.Secrets, want)
	}
	classes, err := capability.SecretClasses(block)
	if err != nil || classes.Of("update-key") != secrets.AccessCredential || classes.Of("update-pin") != secrets.TrustAnchor {
		t.Fatalf("hostname secret classes = %+v, %v", classes, err)
	}
	if claimed := capability.Claimed(input.State.Effective()); !slices.Equal(claimed, []string{"Machine/witness"}) {
		t.Fatalf("claimed Machines = %v", claimed)
	}
}

func TestProvidedHostnameDestroyRetainsTheHostname(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), providedHostnameInput(reconciliation.Destroy, true))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v, %v, want one hostname block", plan, err)
	}
	if definition := plan.Definitions[0]; definition.Description != "retain the hostname of witness" || len(definition.Impacts) != 0 {
		t.Fatalf("destroy definition = %q %v", definition.Description, definition.Impacts)
	}
}

func TestProvidedHostnameRefusesAFrozenRequestWithoutADNSName(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), providedHostnameInput(reconciliation.Apply, true))
	if err != nil {
		t.Fatal(err)
	}
	request, err := decodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, hostname := range []string{"", "-bad.example.test", "bad..example.test", "bad.example.test.", "bad host", "-x"} {
		request.Hostname = hostname
		data, err := reconciliation.Freeze(request, "provided OS hostname test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRequest(data); err == nil {
			t.Fatalf("hostname %q decoded", hostname)
		}
	}
}

func TestProvidedHostnameDoesNotPlanWithoutAnFQDNOrDuringPreparation(t *testing.T) {
	preparation := providedHostnameInput(reconciliation.Apply, true)
	preparation.Preparation = &reconciliation.PreparationScope{Kind: machine.EnrollmentFamily, Targets: []string{"witness"}}
	controller := providedHostnameInput(reconciliation.Apply, true)
	controller.Controller = "witness"
	for name, input := range map[string]lifecycle.PlanInput{
		"enrollment preparation": preparation,
		"controller Machine":     controller,
		"no fqdn":                providedHostnameInput(reconciliation.Apply, false),
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := New(nil).Plan(t.Context(), input)
			if err != nil || len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
				t.Fatalf("plan = %+v, %v, want no hostname or credentials", plan, err)
			}
		})
	}
}

func providedHostnameInput(verb reconciliation.Verb, fqdn bool) lifecycle.PlanInput {
	field := func(name string, value api.Value) api.FieldValue {
		return api.FieldValue{Name: name, Value: value}
	}
	text := func(name, value string) api.FieldValue {
		return field(name, api.StringValue(value))
	}
	addresses := []api.Value{api.MapValue(text("name", "ssh"), text("address", "192.0.2.234"))}
	if fqdn {
		addresses = append(addresses, api.MapValue(text("name", "fqdn"), text("address", "witness.example.test")))
	}
	spec := api.MapValue(
		field("os", api.MapValue(field("provided", api.MapValue()))),
		field("network", api.MapValue(field("addresses", api.ListValue(addresses...)))),
		field("access", api.MapValue(
			field("ssh", api.MapValue(text("addressRef", "ssh"), field("port", api.IntegerValue("2222")),
				field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))))),
			field("lifecycle", api.MapValue(text("user", "maintenance"), text("privilege", "sudo"),
				text("privateKeyRef", "update-key"), text("knownHostsRef", "update-pin"))),
		)),
	)
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Machine, "witness", api.Value{}, spec)})
	return lifecycle.PlanInput{Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: "example"}}
}
