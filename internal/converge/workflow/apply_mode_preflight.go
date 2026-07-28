package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func EvaluateApplyModePreflight(mode ApplyMode, objects []ObjectClassification) error {
	switch mode {
	case ApplyModeCreate:
		var existing []ObjectClassification
		for _, o := range objects {
			if o.Recorded() {
				existing = append(existing, o)
			}
		}
		if len(existing) > 0 {
			return fmt.Errorf("apply --expect-new requires a greenfield environment and these objects already exist: %s; drop --expect-new to reconcile them, or run `bootwright apply --converge-drifted` to rebuild drifted objects", summarizeApplyObjects(existing))
		}
	case ApplyModeContinue:
		var drifted, foreign []ObjectClassification
		for _, o := range objects {
			switch {
			case o.HasForeign():
				foreign = append(foreign, o)
			case o.HasStructuralDrift():
				drifted = append(drifted, o)
			}
		}
		if len(drifted)+len(foreign) > 0 {
			return continueDriftRefusal(drifted, foreign)
		}
	case ApplyModeOverride:
		var foreign []ObjectClassification
		for _, o := range objects {
			if o.HasForeign() {
				foreign = append(foreign, o)
			}
		}
		if len(foreign) > 0 {
			return fmt.Errorf("apply --converge-drifted never rebuilds objects recorded by another manager: %s; resolve ownership before retrying", summarizeApplyObjects(foreign))
		}
	}
	return nil
}

func continueDriftRefusal(drifted, foreign []ObjectClassification) error {
	var parts []string
	if len(drifted) > 0 {
		items := make([]string, 0, len(drifted))
		for _, o := range drifted {
			items = append(items, fmt.Sprintf("%s (would %s)", o.Label, structuralRebuildConsequence(o)))
		}
		sort.Strings(items)
		parts = append(parts, fmt.Sprintf("apply refuses this change to %s: it is not a safe in-place reconcile. To proceed, revert the change to match the recorded desired state, or — if you intend the rebuild — re-run with `bootwright apply --converge-drifted` (a destructive rebuild additionally needs `--confirm-data-loss`, or on a protected environment a `bootwright destroy` for that scope first)", strings.Join(items, ", ")))
	}
	if len(foreign) > 0 {
		parts = append(parts, fmt.Sprintf("apply never modifies objects recorded by another manager: %s; resolve ownership before retrying (foreign objects are never rebuilt)", summarizeApplyObjects(foreign)))
	}
	return fmt.Errorf("%s", strings.Join(parts, ". "))
}

func structuralRebuildConsequence(o ObjectClassification) string {
	switch {
	case o.Kind == ObjectKindStorageCluster:
		return "wipe all Ceph OSD data (cephadm rm-cluster --zap-osds) and re-bootstrap"
	case IsStorageSubObjectKind(o.Kind):
		return "destroy the data in that pool/filesystem and recreate it"
	case o.Kind == ObjectKindContainerCluster:
		return "reinstall the cluster — its nodes re-imaged"
	case machineSubstrateKinds[o.Kind]:
		return "reinstall the machine — its disks wiped"
	default:
		return "rebuild the resource"
	}
}

func summarizeApplyObjects(objs []ObjectClassification) string {
	parts := make([]string, 0, len(objs))
	for _, o := range objs {
		parts = append(parts, fmt.Sprintf("%s (%s)", o.Label, o.Class))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

var overrideReconfigureOnlyKinds = map[string]bool{
	ApplyTaskKindProvider:               true,
	ApplyTaskKindInfraComponentServices: true,
	ApplyTaskKindNodeConfigApply:        true,
	ApplyTaskKindHostVirtctl:            true,
	ApplyTaskKindClusterAddon:           true,
	ApplyTaskKindMachineRegistration:    true,
	ApplyTaskKindMachineRepositories:    true,
	ApplyTaskKindPlaybook:               true,
}

var machineSubstrateKinds = map[string]bool{
	ApplyTaskKindManagedMachineOS:     true,
	ApplyTaskKindMachineInfraPrepare:  true,
	ApplyTaskKindMachineInfraFinalize: true,
}

func isOverrideDestructive(o ObjectClassification) bool {
	return o.HasStructuralDrift() && !overrideReconfigureOnlyKinds[o.Kind]
}

func OverrideDestructiveDriftedObjects(objects []ObjectClassification) []string {
	var labels []string
	for _, o := range objects {
		if !isOverrideDestructive(o) {
			continue
		}
		labels = append(labels, o.Label)
	}
	sort.Strings(labels)
	return labels
}

func objectProtectedKind(o ObjectClassification) string {
	switch {
	case o.Kind == ObjectKindStorageCluster:
		return v1alpha1.KindStorageCluster
	case IsStorageSubObjectKind(o.Kind):
		return v1alpha1.KindStorageCluster
	case o.Kind == ObjectKindContainerCluster:
		return v1alpha1.KindContainerCluster
	case machineSubstrateKinds[o.Kind]:
		return v1alpha1.KindMachine
	}
	return ""
}

func OverrideDestructiveKindProtected(objects []ObjectClassification, protected map[string]bool) []string {
	var labels []string
	for _, o := range objects {
		if !isOverrideDestructive(o) {
			continue
		}
		if kind := objectProtectedKind(o); kind != "" && protected[kind] {
			labels = append(labels, o.Label)
		}
	}
	sort.Strings(labels)
	return labels
}

func OverrideDestructiveClusterScope(objects []ObjectClassification) []string {
	seen := map[string]bool{}
	var clusters []string
	for _, o := range objects {
		if !isOverrideDestructive(o) || machineSubstrateKinds[o.Kind] || o.Cluster == "" {
			continue
		}
		if !seen[o.Cluster] {
			seen[o.Cluster] = true
			clusters = append(clusters, o.Cluster)
		}
	}
	sort.Strings(clusters)
	return clusters
}

func OverrideDestructiveProtectedClusterScope(objects []ObjectClassification, protected map[string]bool) []string {
	seen := map[string]bool{}
	var clusters []string
	for _, o := range objects {
		if !isOverrideDestructive(o) || machineSubstrateKinds[o.Kind] || o.Cluster == "" {
			continue
		}
		if kind := objectProtectedKind(o); kind == "" || !protected[kind] {
			continue
		}
		if !seen[o.Cluster] {
			seen[o.Cluster] = true
			clusters = append(clusters, o.Cluster)
		}
	}
	sort.Strings(clusters)
	return clusters
}

func OverrideDestructiveMachineSubstrate(objects []ObjectClassification) (labels, clusters []string) {
	seen := map[string]bool{}
	for _, o := range objects {
		if !isOverrideDestructive(o) || !machineSubstrateKinds[o.Kind] {
			continue
		}
		labels = append(labels, o.Label)
		if o.Cluster != "" && !seen[o.Cluster] {
			seen[o.Cluster] = true
			clusters = append(clusters, o.Cluster)
		}
	}
	sort.Strings(labels)
	sort.Strings(clusters)
	return labels, clusters
}
