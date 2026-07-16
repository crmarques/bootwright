package converge

import (
	"fmt"
	"slices"
	"strings"
)

func FamilyStageNames() []string { return []string{"infra", "clusters"} }
func SubPhaseStageNames() []string {
	return []string{PhaseFabric, PhaseMachines, PhaseDeps, PhaseBase, PhaseAddons}
}

func ApplyStageNames() []string   { return append(FamilyStageNames(), SubPhaseStageNames()...) }
func DestroyStageNames() []string { return FamilyStageNames() }

func ApplyStageScope(stage string) (Scope, error) {
	switch s := strings.TrimSpace(stage); s {
	case "":
		return AllScope, nil
	case "infra":
		return InfraScope, nil
	case "clusters":
		return ClustersScope, nil
	default:
		if slices.Contains(SubPhaseStageNames(), s) {
			return subPhaseStageScope(s), nil
		}
		return Scope{}, fmt.Errorf("--stage must be one of %s", strings.Join(ApplyStageNames(), ", "))
	}
}

func ApplyThroughScope(stage string) (Scope, error) {
	s := strings.TrimSpace(stage)
	if s == "" {
		return AllScope, nil
	}
	end, ok := throughEndPhase(s)
	if !ok {
		return Scope{}, fmt.Errorf("--through must be one of %s", strings.Join(ApplyStageNames(), ", "))
	}
	idx := slices.Index(AllScope.PhaseNames, end)
	prefix := append([]string(nil), AllScope.PhaseNames[:idx+1]...)
	switch {
	case len(prefix) == len(AllScope.PhaseNames):
		return AllScope, nil
	case slices.Equal(prefix, InfraScope.PhaseNames):
		return InfraScope, nil
	default:
		return prefixStageScope(prefix), nil
	}
}

func throughEndPhase(stage string) (string, bool) {
	switch stage {
	case "infra":
		return PhaseMachines, true
	case "clusters":
		return PhaseAddons, true
	default:
		if slices.Contains(SubPhaseStageNames(), stage) {
			return stage, true
		}
		return "", false
	}
}

func prefixStageScope(prefix []string) Scope {
	end := prefix[len(prefix)-1]
	reachesClusters := slices.Contains(prefix, PhaseDeps)
	limit := infraAnsibleLimit
	if reachesClusters {
		limit = ""
	}
	return Scope{
		Name:                    "through-" + end,
		PhaseNames:              prefix,
		ArtifactsBaseName:       "through-" + end,
		NoAnsible:               false,
		TargetsContainerInstall: reachesClusters,
		AnsibleLimit:            limit,
	}
}

func ApplyThroughCommandLabel(stage, action, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return "through " + strings.TrimSpace(stage) + " " + action
}

func subPhaseStageScope(name string) Scope {
	return Scope{
		Name:              name,
		PhaseNames:        []string{name},
		ArtifactsBaseName: name,
		NoAnsible:         name == PhaseAddons,
		AnsibleLimit:      phases[name].AnsibleLimit,
	}
}

func StageScopeOmissions(scope Scope) (omitted, assumedPrior []string) {
	selected := make(map[string]bool, len(scope.PhaseNames))
	for _, name := range scope.PhaseNames {
		selected[name] = true
	}
	firstSelected := -1
	for i, name := range AllScope.PhaseNames {
		if selected[name] {
			firstSelected = i
			break
		}
	}
	for i, name := range AllScope.PhaseNames {
		if selected[name] {
			continue
		}
		omitted = append(omitted, name)
		if firstSelected >= 0 && i < firstSelected {
			assumedPrior = append(assumedPrior, name)
		}
	}
	return omitted, assumedPrior
}

func ApplyStageCommandLabel(stage, action, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " " + action
}

func ScopeUsesAnsible(scope Scope) bool {
	return !scope.NoAnsible
}

func ScopeTargetsContainerInstall(scope Scope) bool {
	return scope.TargetsContainerInstall
}

func ScopeProvisionsClusterWorkload(scope Scope) bool {
	for _, name := range scope.PhaseNames {
		switch name {
		case PhaseMachines, PhaseDeps, PhaseBase:
			return true
		}
	}
	return false
}

func ScopeSkipsStorageDeviceGate(scope Scope) bool {
	return slices.Contains(scope.PhaseNames, PhaseBase) && !slices.Contains(scope.PhaseNames, PhaseDeps)
}

func ScopeIncludesApplyPhase(scope Scope, phase string) bool {
	names := scope.PhaseNames
	if len(scope.ApplyPhaseNames) > 0 {
		names = scope.ApplyPhaseNames
	}
	return slices.Contains(names, phase)
}

func ScopeTearsMachineLayer(scope Scope) bool {
	return slices.Contains(scope.PhaseNames, PhaseFabric) || slices.Contains(scope.PhaseNames, PhaseMachines)
}

func ScopeTearsClusterLayer(scope Scope) bool {
	for _, name := range scope.PhaseNames {
		switch name {
		case PhaseDeps, PhaseBase, PhaseAddons:
			return true
		}
	}
	return false
}
