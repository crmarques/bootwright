package cli

import (
	"fmt"
	"strings"
)

func applyStageScope(stage string) (scopeSpec, error) {
	switch s := strings.TrimSpace(stage); s {
	case "":
		return allScope, nil
	case "infra":
		return infraScope, nil
	case "clusters":
		return clustersScope, nil
	case "fabric", "machines", "deps", "base", "addons":
		// Sub-phase stages run a single phase via the task graph for surgical
		// reruns; the family stages (infra, clusters) remain the common path.
		return subPhaseStageScope(s), nil
	default:
		return scopeSpec{}, fmt.Errorf("--stage must be one of infra, clusters, fabric, machines, deps, base, addons")
	}
}

// subPhaseStageScope builds an ad-hoc scope that selects exactly one sub-phase.
// Apply runs through the task graph (PlanApplyTasksChecked), so no per-phase
// applyPlaybook is needed; artifacts are keyed by the phase name.
func subPhaseStageScope(name string) scopeSpec {
	return scopeSpec{
		name:              name,
		phaseNames:        []string{name},
		artifactsBaseName: name,
	}
}

func applyStageCommandLabel(stage, action, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " " + action
}

func scopeUsesAnsible(scope scopeSpec) bool {
	return scope.name != "addons" && scope.name != "storage-cluster"
}

func scopeTargetsContainerInstall(scope scopeSpec) bool {
	switch scope.name {
	case "clusters", "container-cluster", "all":
		return true
	default:
		return false
	}
}
