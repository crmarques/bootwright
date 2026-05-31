package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func storageClusterNamesForTarget(state v1alpha1.State, scope string) ([]string, error) {
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

func filterStateToStorageClusters(state v1alpha1.State, names []string) v1alpha1.State {
	return stategraph.FilterStateToStorageClusters(state, names)
}
