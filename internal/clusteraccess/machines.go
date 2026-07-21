package clusteraccess

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func ResolveMachines(state v1alpha1.State, scope string) (Selection, error) {
	names, err := MachineScopeNames(state, scope)
	if err != nil {
		return Selection{}, err
	}
	provision, hosts := stategraph.MachineWorkObjects(state, names)
	container, storage := stategraph.MachineOwningClusterRoots(state, provision)
	storageSet := make(map[string]bool, len(storage))
	for _, name := range storage {
		storageSet[name] = true
	}
	return Selection{
		Target:              "machines",
		Scope:               scope,
		Active:              true,
		RenderState:         state,
		WorkMachines:        hosts,
		WorkStorageClusters: storageSet,
		ContainerRoots:      container,
		StorageRoots:        storage,
		AllRoots:            append(append([]string{}, container...), storage...),
		MachineSelection:    true,
		MachineProvision:    provision,
		MachineHosts:        hosts,
	}, nil
}

func MachineScopeNames(state v1alpha1.State, scope string) ([]string, error) {
	names, err := parseMachineScope(scope)
	if err != nil {
		return nil, err
	}
	if err := ValidateMachineNames(state, names); err != nil {
		return nil, err
	}
	return names, nil
}

func parseMachineScope(scope string) ([]string, error) {
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
		return nil, fmt.Errorf("machine selection must name at least one machine")
	}
	return names, nil
}

func ValidateMachineNames(state v1alpha1.State, names []string) error {
	known := map[string]bool{}
	for _, machine := range state.Machines {
		known[machine.Metadata.Name] = true
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
	available := make([]string, 0, len(known))
	for name := range known {
		available = append(available, name)
	}
	return fmt.Errorf("unknown machine(s): %s; %s", strings.Join(missing, ", "), availableMachineNamesHint(available))
}

func availableMachineNamesHint(names []string) string {
	if len(names) == 0 {
		return "no Machines are declared"
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return "available: " + strings.Join(sorted, ", ")
}
