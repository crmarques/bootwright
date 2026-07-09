package ceph

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func CephOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]any {
	var ops []map[string]any
	ops = append(ops, cephTopologyOperations(cluster)...)
	ops = append(ops, cephCrushRuleOperations(state, cluster)...)
	ops = append(ops, cephPoolOperations(state, cluster)...)
	ops = append(ops, cephFilesystemOperations(state, cluster)...)
	ops = append(ops, cephMgrAndLoggingOperations(cluster)...)
	ops = append(ops, cephObjectGatewayOperations(state, cluster)...)
	ops = append(ops, nfsExportOperations(state, cluster)...)
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
