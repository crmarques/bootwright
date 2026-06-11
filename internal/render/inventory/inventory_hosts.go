package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/render/installer"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func ocpReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	if stateview.Environment(state) != nil {
		out["localhost"] = true
	}
	return out
}

// infraReferencedHosts returns the hosts that back a profile-based
// machine substrate. Bare-metal `machines[]` entries are reached over
// BMC from the controller. KubeVirt and vSphere VM operations run from the
// controller — against a kubeconfig and the vCenter API respectively — so
// they contribute localhost.
func infraReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		ci, err := installer.ClusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, m := range ci.Machines {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, m := range ci.Machines {
			machine, ok := stateview.Machine(state, m.Name)
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
	}
	return out
}

func providerReferencedHosts(state v1alpha1.State) map[string]bool {
	return mergeHostSets(mergeHostSets(providerServiceReferencedHosts(state), providerHostSetupReferencedHosts(state)), providerCapabilityReferencedHosts(state))
}

func providerCapabilityReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, provider := range state.InfraProviders {
		if provider.Spec.Type == v1alpha1.ProvisionerLibvirt && provider.Spec.Libvirt != nil && provider.Spec.Libvirt.MachineRef.Name != "" {
			out[provider.Spec.Libvirt.MachineRef.Name] = true
		}
	}
	return out
}

func providerServiceReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.IsProviderService() && service.MachineRef != "" {
			out[service.MachineRef] = true
		}
	}
	return out
}

func infraComponentReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.IsInfraComponentService() && service.MachineRef != "" {
			out[service.MachineRef] = true
		}
	}
	return out
}

func providerHostSetupReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		ci, err := installer.ClusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, machine := range ci.Machines {
			machineRef := machineHostRef(state, machine)
			if machineRef == "" {
				continue
			}
			if len(ProviderDriver(state, machine).Roles.MachineSetupRoles) > 0 {
				out[machineRef] = true
			}
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			rawMachine, ok := stateview.Machine(state, machine.Name)
			if !ok || !v1alpha1.MachineInstallsOS(rawMachine) {
				continue
			}
			machineRef := machineHostRef(state, machine)
			if machineRef == "" {
				continue
			}
			if len(ProviderDriver(state, machine).Roles.MachineSetupRoles) > 0 {
				out[machineRef] = true
			}
		}
	}
	return out
}

func bootReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		ci, err := installer.ClusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, m := range ci.Machines {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
		if !artifacts.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		server, ok := artifacts.Select(state, ci)
		if !ok || server.Config == nil {
			continue
		}
		if host := server.Config.MachineRef.Name; host != "" {
			out[host] = true
		}
	}
	return out
}

func mergeHostSets(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
