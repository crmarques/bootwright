package cli

import "github.com/crmarques/bootwright/internal/converge/workflow"

type scopeSpec struct {
	name              string
	short             string
	phaseNames        []string
	applyPhaseNames   []string
	applyPlaybook     string
	destroyPlaybook   string
	artifactsBaseName string
}

var infraScope = scopeSpec{
	name:              "infra",
	short:             "Install and configure InfraProvider, InfraComponent, and ClusterInfra",
	phaseNames:        []string{"provider", "infra-components", "cluster-infra", "storage-infra"},
	applyPlaybook:     "bootwright.core.workflow_infra_apply",
	destroyPlaybook:   "bootwright.core.workflow_infra_destroy",
	artifactsBaseName: "infra",
}

var clustersScope = scopeSpec{
	name:              "clusters",
	short:             "Provision storage, OpenShift clusters, addons, and integrations",
	phaseNames:        []string{"storage-cluster", "container-cluster", "addons"},
	applyPlaybook:     "bootwright.core.workflow_clusters_apply",
	artifactsBaseName: "clusters",
}

var containerClusterScope = scopeSpec{
	name:              "container-cluster",
	short:             "Install and configure managed OpenShift clusters via openshift-install agent",
	phaseNames:        []string{"container-cluster"},
	applyPhaseNames:   []string{"container-cluster", "addons"},
	applyPlaybook:     "bootwright.core.workflow_container_cluster_apply",
	destroyPlaybook:   "bootwright.core.workflow_clusters_destroy",
	artifactsBaseName: "container-cluster",
}

var storageClusterScope = scopeSpec{
	name:              "storage-cluster",
	short:             "Provision external storage clusters",
	phaseNames:        []string{"storage-cluster"},
	artifactsBaseName: "storage-cluster",
}

var addonsScope = scopeSpec{
	name:              "addons",
	short:             "Apply post-install cluster addons",
	phaseNames:        []string{"addons"},
	artifactsBaseName: "addons",
}

var allScope = scopeSpec{
	name:              "all",
	short:             "Apply infrastructure, storage, OpenShift clusters, and addons",
	phaseNames:        []string{"provider", "infra-components", "cluster-infra", "storage-infra", "storage-cluster", "container-cluster", "addons"},
	applyPlaybook:     "bootwright.core.workflow_all_apply",
	artifactsBaseName: "all",
}

func (s scopeSpec) phases() []Phase {
	return scopePhases(s.phaseNames)
}

func (s scopeSpec) applyPhases() []Phase {
	names := s.phaseNames
	if len(s.applyPhaseNames) > 0 {
		names = s.applyPhaseNames
	}
	return scopePhases(names)
}

func (s scopeSpec) applyTarget() workflow.ApplyTarget {
	names := s.phaseNames
	if len(s.applyPhaseNames) > 0 {
		names = s.applyPhaseNames
	}
	return workflow.ApplyTarget{Name: s.name, PhaseNames: append([]string(nil), names...)}
}

func scopePhases(names []string) []Phase {
	out := make([]Phase, 0, len(names))
	for _, name := range names {
		out = append(out, phases[name])
	}
	return out
}
