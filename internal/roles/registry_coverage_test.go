package roles

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEveryProvisionerDispatchesToSupportedRoles(t *testing.T) {
	for _, provisioner := range v1alpha1.Provisioners() {
		profile := LookupProfileProvisioner(provisioner)
		machine := LookupMachineProvisioner(provisioner)
		if !profile.ApplySupported() && !machine.ApplySupported() {
			t.Errorf("provisioner %q dispatches to no supported role contract (profile=%q machine=%q); add a dispatchSupport entry and wire it into LookupProfileProvisioner or LookupMachineProvisioner", provisioner, profile.Status, machine.Status)
		}
	}
}

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
