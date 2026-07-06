package workflow

import (
	"encoding/json"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// containerClusterInstallStructuralHashVars projects the DESTRUCTIVE-IDENTITY subset of
// a ContainerCluster's install intent: the cluster-filtered state with the day-2-owned
// intent cleared. Cluster add-ons and per-node labels/taints are applied AFTER install
// by the add-on and node-config tasks (their own reconfigure-only, non-destructive
// re-apply), so editing them must not flip the install object to a destructive reinstall.
// When the full desired hash drifts but this structural hash is unchanged, the only
// change is day-2 intent — reconcilable in place, so continue proceeds and --override
// does not reinstall. A change to install-config / agent-config identity (networks,
// platform, release, endpoints, host machineRefs, roles, FIPS) moves this hash and stays
// a destructive rebuild; the install-state reconcile gate (clusterInstallDesiredHashForContext)
// is the precise second backstop that still refuses regenerating install inputs for an
// installed cluster. The day-2 fields are cleared on a JSON deep copy so the shared
// render state is never mutated. Mirrors storageClusterStructuralHashVars.
func containerClusterInstallStructuralHashVars(clusterState v1alpha1.State) v1alpha1.State {
	var clone v1alpha1.State
	data, err := json.Marshal(clusterState)
	if err != nil {
		return clusterState
	}
	if err := json.Unmarshal(data, &clone); err != nil {
		return clusterState
	}
	clone.ClusterAddons = nil
	clone.ClusterAddonBindings = nil
	clone.ClusterAddonProfiles = nil
	// Shared fabric — the InfraProviders, InfraComponents (artifact server, proxy,
	// registry, DNS), and NetworkConfigs a cluster references — is reconfigure-only:
	// each is classified and re-applied by its OWN task, and a change to it (a BMC TLS
	// cipher, an artifact-server cipher, an egress proxy) does not require reinstalling
	// a running cluster. Left in the embedded state, one edit to a shared object would
	// move EVERY dependent cluster's structural hash and refuse the whole fleet as a
	// reinstall. Clear it: any material effect on THIS cluster's install still moves
	// the hash through the rendered InstallConfig/AgentConfig/Manifests the install-
	// state gate hashes alongside this projection, so an install-changing edit stays a
	// rebuild while a pure-fabric edit reconciles.
	clone.InfraProviders = nil
	clone.InfraComponents = nil
	clone.NetworkConfigs = nil
	for i := range clone.ContainerClusters {
		for j := range clone.ContainerClusters[i].Spec.Hosts {
			clone.ContainerClusters[i].Spec.Hosts[j].Labels = nil
			clone.ContainerClusters[i].Spec.Hosts[j].Taints = nil
			// An infra host installs as a worker and is promoted to infra day-2 by
			// the reconfigure-only nodeconfig task (installerNodeRole maps infra ->
			// worker, computeReplicaCount counts worker+infra), so every rendered
			// install artifact is byte-identical for worker<->infra. Project the role
			// through that same mapping here: master<->worker stays a real reinstall
			// (structural), while promoting an installed worker to infra reconciles
			// in place instead of steering to a full node re-image.
			clone.ContainerClusters[i].Spec.Hosts[j].Role = installTimeNodeRole(clone.ContainerClusters[i].Spec.Hosts[j].Role)
		}
	}
	return clone
}

// installTimeNodeRole mirrors internal/render/installer.installerNodeRole: an infra
// host is a worker at install time (promoted to infra day-2), so it collapses to
// "worker" for the destructive-identity hash. Kept local to avoid importing the
// installer render package into the workflow layer.
func installTimeNodeRole(role string) string {
	if role == v1alpha1.NodeRoleWorker || role == v1alpha1.NodeRoleInfra {
		return v1alpha1.NodeRoleWorker
	}
	return v1alpha1.NodeRoleMaster
}
