package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	"go.yaml.in/yaml/v3"
)

func planNodeConfigActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool, machineServiceTaskIDs []string) error {
	for _, ocp := range state.ContainerClusters {
		if !clusterNeedsNodeConfig(ocp) {
			continue
		}
		cluster := ocp.Metadata.Name
		deps := append([]string(nil), machineServiceTaskIDs...)
		if installPhasePlanned {
			deps = append(deps, "wait."+cluster)
		}
		id := "nodeconfig." + cluster + ".apply"
		if err := graph.Add(Activity{
			ID:                   id,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:          id,
					Kind:        ApplyTaskKindNodeConfigApply,
					Label:       "node config " + cluster + " apply",
					Cluster:     cluster,
					ClusterKind: ApplyClusterKindContainer,
					Status:      TaskStatusPending,
				},
				State: stategraph.FilterStateToClusters(state, []string{cluster}),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func StateHasNodeConfigWork(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		if clusterNeedsNodeConfig(ocp) {
			return true
		}
	}
	return false
}

func clusterNeedsNodeConfig(ocp v1alpha1.ContainerCluster) bool {
	return len(ocp.Spec.Nodes) > 0
}

func clusterDeclaresInfraNode(ocp v1alpha1.ContainerCluster) bool {
	for _, host := range ocp.Spec.Nodes {
		if host.Role == v1alpha1.NodeRoleInfra {
			return true
		}
	}
	return false
}

func nodeConfigDesiredLabels(host v1alpha1.OCPNodeSpec) map[string]string {
	labels := map[string]string{}
	for k, v := range host.Labels {
		labels[k] = v
	}
	if host.Role == v1alpha1.NodeRoleInfra {
		labels[v1alpha1.InfraNodeRoleLabel] = ""
	}
	return labels
}

func nodeConfigDesiredTaints(host v1alpha1.OCPNodeSpec) []v1alpha1.OCPNodeTaint {
	taints := append([]v1alpha1.OCPNodeTaint(nil), host.Taints...)
	if host.Role == v1alpha1.NodeRoleInfra {
		taints = append(taints, v1alpha1.OCPNodeTaint{Key: v1alpha1.InfraNodeRoleLabel, Effect: v1alpha1.TaintEffectNoSchedule})
	}
	return dedupeNodeTaints(taints)
}

func dedupeNodeTaints(taints []v1alpha1.OCPNodeTaint) []v1alpha1.OCPNodeTaint {
	seen := map[string]bool{}
	out := make([]v1alpha1.OCPNodeTaint, 0, len(taints))
	for _, taint := range taints {
		identity := taint.Key + "\x00" + taint.Effect
		if seen[identity] {
			continue
		}
		seen[identity] = true
		out = append(out, taint)
	}
	return out
}

func nodeConfigManifests(ocp v1alpha1.ContainerCluster, live map[string]bool) ([]byte, error) {
	var docs []any
	for _, host := range ocp.Spec.Nodes {
		labels := nodeConfigDesiredLabels(host)
		taints := nodeConfigDesiredTaints(host)
		if len(labels) == 0 && len(taints) == 0 && !live[host.Name] {
			continue
		}
		docs = append(docs, nodePatchManifest(host.Name, labels, taints))
	}
	if clusterDeclaresInfraNode(ocp) {
		docs = append(docs, infraMachineConfigPoolManifest())
	}
	if len(docs) == 0 {
		return nil, nil
	}
	var out []byte
	for _, doc := range docs {
		chunk, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, []byte("---\n")...)
		out = append(out, chunk...)
	}
	return out, nil
}

func nodePatchManifest(name string, labels map[string]string, taints []v1alpha1.OCPNodeTaint) map[string]any {
	meta := map[string]any{"name": name}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata":   meta,
	}
	if len(taints) > 0 {
		rendered := make([]any, 0, len(taints))
		sorted := append([]v1alpha1.OCPNodeTaint(nil), taints...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Key != sorted[j].Key {
				return sorted[i].Key < sorted[j].Key
			}
			return sorted[i].Effect < sorted[j].Effect
		})
		for _, taint := range sorted {
			entry := map[string]any{"key": taint.Key, "effect": taint.Effect}
			if taint.Value != "" {
				entry["value"] = taint.Value
			}
			rendered = append(rendered, entry)
		}
		manifest["spec"] = map[string]any{"taints": rendered}
	}
	return manifest
}

func infraMachineConfigPoolManifest() map[string]any {
	return map[string]any{
		"apiVersion": "machineconfiguration.openshift.io/v1",
		"kind":       "MachineConfigPool",
		"metadata": map[string]any{
			"name":   "infra",
			"labels": map[string]any{v1alpha1.ManagedByLabel: v1alpha1.ManagedByLabelValue},
		},
		"spec": map[string]any{
			"machineConfigSelector": map[string]any{
				"matchExpressions": []any{
					map[string]any{
						"key":      "machineconfiguration.openshift.io/role",
						"operator": "In",
						"values":   []any{v1alpha1.NodeRoleWorker, v1alpha1.NodeRoleInfra},
					},
				},
			},
			"nodeSelector": map[string]any{
				"matchLabels": map[string]any{v1alpha1.InfraNodeRoleLabel: ""},
			},
		},
	}
}
