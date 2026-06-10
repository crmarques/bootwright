package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// ProviderDriver returns the driver registry entry for a machine component.
// The diagnostic dispatch triplet and exact Ansible role names come from one
// registry so playbooks do not build role names from strings.
//
// bootRole names the protocol the controller drives at install time,
// not the substrate underneath it. Libvirt machines and bare-metal
// machines both speak Redfish (libvirt through sushy-emulator,
// bare-metal through a vendor BMC); the renderer projects a
// substrate-blind `boot` block onto the machine component so
// `boot_redfish` consumes one shape for both. bmcRole still carries
// the substrate distinction for the provider-host bmc_<role>/
// converger that stands up the BMC endpoint.
//
// Adding apply support for a new provider is one registry entry plus the
// matching Ansible roles. Public schema support remains owned by the API,
// validation, and specs.
func ProviderDriver(state v1alpha1.State, m v1alpha1.InstallMachine) roles.DispatchSupport {
	provider, ok := stateview.Provider(state, m.Source.ProviderRef.Name)
	if !ok {
		return roles.LookupDispatch("none", "none", "none")
	}
	if m.Source.ProfileRef.Name != "" {
		if _, ok := stateview.MachineProfile(provider, m.Source.ProfileRef.Name); !ok {
			return roles.LookupDispatch("none", "none", "none")
		}
		return roles.LookupProfileProvisioner(provider.Spec.Type)
	}
	if provider.Spec.Type == v1alpha1.ProvisionerBareMetal && m.Source.MachineRef.Name != "" {
		if _, ok := stateview.Machine(state, m.Source.MachineRef.Name); ok {
			return roles.LookupMachineProvisioner(provider.Spec.Type)
		}
	}
	return roles.LookupDispatch("none", "none", "none")
}

func ProviderDispatch(state v1alpha1.State, m v1alpha1.InstallMachine) (substrate, bmc, boot string) {
	driver := ProviderDriver(state, m)
	return driver.Dispatch.SubstrateRole, driver.Dispatch.BMCRole, driver.Dispatch.BootRole
}
