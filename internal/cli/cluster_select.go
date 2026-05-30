package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func scopeState(state v1alpha1.State, target, scope string) (v1alpha1.State, error) {
	switch target {
	case "cluster", "infra", "all", "extensions":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		names, err := clusterNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToClusters(state, names), nil
	case "storage":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		names, err := storageClusterNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToStorageClusters(state, names), nil
	default:
		if strings.TrimSpace(scope) != "" {
			return state, fmt.Errorf("--scope is not supported for %s", target)
		}
		return state, nil
	}
}

func clusterNamesForTarget(state v1alpha1.State, scope string) ([]string, error) {
	if strings.TrimSpace(scope) != "" {
		names, err := parseClusterScope(scope)
		if err != nil {
			return nil, err
		}
		if err := validateClusterNames(state, names); err != nil {
			return nil, err
		}
		return names, nil
	}

	var names []string
	for _, ocp := range state.ContainerClusters {
		names = append(names, ocp.Metadata.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no clusters found")
	}
	return names, nil
}

func parseClusterScope(scope string) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, part := range strings.Split(scope, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("--scope must name at least one cluster")
	}
	return names, nil
}

func validateClusterNames(state v1alpha1.State, names []string) error {
	known := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		known[ocp.Metadata.Name] = true
	}
	var missing []string
	for _, name := range names {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("unknown cluster(s): %s", strings.Join(missing, ", "))
}

// formatDestroyScopeConflicts builds an actionable error message that
// lists every shared provider service and names the unscoped clusters
// that would break if the destroy proceeded.
func formatDestroyScopeConflicts(conflicts []stategraph.DestroyScopeConflict) error {
	var b strings.Builder
	b.WriteString("--scope would destroy shared provider service(s) that other clusters still depend on:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - %s %s/%s shared by scoped {%s} and unscoped {%s}\n",
			c.Slot, c.Provider, c.Name,
			strings.Join(c.ScopedClusters, ", "),
			strings.Join(c.UnscopedClusters, ", ")))
	}
	b.WriteString("re-run without --scope to destroy everything, or extend --scope to include the unscoped clusters")
	return fmt.Errorf("%s", b.String())
}

func validateScopedApplySharedServices(state v1alpha1.State, target, scope string) error {
	if strings.TrimSpace(scope) == "" || (target != "infra" && target != "all") {
		return nil
	}
	selectedNames, err := clusterNamesForTarget(state, scope)
	if err != nil {
		return err
	}
	conflicts := stategraph.SharedDestroyConflicts(state, selectedNames)
	if len(conflicts) == 0 {
		return nil
	}
	return formatApplyScopeConflicts(conflicts)
}

func formatApplyScopeConflicts(conflicts []stategraph.DestroyScopeConflict) error {
	var b strings.Builder
	b.WriteString("--scope would narrow shared provider service(s) that other clusters still depend on:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - %s %s/%s shared by scoped {%s} and unscoped {%s}\n",
			c.Slot, c.Provider, c.Name,
			strings.Join(c.ScopedClusters, ", "),
			strings.Join(c.UnscopedClusters, ", ")))
	}
	b.WriteString("re-run without --scope to apply every consumer, or extend --scope to include the unscoped clusters")
	return fmt.Errorf("%s", b.String())
}

func filterStateToClusters(state v1alpha1.State, names []string) v1alpha1.State {
	return stategraph.FilterStateToClusters(state, names)
}
