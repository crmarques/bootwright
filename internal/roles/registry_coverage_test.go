package roles

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestEveryProvisionerDispatchesToSupportedRoles enforces that each substrate
// provisioner the schema accepts (v1alpha1.Provisioners) resolves to a supported
// role contract through one of the two dispatch entry points. A new substrate
// added to the schema without a registry entry would silently fall through to
// the "unknown" apply-support fallback; this guard makes that a build failure so
// the registry cannot drift behind the provisioner set.
func TestEveryProvisionerDispatchesToSupportedRoles(t *testing.T) {
	for _, provisioner := range v1alpha1.Provisioners() {
		profile := LookupProfileProvisioner(provisioner)
		machine := LookupMachineProvisioner(provisioner)
		if !profile.ApplySupported() && !machine.ApplySupported() {
			t.Errorf("provisioner %q dispatches to no supported role contract (profile=%q machine=%q); add a dispatchSupport entry and wire it into LookupProfileProvisioner or LookupMachineProvisioner", provisioner, profile.Status, machine.Status)
		}
	}
}

// TestEveryComponentSlotHasSupportedService enforces that each authored
// InfraComponent slot (v1alpha1.InfraComponentSlots) has at least one supported
// service realisation in the registry. A new managed service arm added to the
// schema without a serviceSupport entry would render no apply role; this guard
// forces the registry entry to land with the arm.
func TestEveryComponentSlotHasSupportedService(t *testing.T) {
	covered := map[string]bool{}
	for _, service := range ServiceEntries() {
		if service.Status == StatusSupported {
			covered[service.Key.Kind] = true
		}
	}
	for _, slot := range v1alpha1.InfraComponentSlots() {
		if !covered[slot] {
			t.Errorf("component slot %q has no supported service realisation in the registry; add a serviceSupport entry for it", slot)
		}
	}
}
