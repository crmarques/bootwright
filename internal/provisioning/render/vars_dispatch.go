package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/support"
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
func ProviderDriver(state v1alpha1.State, m v1alpha1.ClusterMachineComponent) support.DispatchSupport {
	provider, ok := findProvider(state, m.From.Provider)
	if !ok {
		return support.LookupDispatch("none", "none", "none")
	}
	if m.From.Profile != "" {
		profile, ok := findProfile(provider, m.From.Profile)
		if !ok {
			return support.LookupDispatch("none", "none", "none")
		}
		return support.LookupProfileProvisioner(v1alpha1.ProfileProvisionerKind(profile))
	}
	if m.From.Name != "" {
		server, ok := findProviderMachine(provider, m.From.Name)
		if !ok {
			return support.LookupDispatch("none", "none", "none")
		}
		return support.LookupMachineProvisioner(v1alpha1.MachineProvisionerKind(server))
	}
	return support.LookupDispatch("none", "none", "none")
}

func ProviderDispatch(state v1alpha1.State, m v1alpha1.ClusterMachineComponent) (substrate, bmc, boot string) {
	driver := ProviderDriver(state, m)
	return driver.Dispatch.SubstrateRole, driver.Dispatch.BMCRole, driver.Dispatch.BootRole
}
