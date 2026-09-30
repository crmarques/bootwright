package environment_test

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

// No rescue journey exists yet, so admission refuses a rescue declaration,
// however valid, at the declaration itself and names the field to remove. An
// Environment that declares none is not refused for it.
func TestAdmissionRefusesARescueDeclarationUntilARescueJourneyExists(t *testing.T) {
	image := api.NewObject(api.MachineImage, "rescue-media", api.Value{}, api.MapValue(
		api.FieldValue{Name: "bootMedia", Value: api.StringValue("local-media:rhel-9.8-x86_64-boot.iso")},
	))
	rescue := api.MapValue(
		api.FieldValue{Name: "imageRef", Value: api.StringValue("rescue-media")},
		api.FieldValue{Name: "os", Value: api.MapValue(
			api.FieldValue{Name: "family", Value: api.StringValue("rhel")},
			api.FieldValue{Name: "version", Value: api.StringValue("9.8")},
			api.FieldValue{Name: "architecture", Value: api.StringValue("x86_64")},
		)},
	)
	declared := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		api.FieldValue{Name: "lifecycle", Value: api.MapValue(api.FieldValue{Name: "rescue", Value: rescue})},
	))
	want := api.Issue{
		Code: "api.invariant", Field: "$.spec.lifecycle.rescue",
		Message:     "no rescue journey exists yet, so no lifecycle can use a rescue declaration",
		Remediation: "remove spec.lifecycle.rescue from Environment/lab",
	}
	issues := environment.Validate(declared, api.NewCatalog([]api.Object{declared, image}))
	if !slices.Equal(issues, []api.Issue{want}) {
		t.Fatalf("issues = %+v, want %+v", issues, []api.Issue{want})
	}
	undeclared := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue())
	if issues := environment.Validate(undeclared, api.NewCatalog([]api.Object{undeclared, image})); len(issues) != 0 {
		t.Fatalf("an Environment without a rescue declaration refuses %+v", issues)
	}
}
