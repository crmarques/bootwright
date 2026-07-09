package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	"go.yaml.in/yaml/v3"
)

func planNodeConfigActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool) error {
	for _, ocp := range state.ContainerClusters {
		if !clusterNeedsNodeConfig(ocp) {
			continue
		}
		cluster := ocp.Metadata.Name
		deps := []string{}
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

func clusterNeedsNodeConfig(ocp v1alpha1.ContainerCluster) bool {
	for _, host := range ocp.Spec.Hosts {
		if host.Role == v1alpha1.NodeRoleInfra || len(host.Labels) > 0 || len(host.Taints) > 0 {
			return true
		}
	}
	return false
}

func nodeConfigManifests(ocp v1alpha1.ContainerCluster) ([]byte, error) {
	var docs []any
	hasInfra := false
	for _, host := range ocp.Spec.Hosts {
		infra := host.Role == v1alpha1.NodeRoleInfra
		if infra {
			hasInfra = true
		}
		labels := map[string]string{}
		for k, v := range host.Labels {
			labels[k] = v
		}
		if infra {
			labels[v1alpha1.InfraNodeRoleLabel] = ""
		}
		taints := append([]v1alpha1.OCPNodeTaint(nil), host.Taints...)
		if infra {
			taints = append(taints, v1alpha1.OCPNodeTaint{Key: v1alpha1.InfraNodeRoleLabel, Effect: v1alpha1.TaintEffectNoSchedule})
		}
		if len(labels) == 0 && len(taints) == 0 {
			continue
		}
		docs = append(docs, nodePatchManifest(host.Hostname, labels, taints))
	}
	if hasInfra {
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
		"metadata":   map[string]any{"name": "infra"},
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
