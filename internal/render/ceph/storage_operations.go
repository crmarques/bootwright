package ceph

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// CephOperations assembles the StorageOperations document for a managed cluster
// by appending each operation family in rendered order: the cluster topology
// (networks/config/stretch), the CRUSH rules, the pools, the filesystems, the
// mgr-module/logging wiring, the object-gateway realm/zone/admin ops, the NFS
// exports, and the data-foundation credential captures. Ordering across families
// is significant — the apply role runs operations in slice order by phase.
func CephOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]any {
	var ops []map[string]any
	ops = append(ops, cephTopologyOperations(cluster)...)
	ops = append(ops, cephCrushRuleOperations(state, cluster)...)
	ops = append(ops, cephPoolOperations(state, cluster)...)
	ops = append(ops, cephFilesystemOperations(state, cluster)...)
	ops = append(ops, cephMgrAndLoggingOperations(cluster)...)
	ops = append(ops, cephObjectGatewayOperations(state, cluster)...)
	ops = append(ops, nfsExportOperations(state, cluster)...)
	ops = append(ops, dataFoundationCredentialOperations(state, cluster)...)
	return map[string]any{
		"apiVersion": "bootwright.io/v1alpha1",
		"kind":       "StorageOperations",
		"metadata": map[string]any{
			"name": cluster.Metadata.Name,
		},
		"operations": ops,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func operationInPhase(phase, name string, command ...string) map[string]any {
	if command == nil {
		// A structured operation (role-implemented, no argv) must render
		// `command: []`, not null: the role filters on `command | length`.
		command = []string{}
	}
	return map[string]any{
		"phase":   phase,
		"name":    name,
		"command": command,
	}
}

func operationWithIdempotency(phase, name, kind, resourceName string, command ...string) map[string]any {
	op := operationInPhase(phase, name, command...)
	op["idempotency"] = map[string]any{
		"kind": kind,
		"name": resourceName,
	}
	return op
}
