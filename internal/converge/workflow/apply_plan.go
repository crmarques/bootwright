package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

// PlanApplyTasksChecked builds the apply DAG for a scope. It composes the
// per-domain planners (each in its own file) rather than inlining every phase:
// graph setup and prior-phase availability here, then the storage-cluster chain
// (apply_plan_storage.go), the container-cluster chain (apply_plan_container.go),
// add-ons/attachments/node-config, and operator provisioning playbooks. Each
// stage returns the per-cluster task IDs the next stage depends on, so the
// dependency wiring stays visible at this level while the activity construction
// lives with its domain. The pure desired-state lookups the planners share live
// in apply_plan_facts.go, and the destructive-identity hash projection in
// apply_plan_hash.go.
func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	// deps and base are shared by storage and container clusters; ClusterKind
	// lets a single-kind scope plan only its kind so the unified gates do not
	// pull in the other kind's tasks.
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
	// A scoped sub-phase run assumes the phases it does NOT include already ran, so
	// mark the capabilities those phases land as available. Without this a documented
	// sub-phase (e.g. `--stage machines`) fails Lower() with "requires unavailable
	// capability provider.host-ready:<host>" (or the KubeVirt host-cluster caps),
	// mirroring how base-only ISO reuse and addAvailableMachineOSCapabilities already
	// assume a prior run for capabilities their phase does not plan.
	addAvailablePriorPhaseCapabilities(graph, state, phaseSet, kubeVirtReqsByCluster)
	machineServiceTaskIDs, err := planMachineServiceActivities(graph, state, phaseSet)
	if err != nil {
		return nil, err
	}

	// Storage-cluster chain: managed-OS install (machines) -> cephadm prereqs
	// (deps) -> bootstrap + operations (base). Each stage feeds the next its
	// per-cluster task IDs.
	managedOSDepsByCluster, err := planStorageManagedOSInstallActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs)
	if err != nil {
		return nil, err
	}
	storageInfraDepsByCluster, err := planStorageInfraActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs, managedOSDepsByCluster)
	if err != nil {
		return nil, err
	}
	storageDepsByCluster, err := planStorageClusterActivities(graph, state, target, phaseSet, includeStorage, machineServiceTaskIDs, storageInfraDepsByCluster)
	if err != nil {
		return nil, err
	}

	// Container-cluster chain: machine infra (machines) -> agent ISO + per-host
	// virtctl (deps) -> boot + install-wait (base). virtctl readiness is computed
	// once and shared by the virtctl provision and the child boot ordering.
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
		if err := planExtensionActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
		if err := planStorageAttachmentActivities(graph, state, phaseSet[ApplyPhaseBase], storageDepsByCluster); err != nil {
			return nil, err
		}
		if err := planNodeConfigActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
	}
	// ProvisioningPlaybooks anchor to any of the five phases, so they plan after
	// every core activity is added — the phase index they wire against reads the
	// completed graph snapshot.
	if err := planProvisioningPlaybookActivities(graph, state, phaseSet, target); err != nil {
		return nil, err
	}
	return graph.Lower()
}

func planExtensionActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool) error {
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
			// One task per addon installs (oc apply) and then waits for the addon
			// to report ready; it provides the capabilities downstream tasks need.
			id := "addon." + binding.Cluster + "." + extension.Name
			provides := addonProvidedCapabilities(binding.Cluster, extension.Extension)
			if err := graph.Add(Activity{
				ID:                   id,
				Provides:             provides,
				ExplicitDependencies: append([]string(nil), deps...),
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          id,
						Kind:        ApplyTaskKindClusterAddon,
						Label:       "addon " + extension.Name,
						Cluster:     binding.Cluster,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
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

// addAvailablePriorPhaseCapabilities marks the capabilities landed by phases that
// are NOT in the planned phaseSet as already-available, so a scoped sub-phase run
// (e.g. `--stage machines`) plans against the assumption that its excluded phases
// already ran — the same prior-run assumption the base-only ISO-reuse path makes.
// It only adds a capability whose provider is genuinely absent from this graph
// (the phase that provides it is out of scope): ActivityGraph.Lower prefers an
// available capability over an in-graph provider, so adding one that a planned
// phase provides would silently drop the ordering dependency. The phaseSet guards
// below encode exactly that "provider not planned" condition.
func addAvailablePriorPhaseCapabilities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, kubeVirtReqsByCluster map[string][]CapabilityRef) {
	// Fabric lands the provider-host services (provider.host-ready / service-ready)
	// and the infra-component service endpoints that machine-prepare tasks require.
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
	// KubeVirt child machine/ISO/boot tasks require the parent host cluster installed
	// (landed by base) and its kubevirt-providing add-on (landed by add-ons).
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
