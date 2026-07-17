package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	includeStorage := target.ClusterKind != ApplyClusterKindContainer
	includeContainer := target.ClusterKind != ApplyClusterKindStorage
	graph := NewActivityGraph()
	addAvailableMachineOSCapabilities(graph, state)
	kubeVirtReqsByCluster := map[string][]CapabilityRef{}
	if phaseSet[ApplyPhaseMachines] || phaseSet[ApplyPhaseDeps] || phaseSet[ApplyPhaseBase] {
		var err error
		kubeVirtReqsByCluster, err = kubeVirtHostClusterApplyCapabilities(state)
		if err != nil {
			return nil, err
		}
	}
	addAvailablePriorPhaseCapabilities(graph, state, phaseSet, kubeVirtReqsByCluster)
	machineServiceTaskIDs, err := planMachineServiceActivities(graph, state, phaseSet)
	if err != nil {
		return nil, err
	}

	managedOSDepsByCluster, err := planStorageManagedOSInstallActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs)
	if err != nil {
		return nil, err
	}
	registrationDepsByCluster, err := planStorageRegistrationActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster)
	if err != nil {
		return nil, err
	}
	storageInfraDepsByCluster, err := planStorageInfraActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster, registrationDepsByCluster)
	if err != nil {
		return nil, err
	}
	storageDepsByCluster, err := planStorageClusterActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs, storageInfraDepsByCluster)
	if err != nil {
		return nil, err
	}

	clusterNames := applyClusterNames(state)
	infraDepsByCluster, err := planContainerMachineInfraActivities(graph, state, phaseSet, includeContainer, clusterNames, kubeVirtReqsByCluster, machineServiceTaskIDs)
	if err != nil {
		return nil, err
	}
	hostVirtctlReadiness, hostsByContainerCluster, err := kubeVirtHostClusterReadiness(state)
	if err != nil {
		return nil, err
	}
	if err := planHostVirtctlActivities(graph, state, phaseSet, includeContainer, hostVirtctlReadiness); err != nil {
		return nil, err
	}
	if err := planContainerInstallActivities(graph, state, phaseSet, includeContainer, clusterNames, kubeVirtReqsByCluster, infraDepsByCluster, hostsByContainerCluster); err != nil {
		return nil, err
	}

	if phaseSet[ApplyPhaseAddons] {
		if err := planExtensionActivities(graph, state, phaseSet[ApplyPhaseBase], storageDepsByCluster); err != nil {
			return nil, err
		}
		if err := planNodeConfigActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
	}
	if err := planProvisioningPlaybookActivities(graph, state, phaseSet, target); err != nil {
		return nil, err
	}
	return graph.Lower()
}

func planExtensionActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) error {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return err
	}
	for _, binding := range plans {
		if !stateHasContainerCluster(state, binding.Cluster) {
			continue
		}
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+binding.Cluster)
		}
		for _, extension := range binding.Addons {
			extension := extension
			id := "addon." + binding.Cluster + "." + extension.Name
			provides := addonProvidedCapabilities(binding.Cluster, extension.Extension)
			hookDeps := hookCrossClusterDependencies(state, binding, extension.Name, extension.Extension, installPhasePlanned, storageDepsByCluster)
			addonDeps := appendUniqueStrings(append([]string(nil), deps...), hookDeps...)
			hookStateContainers, hookStateStorage := hookReferencedClusters(state, binding, extension.Name, extension.Extension)
			if err := graph.Add(Activity{
				ID:                   id,
				Provides:             provides,
				ExplicitDependencies: addonDeps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          id,
						Kind:        ApplyTaskKindClusterAddon,
						Label:       "addon " + extension.Name,
						Cluster:     binding.Cluster,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					State:     stategraph.FilterStateToApplyClusterRoots(state, hookStateContainers, hookStateStorage),
					Extension: &extension,
				},
			}); err != nil {
				return err
			}
			deps = []string{id}
		}
	}
	return nil
}

func addAvailablePriorPhaseCapabilities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, kubeVirtReqsByCluster map[string][]CapabilityRef) {
	if !phaseSet[ApplyPhaseFabric] {
		members := render.HostGroupMembers(state)
		for _, host := range members[render.GroupProviderHosts] {
			graph.AddAvailable(providerHostReadyCapability(host))
			graph.AddAvailable(providerServiceReadyCapability(host))
		}
		for _, host := range members[render.GroupInfraComponentHosts] {
			graph.AddAvailable(serviceEndpointReadyCapability(host))
		}
	}
	clusterInstalledKind := clusterInstalledCapability("").Kind
	kubevirtAddonKind := addonProvidesCapability("", v1alpha1.ClusterAddonProvidesKubeVirt).Kind
	baseInScope := phaseSet[ApplyPhaseBase]
	addonsInScope := phaseSet[ApplyPhaseAddons]
	for _, caps := range kubeVirtReqsByCluster {
		for _, capability := range caps {
			switch {
			case capability.Kind == clusterInstalledKind && !baseInScope:
				graph.AddAvailable(capability)
			case capability.Kind == kubevirtAddonKind && !addonsInScope:
				graph.AddAvailable(capability)
			}
		}
	}
}

func addAvailableMachineOSCapabilities(graph *ActivityGraph, state v1alpha1.State) {
	for _, machine := range state.Machines {
		if v1alpha1.MachineOSProvided(machine) {
			graph.AddAvailable(machineOSReadyCapability(machine.Metadata.Name))
		}
	}
}

func addonProvidedCapabilities(cluster string, extension v1alpha1.ClusterAddon) []CapabilityRef {
	out := make([]CapabilityRef, 0, len(extension.Spec.Provides))
	for _, capability := range extension.Spec.Provides {
		if capability == "" {
			continue
		}
		out = append(out, addonProvidesCapability(cluster, capability))
	}
	return out
}
