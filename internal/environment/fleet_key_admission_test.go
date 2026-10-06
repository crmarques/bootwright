package environment_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func fleetMachine(name string, spec api.Value) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, spec)
}

func installedMachine(name string) api.Object {
	return fleetMachine(name, api.MapValue(api.FieldValue{Name: "os", Value: api.MapValue(
		api.FieldValue{Name: "provided", Value: api.BoolValue(false)},
		api.FieldValue{Name: "installProfileRef", Value: api.StringValue("rhel")},
	)}))
}

func TestAbsentFleetKeyIsOneRequiredRefusalNamingAnInstalledMachine(t *testing.T) {
	env := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue())
	issues := environment.Validate(env, api.NewCatalog([]api.Object{env, installedMachine("node-b"), installedMachine("node-a")}))
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want one", issues)
	}
	issue := issues[0]
	if issue.Code != "api.required" || issue.Field != "$.spec.remoteMachinesAccessKey.keyRef" ||
		!strings.HasPrefix(issue.Message, "Machine/node-a installs a managed OS") || !strings.Contains(issue.Message, "(and 1 other Machine)") ||
		issue.Remediation != "set spec.remoteMachinesAccessKey.keyRef to an sshKeyPair Secret" {
		t.Fatalf("absent fleet key refusal = %+v", issue)
	}
}

func TestReusedFleetKeyNamesTheProvidedMachine(t *testing.T) {
	env := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		api.FieldValue{Name: "remoteMachinesAccessKey", Value: api.MapValue(api.FieldValue{Name: "keyRef", Value: api.StringValue("fleet-key")})},
	))
	provided := fleetMachine("bastion", api.MapValue(
		api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(true)})},
		api.FieldValue{Name: "access", Value: api.MapValue(api.FieldValue{Name: "ssh", Value: api.MapValue(api.FieldValue{Name: "auth", Value: api.MapValue(
			api.FieldValue{Name: "privateKeyRef", Value: api.StringValue("fleet-key")},
		)})})},
	))
	issues := environment.Validate(env, api.NewCatalog([]api.Object{env, provided, installedMachine("node-a")}))
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want one", issues)
	}
	issue := issues[0]
	if issue.Code != "api.invariant" || issue.Field != "$.spec.remoteMachinesAccessKey.keyRef" ||
		!strings.HasPrefix(issue.Message, "Machine/bastion uses the fleet install key as its access key") ||
		issue.Remediation != "give Machine/bastion its own sshKeyPair Secret, or choose another fleet key" {
		t.Fatalf("reused fleet key refusal = %+v", issue)
	}
}
