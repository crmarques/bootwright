package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	selected := map[string]bool{}
	for _, name := range names {
		selected[name] = true
	}
	var clusters []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if selected[cluster.Metadata.Name] {
			clusters = append(clusters, cluster)
		}
	}
	var policies []v1alpha1.StoragePlacementPolicy
	for _, policy := range state.StoragePlacementPolicies {
		if selected[policy.Spec.StorageClusterRef.Name] {
			policies = append(policies, policy)
		}
	}
	var pools []v1alpha1.StoragePool
	for _, pool := range state.StoragePools {
		if selected[pool.Spec.StorageClusterRef.Name] {
			pools = append(pools, pool)
		}
	}
	var filesystems []v1alpha1.StorageFilesystem
	for _, fs := range state.StorageFilesystems {
		if selected[fs.Spec.StorageClusterRef.Name] {
			filesystems = append(filesystems, fs)
		}
	}
	var gateways []v1alpha1.StorageObjectGateway
	for _, gateway := range state.StorageObjectGateways {
		if selected[gateway.Spec.StorageClusterRef.Name] {
			gateways = append(gateways, gateway)
		}
	}
	var exports []v1alpha1.StorageExport
	selectedExports := map[string]bool{}
	for _, export := range state.StorageExports {
		if selected[export.Spec.StorageClusterRef.Name] {
			exports = append(exports, export)
			selectedExports[export.Metadata.Name] = true
		}
	}
	var bindings []v1alpha1.StorageClusterBinding
	for _, binding := range state.StorageClusterBindings {
		if selectedExports[binding.Spec.StorageExportRef.Name] {
			bindings = append(bindings, binding)
		}
	}
	state.StorageClusters = clusters
	state.StoragePlacementPolicies = policies
	state.StoragePools = pools
	state.StorageFilesystems = filesystems
	state.StorageObjectGateways = gateways
	state.StorageExports = exports
	state.StorageClusterBindings = bindings
	return state
}
