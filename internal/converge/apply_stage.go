package converge

import (
	"fmt"
	"slices"
	"strings"
)

// Canonical --stage vocabularies, the single source the error messages, flag
// help, and shell completion all derive from so the three never drift.
// apply/plan/state-check accept both families and the five sub-phases; destroy
// accepts the families only — a sub-phase has no single destroy playbook.
// Each accessor returns a fresh slice so callers may append freely.
func FamilyStageNames() []string { return []string{"infra", "clusters"} }
func SubPhaseStageNames() []string {
	return []string{PhaseFabric, PhaseMachines, PhaseDeps, PhaseBase, PhaseAddons}
}

// ApplyStageNames is every value apply/plan/state-check accept (families first,
// then sub-phases); DestroyStageNames is destroy's family-only set.
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
			// Sub-phase stages run a single phase via the task graph for surgical
			// reruns; the family stages (infra, clusters) remain the common path.
			return subPhaseStageScope(s), nil
		}
		return Scope{}, fmt.Errorf("--stage must be one of %s", strings.Join(ApplyStageNames(), ", "))
	}
}

// subPhaseStageScope builds the scope that selects exactly one sub-phase. It is
// the single constructor for every sub-phase scope; ApplyStageScope only calls
// it for a validated sub-phase name, so a sub-phase has no second hand-written
// Scope var. Apply runs it through the task graph (PlanApplyTasksChecked), so no
// per-phase ApplyPlaybook is needed; artifacts are keyed by the phase name.
func subPhaseStageScope(name string) Scope {
	return Scope{
		Name:              name,
		PhaseNames:        []string{name},
		ArtifactsBaseName: name,
		NoAnsible:         name == PhaseAddons,
		AnsibleLimit:      phases[name].AnsibleLimit,
	}
}

// StageScopeOmissions reports, for a resolved apply/plan scope, which canonical
// phases the plan does NOT include (omitted) and which of those precede the
// selected work in canonical order (assumedPrior — the run reuses what an earlier
// apply produced rather than building it now). Both are empty for the full graph.
// Used to annotate dry-run/plan output so a narrow --stage selection is not
// misread as a full plan.
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

// ScopeProvisionsClusterWorkload reports whether the scope's phases include a
// phase that provisions onto cluster nodes — machine instantiation (machines)
// or the deps/base cluster phases — i.e. the phases whose --clusters selection
// needs the KubeVirt host cluster to be ready. fabric (provider/network) and
// addons (post-install manifests) do not, so selecting one of those sub-phases
// alone is not gated; a family scope that contains them still gates via its
// machines/deps/base phases.
func ScopeProvisionsClusterWorkload(scope Scope) bool {
	for _, name := range scope.PhaseNames {
		switch name {
		case PhaseMachines, PhaseDeps, PhaseBase:
			return true
		}
	}
	return false
}
