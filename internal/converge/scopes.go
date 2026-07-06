// Package converge is the bounded-context root for apply/destroy
// orchestration: the scope model, phase selection, workflow planning, and
// run execution that the CLI sequences. Presentation (printing, prompts,
// confirmation flows) stays in internal/cli.
package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
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
	// Per-scope policy, declared with the scope rather than in separate
	// name-keyed switches: NoAnsible marks scopes whose work runs no ansible
	// (addons, storage-cluster); TargetsContainerInstall marks scopes that drive
	// the openshift-install agent; AnsibleLimit is the inventory --limit the
	// scope targets ("" means no limit / all groups).
	NoAnsible               bool
	TargetsContainerInstall bool
	AnsibleLimit            string
}

// Phase names of the five sub-phases, aliased from the workflow package's
// canonical constants so the scope definitions, the phases map, and the
// sub-phase/limit switches all reference one spelling. A typo becomes a
// compile error rather than a silently mis-mapped phase.
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

// ansibleLimit joins inventory group names into an Ansible `--limit` expression.
// The group names come from render (the single owner of inventory group
// vocabulary), so a renamed group is a compile error here rather than a silently
// stale literal.
func ansibleLimit(groups ...string) string { return strings.Join(groups, ":") }

// Ansible --limit groups shared across scope command builders. Centralised so a
// single edit reaches all three (check/apply/destroy).
var (
	// infraAnsibleLimit pins the inventory groups `apply --stage infra` and
	// `check infra` target. GroupOCPHosts is included so bastion-side
	// external_validate can run in every context input set, including
	// bare-metal/all-external shapes like test 002 where the other remote
	// groups would otherwise be empty and ansible would abort with "no hosts to
	// target".
	infraAnsibleLimit    = ansibleLimit(render.GroupProviderHosts, render.GroupInfraComponentHosts, render.GroupInfraHosts, render.GroupOCPHosts)
	clustersAnsibleLimit = ansibleLimit(render.GroupInfraHosts, render.GroupOCPHosts, render.GroupBootHosts, render.GroupStorageHosts)
	clusterAnsibleLimit  = ansibleLimit(render.GroupOCPHosts, render.GroupBootHosts)
)
