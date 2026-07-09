package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

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
