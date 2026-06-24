package clusteraccess

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func ScopeState(state v1alpha1.State, target, scope string) (v1alpha1.State, error) {
	switch target {
	case "container-cluster", "addons":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		names, err := ClusterNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToClusters(state, names), nil
	case "storage-cluster":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		names, err := StorageClusterNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToStorageClusters(state, names), nil
	case "clusters", "infra", "all", "fabric", "machines", "deps", "base":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		containerNames, storageNames, err := ClusterRootNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToClusterRoots(state, containerNames, storageNames), nil
	default:
		if strings.TrimSpace(scope) != "" {
			return state, fmt.Errorf("--clusters is not supported for %s", target)
		}
		return state, nil
	}
}

func ScopeStateForApply(state v1alpha1.State, target, scope string) (v1alpha1.State, error) {
	switch target {
	case "clusters", "infra", "all", "fabric", "machines", "deps", "base":
		if strings.TrimSpace(scope) == "" {
			return state, nil
		}
		containerNames, storageNames, err := ClusterRootNamesForTarget(state, scope)
		if err != nil {
			return state, err
		}
		return stategraph.FilterStateToApplyClusterRoots(state, containerNames, storageNames), nil
	default:
		return ScopeState(state, target, scope)
	}
}

func ClusterRootNamesForTarget(state v1alpha1.State, scope string) ([]string, []string, error) {
	names, err := parseClusterScope(scope)
	if err != nil {
		return nil, nil, err
	}
	containerKnown := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		containerKnown[ocp.Metadata.Name] = true
	}
	storageKnown := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		storageKnown[cluster.Metadata.Name] = true
	}
	var containerNames []string
	var storageNames []string
	var missing []string
	for _, name := range names {
		containerOK := containerKnown[name]
		storageOK := storageKnown[name]
		if containerOK {
			containerNames = append(containerNames, name)
		}
		if storageOK {
			storageNames = append(storageNames, name)
		}
		if !containerOK && !storageOK {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		var available []string
		for name := range containerKnown {
			available = append(available, name)
		}
		for name := range storageKnown {
			available = append(available, name)
		}
		return nil, nil, fmt.Errorf("unknown cluster(s): %s; %s", strings.Join(missing, ", "), availableClusterNamesHint(available))
	}
	return containerNames, storageNames, nil
}

// availableClusterNamesHint formats the declared cluster roots for an
// unknown-cluster error so an operator can correct a typo from the message
// alone, mirroring the actionable style of the shared-service conflict errors.
func availableClusterNamesHint(names []string) string {
	if len(names) == 0 {
		return "no ContainerCluster or StorageCluster roots are declared"
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return "available: " + strings.Join(sorted, ", ")
}

func ClusterNamesForTarget(state v1alpha1.State, scope string) ([]string, error) {
	if strings.TrimSpace(scope) != "" {
		names, err := parseClusterScope(scope)
		if err != nil {
			return nil, err
		}
		if err := ValidateClusterNames(state, names); err != nil {
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
		return nil, fmt.Errorf("cluster selection must name at least one cluster")
	}
	return names, nil
}

func ValidateClusterNames(state v1alpha1.State, names []string) error {
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
	var available []string
	for name := range known {
		available = append(available, name)
	}
	return fmt.Errorf("unknown cluster(s): %s; %s", strings.Join(missing, ", "), availableClusterNamesHint(available))
}

// ValidateAccessClusterName validates a --cluster value for the cluster access
// surface. When storage is in scope the name may resolve to a ContainerCluster
// or a StorageCluster; otherwise only container clusters are accepted.
func ValidateAccessClusterName(state v1alpha1.State, name string, includeStorage bool) error {
	if !includeStorage {
		return ValidateClusterNames(state, []string{name})
	}
	known := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		known[ocp.Metadata.Name] = true
	}
	for _, cluster := range state.StorageClusters {
		known[cluster.Metadata.Name] = true
	}
	if known[name] {
		return nil
	}
	var available []string
	for declared := range known {
		available = append(available, declared)
	}
	return fmt.Errorf("unknown cluster(s): %s; %s", name, availableClusterNamesHint(available))
}

// FormatDestroyScopeConflicts builds an actionable error message that
// lists every shared machine service and names the unscoped clusters
// that would break if the destroy proceeded.
func FormatDestroyScopeConflicts(conflicts []stategraph.DestroyScopeConflict, flagName string) error {
	var b strings.Builder
	b.WriteString(flagName + " would destroy shared machine service(s) that other clusters still depend on:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - %s %s/%s shared by scoped {%s} and unscoped {%s}\n",
			c.Slot, c.Provider, c.Name,
			strings.Join(c.ScopedClusters, ", "),
			strings.Join(c.UnscopedClusters, ", ")))
	}
	b.WriteString("re-run without " + flagName + " to destroy everything, or extend " + flagName + " to include the unscoped clusters")
	return fmt.Errorf("%s", b.String())
}

func ValidateScopedApplySharedServices(state v1alpha1.State, target, scope string) error {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	switch target {
	case "infra", "all", "fabric", "machines":
	default:
		return nil
	}
	selectedNames, _, err := ClusterRootNamesForTarget(state, scope)
	if err != nil {
		return err
	}
	if len(selectedNames) == 0 {
		return nil
	}
	conflicts := stategraph.SharedDestroyConflicts(state, selectedNames)
	if len(conflicts) == 0 {
		return nil
	}
	return formatApplyScopeConflicts(conflicts)
}

func formatApplyScopeConflicts(conflicts []stategraph.DestroyScopeConflict) error {
	var b strings.Builder
	b.WriteString("--clusters would narrow shared machine service(s) that other clusters still depend on:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - %s %s/%s shared by scoped {%s} and unscoped {%s}\n",
			c.Slot, c.Provider, c.Name,
			strings.Join(c.ScopedClusters, ", "),
			strings.Join(c.UnscopedClusters, ", ")))
	}
	b.WriteString("re-run without --clusters to apply every consumer, or extend --clusters to include the unscoped clusters")
	return fmt.Errorf("%s", b.String())
}

func FilterStateToClusters(state v1alpha1.State, names []string) v1alpha1.State {
	return stategraph.FilterStateToClusters(state, names)
}

// ApplyWorkObjects returns the Machine and StorageCluster names a scoped apply
// acts on for the given selected cluster roots, excluding render-reference
// pull-ins (a managed StorageCluster reached only through a container cluster's
// data-foundation attachment, and its nodes). Readiness checks use it so those
// render references do not require their own bootstrap secrets.
func ApplyWorkObjects(state v1alpha1.State, containerNames, storageNames []string) (machines map[string]bool, storageClusters map[string]bool) {
	return stategraph.ApplyWorkObjects(state, containerNames, storageNames)
}

func StorageClusterNamesForTarget(state v1alpha1.State, scope string) ([]string, error) {
	if strings.TrimSpace(scope) != "" {
		names, err := parseClusterScope(scope)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, cluster := range state.StorageClusters {
			known[cluster.Metadata.Name] = true
		}
		var missing []string
		for _, name := range names {
			if !known[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("unknown storage cluster(s): %s", strings.Join(missing, ", "))
		}
		return names, nil
	}
	var names []string
	for _, cluster := range state.StorageClusters {
		names = append(names, cluster.Metadata.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no storage clusters found")
	}
	return names, nil
}

func FilterStateToStorageClusters(state v1alpha1.State, names []string) v1alpha1.State {
	return stategraph.FilterStateToStorageClusters(state, names)
}
