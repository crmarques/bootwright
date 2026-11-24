package providedregistration

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

func TestProvidedRegistrationPlansTheOptedInMachineWithItsSatelliteAndPin(t *testing.T) {
	capability := New(nil)
	input := registrationInput(true)
	plan, err := capability.Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("registration definitions = %d, want one", len(plan.Definitions))
	}
	definition := plan.Definitions[0]
	hardware := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily}
	if definition.ID != "os-registration-witness" || definition.Stage != reconciliation.StageMachines ||
		definition.Family != machine.ProvidedRegistrationFamily || !slices.Equal(definition.Requires, []reconciliation.ObjectRef{hardware}) ||
		definition.Description != "converge the Satellite registration of witness onto satellite.example.test" ||
		!slices.Equal(definition.Impacts, []string{"point the subscription configuration of witness at satellite.example.test and regenerate its repositories"}) {
		t.Fatalf("registration definition = %+v", definition)
	}
	block := reconciliation.Block{BlockDefinition: definition}
	request, err := decodeBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement := machine.Placement{Connection: machine.ConnectionSSH, Machine: "witness", Address: "192.0.2.234",
		Port: 2222, User: "maintenance", Privilege: "sudo", PrivateKeyRef: "update-key", KnownHostsRef: "update-pin"}
	wantSatellite := Satellite{ContentBaseURL: "https://satellite.example.test/pulp/content", Hostname: "satellite.example.test", TrustBundleRef: "satellite-ca"}
	if request.EntitlementRef != "rhel" || request.Identity.Context != input.Context.Name || request.Placement != wantPlacement || request.Satellite != wantSatellite {
		t.Fatalf("frozen registration request = %+v", request)
	}
	if want := []string{"satellite-ca", "update-key", "update-pin"}; !slices.Equal(plan.Secrets, want) {
		t.Fatalf("registration binding secrets = %v, want %v", plan.Secrets, want)
	}
	classes, err := capability.SecretClasses(block)
	if err != nil || classes.Of("update-key") != secrets.AccessCredential || classes.Of("update-pin") != secrets.TrustAnchor || classes.Of("satellite-ca") != secrets.TrustAnchor {
		t.Fatalf("registration secret classes = %+v, %v", classes, err)
	}
	if references, err := capability.SecretReferences(block); err != nil || !slices.Contains(references, "satellite-ca") {
		t.Fatalf("registration secret references = %v, %v", references, err)
	}
	if claimed := capability.Claimed(input.State.Effective()); !slices.Equal(claimed, []string{"Machine/witness"}) {
		t.Fatalf("claimed Machines = %v", claimed)
	}
}

func TestProvidedRegistrationDestroyPlansItsRetention(t *testing.T) {
	input := registrationInput(true)
	input.Verb = reconciliation.Destroy
	plan, err := New(nil).Plan(t.Context(), input)
	if err != nil || len(plan.Definitions) != 1 || plan.Definitions[0].Description != "retain the Satellite registration of witness" || len(plan.Definitions[0].Impacts) != 0 {
		t.Fatalf("destroy plan = %+v, %v", plan, err)
	}
}

func TestProvidedRegistrationRefusesAFrozenRequestWithoutItsSatellite(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), registrationInput(true))
	if err != nil {
		t.Fatal(err)
	}
	request, err := decodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Request){
		"no hostname":      func(r *Request) { r.Satellite.Hostname = "" },
		"no CA bundle":     func(r *Request) { r.Satellite.TrustBundleRef = "" },
		"plain HTTP root":  func(r *Request) { r.Satellite.ContentBaseURL = "http://satellite.example.test/pulp/content" },
		"no content root":  func(r *Request) { r.Satellite.ContentBaseURL = "" },
		"no Entitlement":   func(r *Request) { r.EntitlementRef = "" },
		"another block id": func(r *Request) { r.Identity.Block = "os-registration-other" },
		"unpinned":         func(r *Request) { r.Placement.KnownHostsRef = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			change(&changed)
			data, err := reconciliation.Freeze(changed, "provided registration test")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRequest(data); err == nil {
				t.Fatal("the invalid request decoded")
			}
		})
	}
}

func TestProvidedRegistrationDoesNotPlanWithoutAnOptInOrDuringPreparation(t *testing.T) {
	preparation := registrationInput(true)
	preparation.Preparation = &reconciliation.PreparationScope{Kind: machine.EnrollmentFamily, Targets: []string{"witness"}}
	for name, input := range map[string]lifecycle.PlanInput{
		"plain provided Machine": registrationInput(false),
		"enrollment preparation": preparation,
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := New(nil).Plan(t.Context(), input)
			if err != nil || len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
				t.Fatalf("plan = %+v, %v, want no registration or credentials", plan, err)
			}
		})
	}
}

func registrationInput(opted bool) lifecycle.PlanInput {
	field := func(name string, value api.Value) api.FieldValue {
		return api.FieldValue{Name: name, Value: value}
	}
	text := func(name, value string) api.FieldValue {
		return field(name, api.StringValue(value))
	}
	provided := field("os", api.MapValue(field("provided", api.MapValue())))
	spec := api.MapValue(
		provided,
		field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ssh"), text("address", "192.0.2.234")))))),
		field("access", api.MapValue(
			field("ssh", api.MapValue(text("addressRef", "ssh"), field("port", api.IntegerValue("2222")),
				field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))))),
			field("lifecycle", api.MapValue(text("user", "maintenance"), text("privilege", "sudo"),
				text("privateKeyRef", "update-key"), text("knownHostsRef", "update-pin"))),
		)),
	)
	plain := api.NewObject(api.Machine, "operator-managed", api.Value{}, api.MapValue(provided))
	if opted {
		spec = spec.WithPath(api.MapValue(text("entitlementRef", "rhel")), "os", "provided", "subscription")
	}
	entitlement := api.NewObject(api.Entitlement, "rhel", api.Value{}, api.MapValue(text("type", "redhatRhel"),
		field("rhsm", api.MapValue(field("satellite", api.MapValue(text("hostname", "satellite.example.test"),
			text("contentBaseURL", "https://satellite.example.test/pulp/content"), text("trustBundleRef", "satellite-ca")))))))
	catalog := api.NewCatalog([]api.Object{plain, api.NewObject(api.Machine, "witness", api.Value{}, spec), entitlement})
	return lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: "example"}}
}
