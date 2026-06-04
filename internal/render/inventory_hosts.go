package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func ocpReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	if primaryEnvironment(state) != nil {
		out["localhost"] = true
	}
	return out
}

// infraReferencedHosts returns the hosts that back a profile-based
// machine substrate. Bare-metal `machines[]` entries are reached over
// BMC from the controller. vSphere guests live on remote infrastructure and
// contribute nothing here. KubeVirt VM operations run from the controller
// against a kubeconfig, so they contribute localhost.
func infraReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, m := range ci.Machines {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
	}
	for _, cluster := range managedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, m := range ci.Machines {
			machine, ok := findMachine(state, m.Name)
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
	return mergeHostSets(providerServiceReferencedHosts(state), providerHostSetupReferencedHosts(state))
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
		ci, err := clusterInstallForOCP(state, ocp)
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
	for _, cluster := range managedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			rawMachine, ok := findMachine(state, machine.Name)
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
		ci, err := clusterInstallForOCP(state, ocp)
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
