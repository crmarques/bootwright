package providedupdate

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestProvidedUpdatePlansTheOptedInMachineWithItsLifecyclePin(t *testing.T) {
	capability := New(nil)
	input := providedUpdateInput(PolicyWithinRelease)
	plan, err := capability.Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("update definitions = %d, want one", len(plan.Definitions))
	}
	definition := plan.Definitions[0]
	hardware := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily}
	if definition.ID != "os-update-witness" || definition.Stage != reconciliation.StageMachines ||
		definition.Family != machine.ProvidedUpdateFamily || !slices.Equal(definition.Requires, []reconciliation.ObjectRef{hardware}) {
		t.Fatalf("update definition = %+v", definition)
	}
	block := reconciliation.Block{BlockDefinition: definition}
	request, err := decodeBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	wantPlacement := machine.Placement{Connection: machine.ConnectionSSH, Machine: "witness", Address: "192.0.2.234",
		Port: 2222, User: "maintenance", Privilege: "sudo", PrivateKeyRef: "update-key", KnownHostsRef: "update-pin"}
	if request.Policy != PolicyWithinRelease || request.Release != "9.8" || request.Identity.Context != input.Context.Name || request.Placement != wantPlacement {
		t.Fatalf("frozen update request = %+v", request)
	}
	if want := []string{"update-key", "update-pin"}; !slices.Equal(plan.Secrets, want) {
		t.Fatalf("update binding secrets = %v, want %v", plan.Secrets, want)
	}
	classes, err := capability.SecretClasses(block)
	if err != nil || classes.Of("update-key") != secrets.AccessCredential || classes.Of("update-pin") != secrets.TrustAnchor {
		t.Fatalf("update secret classes = %+v, %v", classes, err)
	}
	if claimed := capability.Claimed(input.State.Effective()); !slices.Equal(claimed, []string{"Machine/witness"}) {
		t.Fatalf("claimed Machines = %v", claimed)
	}
}

func TestProvidedUpdateToReleaseFreezesItsPolicyAndNamesTheMove(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), providedUpdateInput(PolicyToRelease))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v, %v, want one update", plan, err)
	}
	definition := plan.Definitions[0]
	request, err := decodeBlock(reconciliation.Block{BlockDefinition: definition})
	if err != nil || request.Policy != PolicyToRelease {
		t.Fatalf("frozen update request = %+v, %v", request, err)
	}
	if definition.Description != "update witness to RHEL 9.8" ||
		!slices.Equal(definition.Impacts, []string{"update packages to RHEL 9.8 and reboot if changed on witness"}) {
		t.Fatalf("update definition = %q %v", definition.Description, definition.Impacts)
	}
	refusals := refusals(request)
	if got := diagnostics.Of(refusals["update-release-mismatch"]); len(got) != 1 || got[0].Message != "Machine/witness: the installed system must run RHEL 9.8 or an earlier RHEL 9 minor release" {
		t.Fatalf("toRelease mismatch = %+v", got)
	}
	if _, ok := refusals["update-release-unavailable"]; !ok {
		t.Fatal("toRelease names no refusal for repositories without the release")
	}
}

func TestProvidedUpdateRefusesAFrozenRequestWithoutAPolicy(t *testing.T) {
	plan, err := New(nil).Plan(t.Context(), providedUpdateInput(PolicyWithinRelease))
	if err != nil {
		t.Fatal(err)
	}
	request, err := decodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"", "anyRelease"} {
		request.Policy = policy
		data, err := reconciliation.Freeze(request, "provided OS update test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRequest(data); err == nil {
			t.Fatalf("policy %q decoded", policy)
		}
	}
}

func TestProvidedUpdateDoesNotPlanWithoutAnOptInOrDuringPreparation(t *testing.T) {
	preparation := providedUpdateInput(PolicyWithinRelease)
	preparation.Preparation = &reconciliation.PreparationScope{Kind: machine.EnrollmentFamily, Targets: []string{"witness"}}
	for name, input := range map[string]lifecycle.PlanInput{
		"plain provided Machine": providedUpdateInput(""),
		"enrollment preparation": preparation,
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := New(nil).Plan(t.Context(), input)
			if err != nil || len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
				t.Fatalf("plan = %+v, %v, want no update or credentials", plan, err)
			}
		})
	}
}

func providedUpdateInput(policy string) lifecycle.PlanInput {
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
	if policy != "" {
		spec = spec.WithPath(api.MapValue(text("policy", policy), text("release", "9.8")), "os", "provided", "updates")
	}
	catalog := api.NewCatalog([]api.Object{plain, api.NewObject(api.Machine, "witness", api.Value{}, spec)})
	return lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: "example"}}
}
