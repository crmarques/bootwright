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

const ThroughEndSentinel = "end"

func ApplyThroughNames() []string { return append(ApplyStageNames(), ThroughEndSentinel) }

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
		return Scope{}, fmt.Errorf("--through must be one of %s", strings.Join(ApplyThroughNames(), ", "))
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
	case ThroughEndSentinel:
		return AllScope.PhaseNames[len(AllScope.PhaseNames)-1], true
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

func stageStartPhase(stage string) (string, bool) {
	switch stage {
	case "infra":
		return PhaseFabric, true
	case "clusters":
		return PhaseDeps, true
	default:
		if slices.Contains(SubPhaseStageNames(), stage) {
			return stage, true
		}
		return "", false
	}
}

func ApplyRangeScope(stage, through string) (Scope, error) {
	s := strings.TrimSpace(stage)
	t := strings.TrimSpace(through)

	startIdx := 0
	if s != "" {
		start, ok := stageStartPhase(s)
		if !ok {
			return Scope{}, fmt.Errorf("--stage must be one of %s", strings.Join(ApplyStageNames(), ", "))
		}
		startIdx = slices.Index(AllScope.PhaseNames, start)
	}

	endIdx := len(AllScope.PhaseNames) - 1
	switch {
	case t != "":
		end, ok := throughEndPhase(t)
		if !ok {
			return Scope{}, fmt.Errorf("--through must be one of %s", strings.Join(ApplyThroughNames(), ", "))
		}
		endIdx = slices.Index(AllScope.PhaseNames, end)
	case s != "":
		end, _ := throughEndPhase(s)
		endIdx = slices.Index(AllScope.PhaseNames, end)
	}

	if startIdx > endIdx {
		return Scope{}, fmt.Errorf("--stage %s starts after --through %s: the start stage must not come after the end stage", s, t)
	}
	return stageRangeScope(startIdx, endIdx), nil
}

func stageRangeScope(startIdx, endIdx int) Scope {
	names := append([]string(nil), AllScope.PhaseNames[startIdx:endIdx+1]...)
	switch {
	case slices.Equal(names, AllScope.PhaseNames):
		return AllScope
	case slices.Equal(names, InfraScope.PhaseNames):
		return InfraScope
	case slices.Equal(names, ClustersScope.PhaseNames):
		return ClustersScope
	case len(names) == 1:
		return subPhaseStageScope(names[0])
	case names[0] == AllScope.PhaseNames[0]:
		return prefixStageScope(names)
	default:
		return midRangeScope(names)
	}
}

func midRangeScope(names []string) Scope {
	reachesInfra := slices.Contains(names, PhaseFabric) || slices.Contains(names, PhaseMachines)
	reachesClusters := slices.Contains(names, PhaseDeps) || slices.Contains(names, PhaseBase) || slices.Contains(names, PhaseAddons)
	limit := ""
	switch {
	case !reachesClusters:
		limit = infraAnsibleLimit
	case !reachesInfra:
		limit = clustersAnsibleLimit
	}
	name := "range-" + names[0] + "-" + names[len(names)-1]
	return Scope{
		Name:                    name,
		PhaseNames:              names,
		ArtifactsBaseName:       name,
		NoAnsible:               false,
		TargetsContainerInstall: reachesClusters,
		AnsibleLimit:            limit,
	}
}

func ApplyRangeCommandLabel(stage, through, action, defaultLabel string) string {
	s := strings.TrimSpace(stage)
	t := strings.TrimSpace(through)
	switch {
	case s != "" && t != "":
		return s + " through " + t + " " + action
	case t != "":
		return "through " + t + " " + action
	case s != "":
		return s + " " + action
	default:
		return defaultLabel
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

func ScopeIsMachineLayerOnly(scope Scope) bool {
	if len(scope.PhaseNames) == 0 {
		return false
	}
	for _, name := range scope.PhaseNames {
		if name != PhaseFabric && name != PhaseMachines {
			return false
		}
	}
	return true
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
