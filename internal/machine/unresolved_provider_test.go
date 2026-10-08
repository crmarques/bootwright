package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAMachineWhoseProviderDoesNotResolveGetsNoProviderRefusal(t *testing.T) {
	machine, catalog := installedFixture()
	spec := machine.Spec()
	bmc := spec.Get("hardware", "management", "bmc")
	if !bmc.Present() {
		t.Fatal("the fixture declares no BMC, so the test proves nothing")
	}
	spec = spec.With("hardware", spec.Get("hardware").With("management", m("bmc", bmc.Without("credentialsRef"))))
	machine = machine.WithSpec(spec)
	objects := []api.Object{machine}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.InfraProvider && existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	unresolved := api.NewCatalog(objects)
	if _, found := Provider(machine, unresolved); found {
		t.Fatal("the provider still resolves")
	}
	issues := Validate(machine, unresolved)
	for _, field := range []string{"$.spec.os.install.hostKeyRef", "$.spec.hardware.management.bmc.credentialsRef"} {
		if mentions(issues, field) {
			t.Fatalf("a Machine whose provider does not resolve was refused at %s: %v", field, issues)
		}
	}
	resolved := api.NewCatalog(append(objects, catalog.OfKind(api.InfraProvider)...))
	if !mentions(Validate(machine, resolved), "$.spec.hardware.management.bmc.credentialsRef") {
		t.Fatal("a resolved bare-metal provider no longer requires BMC credentials")
	}
}
