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
			return fmt.Errorf("apply --mode create requires a greenfield environment and these objects already exist: %s; use --mode reconcile to reconcile them, or run `bootwright apply --mode rebuild` to rebuild drifted objects", summarizeApplyObjects(existing))
		}
	case ApplyModeReconcile:
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
	case ApplyModeRebuild:
		var foreign []ObjectClassification
		for _, o := range objects {
			if o.HasForeign() {
				foreign = append(foreign, o)
			}
		}
		if len(foreign) > 0 {
			return fmt.Errorf("apply --mode rebuild never rebuilds objects recorded by another manager: %s; resolve ownership before retrying", summarizeApplyObjects(foreign))
		}
	}
	return nil
}

func continueDriftRefusal(drifted, foreign []ObjectClassification) error {
	var parts []string
	switch {
	case len(drifted) > 0 && stretchArbiterShapeDriftSet(drifted):
		parts = append(parts, stretchArbiterShapeRefusal(drifted))
	case len(drifted) > 0 && tiebreakerOnlyDriftSet(drifted):
		parts = append(parts, tiebreakerDriftRefusal(drifted))
	case len(drifted) > 0:
		items := make([]string, 0, len(drifted))
		for _, o := range drifted {
			items = append(items, fmt.Sprintf("%s (would %s)", o.Label, structuralRebuildConsequence(o)))
		}
		sort.Strings(items)
		parts = append(parts, fmt.Sprintf("apply refuses this change to %s: it is not a safe in-place reconcile. To proceed, revert the change to match the recorded desired state, or — if you intend the rebuild — re-run with `bootwright apply --mode rebuild` (a destructive rebuild additionally needs `--authorize data-loss`, or on a protected environment a `bootwright destroy` for that scope first)", strings.Join(items, ", ")))
	}
	if len(foreign) > 0 {
		parts = append(parts, fmt.Sprintf("apply never modifies objects recorded by another manager: %s; resolve ownership before retrying (foreign objects are never rebuilt)", summarizeApplyObjects(foreign)))
	}
	return fmt.Errorf("%s", strings.Join(parts, ". "))
}

func tiebreakerOnlyDriftSet(drifted []ObjectClassification) bool {
	named := 0
	for _, o := range drifted {
		if !o.TiebreakerOnlyStructuralDrift() {
			return false
		}
		if storageClusterObjectName(o) != "" {
			named++
		}
	}
	return named == len(drifted) && named > 0
}

func TiebreakerReplacementCommand(cluster string) string {
	return "bootwright storage-cluster replace-arbiter --name " + cluster
}

func WithoutTiebreakerDrift(objects []ObjectClassification, cluster string) []ObjectClassification {
	kept := make([]ObjectClassification, 0, len(objects))
	for _, o := range objects {
		if o.Cluster == cluster && o.TiebreakerOnlyStructuralDrift() {
			continue
		}
		kept = append(kept, o)
	}
	return kept
}

func stretchArbiterShapeDriftSet(drifted []ObjectClassification) bool {
	named := 0
	for _, o := range drifted {
		if !o.StretchArbiterShapeChange() {
			return false
		}
		if storageClusterObjectName(o) != "" {
			named++
		}
	}
	return named == len(drifted) && named > 0
}

func stretchArbiterShapeRefusal(drifted []ObjectClassification) string {
	labels := make([]string, 0, len(drifted))
	added := false
	for _, o := range drifted {
		labels = append(labels, o.Label)
		if o.StretchArbiterAdded() {
			added = true
		}
	}
	sort.Strings(labels)
	change := "removes the stretch tiebreaker of"
	shape := "a stretch cluster without an arbiter"
	if added {
		change = "adds a stretch tiebreaker to"
		shape = "a stretch cluster arbitrated by a tiebreaker mon"
	}
	return fmt.Sprintf("apply refuses this change to %s: it %s a cluster that is already built, and whether a stretch cluster carries an arbiter is fixed when the cluster is bootstrapped. Bootwright has no path that moves a running cluster between the two shapes — not this apply, and not `bootwright storage-cluster replace-arbiter`, which moves an existing tiebreaker between nodes and cannot create or retire one. Revert the change to match the recorded desired state and keep running the shape you have, or rebuild the cluster deliberately as %s: that is `bootwright destroy` for this cluster followed by a fresh apply, and it destroys all OSD data. `--mode rebuild` is not the remedy here either — it reaches the same data loss (cephadm rm-cluster --zap-osds) without saying so",
		strings.Join(labels, ", "), change, shape)
}

func tiebreakerDriftRefusal(drifted []ObjectClassification) string {
	labels := make([]string, 0, len(drifted))
	seen := map[string]bool{}
	var commands []string
	for _, o := range drifted {
		labels = append(labels, o.Label)
		name := storageClusterObjectName(o)
		if seen[name] {
			continue
		}
		seen[name] = true
		commands = append(commands, "`"+TiebreakerReplacementCommand(name)+"`")
	}
	sort.Strings(labels)
	sort.Strings(commands)
	return fmt.Sprintf("apply refuses this change to %s: its only structural change is spec.ceph.topology.stretch.tiebreaker, and the stretch arbiter is a property of the live monmap that apply cannot reach — `ceph mon enable_stretch_mode` is idempotency-guarded, so a re-apply would leave the old arbiter holding the tiebreaker while the input says otherwise. Move it with %s, which reconciles the live tiebreaker onto the authored one, or revert the change to match the recorded desired state. Rebuilding is not the remedy here: it would wipe and re-bootstrap the whole cluster (cephadm rm-cluster --zap-osds) to move one vote", strings.Join(labels, ", "), strings.Join(commands, ", "))
}

func storageClusterObjectName(o ObjectClassification) string {
	if o.Cluster != "" {
		return o.Cluster
	}
	return strings.TrimPrefix(o.Label, ObjectKindStorageCluster+"/")
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
