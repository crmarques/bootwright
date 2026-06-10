package converge

import (
	"fmt"
	"strings"
)

func ApplyStageScope(stage string) (Scope, error) {
	switch s := strings.TrimSpace(stage); s {
	case "":
		return AllScope, nil
	case "infra":
		return InfraScope, nil
	case "clusters":
		return ClustersScope, nil
	case "fabric", "machines", "deps", "base", "addons":
		// Sub-phase stages run a single phase via the task graph for surgical
		// reruns; the family stages (infra, clusters) remain the common path.
		return subPhaseStageScope(s), nil
	default:
		return Scope{}, fmt.Errorf("--stage must be one of infra, clusters, fabric, machines, deps, base, addons")
	}
}

// subPhaseStageScope builds an ad-hoc scope that selects exactly one sub-phase.
// Apply runs through the task graph (PlanApplyTasksChecked), so no per-phase
// applyPlaybook is needed; artifacts are keyed by the phase name.
func subPhaseStageScope(name string) Scope {
	return Scope{
		Name:              name,
		PhaseNames:        []string{name},
		ArtifactsBaseName: name,
	}
}

func ApplyStageCommandLabel(stage, action, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " " + action
}

func ScopeUsesAnsible(scope Scope) bool {
	return scope.Name != "addons" && scope.Name != "storage-cluster"
}

func ScopeTargetsContainerInstall(scope Scope) bool {
	switch scope.Name {
	case "clusters", "container-cluster", "all":
		return true
	default:
		return false
	}
}
