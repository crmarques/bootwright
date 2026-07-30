package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

const addonCapabilityKindPrefix = "addon."

func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	return PlanApplyTasksCheckedWithHashState(target, state, state)
}

func PlanApplyTasksCheckedWithHashState(target ApplyTarget, state v1alpha1.State, hashState v1alpha1.State) ([]ApplyTask, error) {
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
	machineServiceTaskIDs, err := planMachineServiceActivities(graph, state, hashState, target, phaseSet)
	if err != nil {
		return nil, err
	}

	managedOSDepsByCluster, err := planStorageManagedOSInstallActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs)
	if err != nil {
		return nil, err
	}
	nodeAccessDepsByCluster, err := planStorageNodeAccessActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster)
	if err != nil {
		return nil, err
	}
	for clusterName, ids := range nodeAccessDepsByCluster {
		managedOSDepsByCluster[clusterName] = append(managedOSDepsByCluster[clusterName], ids...)
	}
	registrationDepsByCluster, err := planStorageRegistrationActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster)
	if err != nil {
		return nil, err
	}
	repositoryDepsByCluster, err := planStorageRepositoryActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster, registrationDepsByCluster)
	if err != nil {
		return nil, err
	}
	for clusterName, ids := range repositoryDepsByCluster {
		registrationDepsByCluster[clusterName] = append(registrationDepsByCluster[clusterName], ids...)
	}
	storageInfraDepsByCluster, err := planStorageInfraActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster, registrationDepsByCluster)
	if err != nil {
		return nil, err
	}
	storageDepsByCluster, err := planStorageClusterActivities(graph, state, hashState, target, phaseSet, includeStorage, machineServiceTaskIDs, storageInfraDepsByCluster)
	if err != nil {
		return nil, err
	}

	clusterNames := applyClusterNames(state)
	infraDepsByCluster, prepareDepsByCluster, err := planContainerMachineInfraActivities(graph, state, target, phaseSet, includeContainer, clusterNames, kubeVirtReqsByCluster, machineServiceTaskIDs)
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
	if err := planContainerInstallActivities(graph, state, phaseSet, includeContainer, clusterNames, kubeVirtReqsByCluster, infraDepsByCluster, prepareDepsByCluster, machineServiceTaskIDs, hostsByContainerCluster); err != nil {
		return nil, err
	}

	if phaseSet[ApplyPhaseAddons] {
		if err := planExtensionActivities(graph, state, hashState, phaseSet[ApplyPhaseBase], storageDepsByCluster); err != nil {
			return nil, err
		}
		if err := planNodeConfigActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
	}
	if err := planPlaybookActivities(graph, state, phaseSet, target); err != nil {
		return nil, err
	}
	return graph.Lower()
}

func planExtensionActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) error {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return err
	}
	exclusiveOLMOwner := map[string]string{}
	for _, binding := range plans {
		if !stateHasContainerCluster(state, binding.Cluster) {
			continue
		}
		clusterDeps := []string{}
		if installPhasePlanned {
			clusterDeps = append(clusterDeps, "wait."+binding.Cluster)
		}
		for _, extension := range binding.Addons {
			extension := extension
			id := "addon." + binding.Cluster + "." + extension.Name
			provides := append(addonProvidedCapabilities(binding.Cluster, extension.Extension), addonAppliedCapability(binding.Cluster, extension.Name))
			stepDeps := stepCrossClusterDependencies(state, binding, extension.Name, extension.Extension, installPhasePlanned, storageDepsByCluster)
			addonDeps := appendUniqueStrings(append([]string(nil), clusterDeps...), stepDeps...)
			for _, key := range addonExclusiveOLMKeys(binding.Cluster, extension.Extension) {
				if previous := exclusiveOLMOwner[key]; previous != "" {
					addonDeps = appendUniqueStrings(addonDeps, previous)
				}
				exclusiveOLMOwner[key] = id
			}
			stepStateContainers, stepStateStorage := stepReferencedClusters(state, binding, extension.Name, extension.Extension)
			if err := graph.Add(Activity{
				ID:                   id,
				Requires:             addonRequiredCapabilities(binding.Cluster, extension.Extension),
				Provides:             provides,
				ExplicitDependencies: addonDeps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           id,
						Kind:         ApplyTaskKindClusterAddon,
						Label:        "addon " + extension.Name,
						Cluster:      binding.Cluster,
						ClusterKind:  ApplyClusterKindContainer,
						ResourceKeys: addonSharedResourceKeys(binding.Cluster, extension),
						Status:       TaskStatusPending,
					},
					State:            stategraph.FilterStateToApplyClusterRoots(state, stepStateContainers, stepStateStorage),
					DesiredHashState: addonDesiredHashState(hashState, stepStateContainers, stepStateStorage),
					Extension:        &extension,
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func addonDesiredHashState(hashState v1alpha1.State, containers, storage []string) *v1alpha1.State {
	s := stategraph.FilterStateToApplyClusterRoots(hashState, containers, storage)
	return &s
}

func addonExclusiveOLMKeys(cluster string, extension v1alpha1.ClusterAddon) []string {
	olm := extension.Spec.OLM
	if olm == nil {
		return nil
	}
	var out []string
	if olm.Namespace.Name != "" && (olm.Namespace.Create || olm.OperatorGroup != nil) {
		out = append(out, cluster+"/namespace/"+olm.Namespace.Name)
	}
	if olm.CatalogSource != nil && olm.CatalogSource.Name != "" {
		out = append(out, cluster+"/catalogsource/"+olm.Subscription.SourceNamespace+"/"+olm.CatalogSource.Name)
	}
	return out
}

func addonSharedResourceKeys(cluster string, extension extensionplan.ExtensionPlan) []string {
	for _, input := range extension.Extension.Spec.Accepts.Inputs {
		if !addonInputMergesGlobalPullSecret(input) || addonBindingInputValue(extension.Inputs, input.Name) == "" {
			continue
		}
		return []string{globalPullSecretResource(cluster)}
	}
	return nil
}

func addonInputMergesGlobalPullSecret(input v1alpha1.ClusterAddonAcceptedInput) bool {
	for _, effect := range input.Effects {
		if effect.GlobalPullSecretMerge != nil {
			return true
		}
	}
	return false
}

func addonBindingInputValue(inputs []v1alpha1.ClusterAddonBindingInput, name string) string {
	for _, input := range inputs {
		if input.Name == name {
			return input.Value
		}
	}
	return ""
}

func globalPullSecretResource(cluster string) string {
	return "pullsecret:" + cluster
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
	baseInScope := phaseSet[ApplyPhaseBase]
	addonsInScope := phaseSet[ApplyPhaseAddons]
	for _, caps := range kubeVirtReqsByCluster {
		for _, capability := range caps {
			switch {
			case capability.Kind == clusterInstalledKind && !baseInScope:
				graph.AddAvailable(capability)
			case strings.HasPrefix(capability.Kind, addonCapabilityKindPrefix) && !addonsInScope:
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
	out := make([]CapabilityRef, 0, len(extension.Spec.Provides)+1)
	for _, capability := range extension.Spec.Provides {
		if capability == "" {
			continue
		}
		out = append(out, addonProvidesCapability(cluster, capability))
	}
	return out
}

func addonRequiredCapabilities(cluster string, extension v1alpha1.ClusterAddon) []CapabilityRef {
	out := make([]CapabilityRef, 0, len(extension.Spec.Requires))
	for _, capability := range extension.Spec.Requires {
		if capability == "" {
			continue
		}
		out = append(out, addonProvidesCapability(cluster, capability))
	}
	return out
}

func addonAppliedCapability(cluster, addon string) CapabilityRef {
	return CapabilityRef{Kind: addonCapabilityKindPrefix + "applied", Name: cluster + "/" + addon}
}
