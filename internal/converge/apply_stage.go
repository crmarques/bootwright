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

// ApplyThroughScope builds a cumulative prefix scope: every canonical phase from
// fabric up to and including the named endpoint, in AllScope order. It is the
// --through counterpart to ApplyStageScope's exact-slice --stage. A family
// endpoint resolves to the last phase of that family (infra->machines,
// clusters->addons); "" means the full graph. Where a prefix coincides with an
// existing named scope it returns that scope unchanged so playbook, artifact
// name, and limit stay canonical. Because the prefix always begins at fabric,
// StageScopeOmissions reports no assumedPrior phases for any --through scope.
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
	prefix := append([]string(nil), AllScope.PhaseNames[:idx+1]...) // own the slice; fabric..end inclusive
	switch {
	case len(prefix) == len(AllScope.PhaseNames):
		return AllScope, nil // through addons / through clusters
	case slices.Equal(prefix, InfraScope.PhaseNames):
		return InfraScope, nil // through machines / through infra
	default:
		return prefixStageScope(prefix), nil // through fabric / through deps / through base
	}
}

// throughEndPhase maps a --through endpoint token to its last canonical phase.
// Families collapse to their final phase; a sub-phase is its own endpoint.
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

// prefixStageScope is the single constructor for a from-beginning prefix that
// does NOT coincide with InfraScope/AllScope (it ends at fabric, deps, or base).
// Apply runs it through the task graph (PlanApplyTasksChecked filters by
// PhaseNames), so no per-prefix ApplyPlaybook is needed — identical to
// subPhaseStageScope. A prefix that reaches deps spans both the infra and
// clusters host groups, so it adopts AllScope's empty AnsibleLimit (= all
// groups) rather than a single family's limit; a prefix wholly within infra
// ([fabric]) keeps infraAnsibleLimit.
func prefixStageScope(prefix []string) Scope {
	end := prefix[len(prefix)-1]
	reachesClusters := slices.Contains(prefix, PhaseDeps) // prefix always contains fabric
	limit := infraAnsibleLimit
	if reachesClusters {
		limit = ""
	}
	return Scope{
		Name:                    "through-" + end,
		PhaseNames:              prefix,
		ArtifactsBaseName:       "through-" + end,
		NoAnsible:               false,           // fabric is always present and uses ansible
		TargetsContainerInstall: reachesClusters, // deps/base drive openshift-install
		AnsibleLimit:            limit,
	}
}

// ApplyThroughCommandLabel mirrors ApplyStageCommandLabel but prefixes "through "
// so the run banner distinguishes a cumulative prefix from a --stage slice.
func ApplyThroughCommandLabel(stage, action, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return "through " + strings.TrimSpace(stage) + " " + action
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
