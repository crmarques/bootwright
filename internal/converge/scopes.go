package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

type Scope struct {
	Name                    string
	Short                   string
	PhaseNames              []string
	ApplyPhaseNames         []string
	ApplyPlaybook           string
	DestroyPlaybook         string
	ArtifactsBaseName       string
	ClusterKind             string
	NoAnsible               bool
	TargetsContainerInstall bool
	AnsibleLimit            string
}

const (
	PhaseFabric   = workflow.ApplyPhaseFabric
	PhaseMachines = workflow.ApplyPhaseMachines
	PhaseDeps     = workflow.ApplyPhaseDeps
	PhaseBase     = workflow.ApplyPhaseBase
	PhaseAddons   = workflow.ApplyPhaseAddons
)

var InfraScope = Scope{
	Name:              "infra",
	Short:             "Install and configure InfraProvider, InfraComponent, and ClusterInstall",
	PhaseNames:        []string{PhaseFabric, PhaseMachines},
	ApplyPlaybook:     roles.PlaybookWorkflowInfraApply,
	DestroyPlaybook:   roles.PlaybookWorkflowInfraDestroy,
	ArtifactsBaseName: "infra",
	AnsibleLimit:      infraAnsibleLimit,
}

var ClustersScope = Scope{
	Name:                    "clusters",
	Short:                   "Provision storage, OpenShift clusters, add-ons, and integrations",
	PhaseNames:              []string{PhaseDeps, PhaseBase, PhaseAddons},
	ApplyPlaybook:           roles.PlaybookWorkflowClustersApply,
	DestroyPlaybook:         roles.PlaybookWorkflowClustersDestroy,
	ArtifactsBaseName:       "clusters",
	TargetsContainerInstall: true,
	AnsibleLimit:            clustersAnsibleLimit,
}

var ContainerClusterScope = Scope{
	Name:                    "container-cluster",
	Short:                   "Install and configure managed OpenShift clusters via openshift-install agent",
	PhaseNames:              []string{PhaseDeps, PhaseBase},
	ApplyPhaseNames:         []string{PhaseDeps, PhaseBase, PhaseAddons},
	ClusterKind:             workflow.ApplyClusterKindContainer,
	ApplyPlaybook:           roles.PlaybookWorkflowContainerClusterApply,
	DestroyPlaybook:         roles.PlaybookWorkflowClustersDestroy,
	ArtifactsBaseName:       "container-cluster",
	TargetsContainerInstall: true,
	AnsibleLimit:            clusterAnsibleLimit,
}

var StorageClusterScope = Scope{
	Name:              "storage-cluster",
	Short:             "Provision external storage clusters",
	PhaseNames:        []string{PhaseDeps, PhaseBase},
	ArtifactsBaseName: "storage-cluster",
	ClusterKind:       workflow.ApplyClusterKindStorage,
	NoAnsible:         true,
}

var AllScope = Scope{
	Name:                    "all",
	Short:                   "Apply infrastructure, storage, OpenShift clusters, and add-ons",
	PhaseNames:              []string{PhaseFabric, PhaseMachines, PhaseDeps, PhaseBase, PhaseAddons},
	ApplyPlaybook:           roles.PlaybookWorkflowAllApply,
	ArtifactsBaseName:       "all",
	TargetsContainerInstall: true,
}

func (s Scope) Phases() []Phase {
	return scopePhases(s.PhaseNames)
}

func (s Scope) ApplyPhases() []Phase {
	names := s.PhaseNames
	if len(s.ApplyPhaseNames) > 0 {
		names = s.ApplyPhaseNames
	}
	return scopePhases(names)
}

func (s Scope) ApplyTarget() workflow.ApplyTarget {
	names := s.PhaseNames
	if len(s.ApplyPhaseNames) > 0 {
		names = s.ApplyPhaseNames
	}
	return workflow.ApplyTarget{Name: s.Name, PhaseNames: append([]string(nil), names...), ClusterKind: s.ClusterKind}
}

func scopePhases(names []string) []Phase {
	out := make([]Phase, 0, len(names))
	for _, name := range names {
		out = append(out, phases[name])
	}
	return out
}

const PreflightPlaybook = roles.PlaybookCheckPreflight

func ansibleLimit(groups ...string) string { return strings.Join(groups, ":") }

var (
	infraAnsibleLimit    = ansibleLimit(render.GroupProviderHosts, render.GroupInfraComponentHosts, render.GroupInfraHosts, render.GroupOCPHosts)
	clustersAnsibleLimit = ansibleLimit(render.GroupInfraHosts, render.GroupOCPHosts, render.GroupBootHosts, render.GroupStorageHosts)
	clusterAnsibleLimit  = ansibleLimit(render.GroupOCPHosts, render.GroupBootHosts)
)
