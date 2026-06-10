// Package converge is the bounded-context root for apply/destroy
// orchestration: the scope model, phase selection, workflow planning, and
// run execution that the CLI sequences. Presentation (printing, prompts,
// confirmation flows) stays in internal/cli.
package converge

import (
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/roles"
)

type Scope struct {
	Name              string
	Short             string
	PhaseNames        []string
	ApplyPhaseNames   []string
	ApplyPlaybook     string
	DestroyPlaybook   string
	ArtifactsBaseName string
	// ClusterKind restricts the shared deps/base/addons gates to one cluster
	// kind for the single-kind scopes; empty plans both. Mirrors
	// workflow.ApplyClusterKind{Storage,Container}.
	ClusterKind string
}

var InfraScope = Scope{
	Name:              "infra",
	Short:             "Install and configure InfraProvider, InfraComponent, and ClusterInstall",
	PhaseNames:        []string{"fabric", "machines"},
	ApplyPlaybook:     roles.PlaybookWorkflowInfraApply,
	DestroyPlaybook:   roles.PlaybookWorkflowInfraDestroy,
	ArtifactsBaseName: "infra",
}

var ClustersScope = Scope{
	Name:              "clusters",
	Short:             "Provision storage, OpenShift clusters, addons, and integrations",
	PhaseNames:        []string{"deps", "base", "addons"},
	ApplyPlaybook:     roles.PlaybookWorkflowClustersApply,
	DestroyPlaybook:   roles.PlaybookWorkflowClustersDestroy,
	ArtifactsBaseName: "clusters",
}

var ContainerClusterScope = Scope{
	Name:              "container-cluster",
	Short:             "Install and configure managed OpenShift clusters via openshift-install agent",
	PhaseNames:        []string{"deps", "base"},
	ApplyPhaseNames:   []string{"deps", "base", "addons"},
	ClusterKind:       workflow.ApplyClusterKindContainer,
	ApplyPlaybook:     roles.PlaybookWorkflowContainerClusterApply,
	DestroyPlaybook:   roles.PlaybookWorkflowClustersDestroy,
	ArtifactsBaseName: "container-cluster",
}

var StorageClusterScope = Scope{
	Name:              "storage-cluster",
	Short:             "Provision external storage clusters",
	PhaseNames:        []string{"deps", "base"},
	ArtifactsBaseName: "storage-cluster",
	ClusterKind:       workflow.ApplyClusterKindStorage,
}

var AddonsScope = Scope{
	Name:              "addons",
	Short:             "Apply post-install cluster addons",
	PhaseNames:        []string{"addons"},
	ArtifactsBaseName: "addons",
}

var AllScope = Scope{
	Name:              "all",
	Short:             "Apply infrastructure, storage, OpenShift clusters, and addons",
	PhaseNames:        []string{"fabric", "machines", "deps", "base", "addons"},
	ApplyPlaybook:     roles.PlaybookWorkflowAllApply,
	ArtifactsBaseName: "all",
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

// Paths and Ansible --limit groups shared across scope command builders.
// Centralised so a single edit reaches all three (check/apply/destroy).
const (
	PreflightPlaybook = roles.PlaybookCheckPreflight
	// infraAnsibleLimit pins the inventory groups `apply --stage infra` and
	// `check infra` target. `bootwright_ocp_hosts` is included so
	// bastion-side external_validate can run in every context input set,
	// including bare-metal/all-external shapes like test 002 where the
	// other remote groups would otherwise be empty and ansible would abort
	// with "no hosts to target".
	infraAnsibleLimit    = "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts:bootwright_ocp_hosts"
	clustersAnsibleLimit = "bootwright_infra_hosts:bootwright_ocp_hosts:bootwright_boot_hosts:bootwright_storage_hosts"
	clusterAnsibleLimit  = "bootwright_ocp_hosts:bootwright_boot_hosts"
)

func AnsibleLimitForScope(name string) string {
	switch name {
	case "infra", "fabric", "machines":
		return infraAnsibleLimit
	case "clusters", "deps", "base":
		return clustersAnsibleLimit
	case "container-cluster":
		return clusterAnsibleLimit
	default:
		return ""
	}
}
