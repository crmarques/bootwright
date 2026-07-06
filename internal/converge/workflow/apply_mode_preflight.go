package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// EvaluateApplyModePreflight is the Go-side gate that enforces the apply mode
// contract against the recorded convergence state of the selected objects, before
// any mutation. Per-role Ansible gates enforce the same contract against live
// state; this gate gives a fast, meaningful refusal up front. It returns a non-nil
// error when the chosen mode forbids proceeding.
//
//	create:   greenfield assert (--expect-new) — refuse if any selected object
//	          already exists.
//	continue: safe reconcile (the default) — refuse on drift or foreign ownership;
//	          otherwise proceed (missing objects are created, matching objects no-op).
//	override: break-glass — refuse only on foreign ownership; rebuild drifted and
//	          create missing objects; objects already matching are left untouched.
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
			return fmt.Errorf("apply --expect-new requires a greenfield environment and these objects already exist: %s; drop --expect-new to reconcile them, or run `bootwright apply --override` to rebuild drifted objects", summarizeApplyObjects(existing))
		}
	case ApplyModeContinue:
		var drifted, foreign []ObjectClassification
		for _, o := range objects {
			// Reconcilable drift (a StorageCluster OSD-device add, a storage set-* edit,
			// a ContainerCluster day-2 edit, any reconfigure-only re-apply) is NOT
			// refused: it converges in place on this same run, so continue proceeds.
			// Only foreign ownership or a destructive-identity (structural) change —
			// including any change bootwright cannot map to a safe in-place reconcile —
			// blocks continue and fails closed here.
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
			return fmt.Errorf("apply --override never rebuilds objects recorded by another manager: %s; resolve ownership before retrying", summarizeApplyObjects(foreign))
		}
	}
	return nil
}

// continueDriftRefusal builds the fail-closed continue-mode refusal in the "unmapped
// change = unsupported, stop with guidance" contract: for the owned objects whose
// change is not a safe in-place reconcile, it names the object AND the worst-case
// consequence of rebuilding it, then the exact remedy (revert, or the override path a
// destructive rebuild needs); foreign objects get their own ownership remedy. A change
// bootwright cannot map to a reconcilable transition lands here by default (structural
// is the fail-safe classification), so an untaught edit is refused with this guidance
// rather than silently rebuilt.
func continueDriftRefusal(drifted, foreign []ObjectClassification) error {
	var parts []string
	if len(drifted) > 0 {
		items := make([]string, 0, len(drifted))
		for _, o := range drifted {
			items = append(items, fmt.Sprintf("%s (would %s)", o.Label, structuralRebuildConsequence(o)))
		}
		sort.Strings(items)
		parts = append(parts, fmt.Sprintf("apply refuses this change to %s: it is not a safe in-place reconcile. To proceed, revert the change to match the recorded desired state, or — if you intend the rebuild — re-run with `bootwright apply --override` (a destructive rebuild additionally needs `--allow-destroy`, or on a protected environment a `bootwright destroy` for that scope first)", strings.Join(items, ", ")))
	}
	if len(foreign) > 0 {
		parts = append(parts, fmt.Sprintf("apply never modifies objects recorded by another manager: %s; resolve ownership before retrying (foreign objects are never rebuilt)", summarizeApplyObjects(foreign)))
	}
	return fmt.Errorf("%s", strings.Join(parts, ". "))
}

// structuralRebuildConsequence is the kind-specific worst-case of an --override rebuild
// of a structurally-drifted object, so the refusal states what would be destroyed
// rather than a generic "rebuild".
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

// overrideReconfigureOnlyKinds are the object kinds whose --override rebuild is an
// idempotent re-apply — a fabric-service reconfigure, a node-config or add-on
// re-push, a storage attachment refresh — that destroys no data, OS, or VM. Drift
// on these never crosses the destroy-protection boundary. Every other kind (a
// container or storage cluster, a managed-OS or substrate machine) is treated as a
// destructive rebuild. The set is an ALLOWLIST so a kind not classified here is
// gated by default: a new destructive kind fails safe rather than slipping past.
var overrideReconfigureOnlyKinds = map[string]bool{
	ApplyTaskKindProvider:               true,
	ApplyTaskKindInfraComponentServices: true,
	ApplyTaskKindNodeConfigApply:        true,
	ApplyTaskKindHostVirtctl:            true,
	ApplyTaskKindClusterAddon:           true,
	ApplyTaskKindStorageAttachmentApply: true,
}

// machineSubstrateKinds are the destructive apply-task kinds whose teardown lives
// in the infra (fabric/machines) stage rather than the clusters stage — a
// managed-OS install or a per-host machine-infra step. A clusters-stage destroy
// deliberately leaves machine substrate in place ("machine substrate cleanup is
// handled by destroy --stage infra"), so its convergence records survive it and a
// re-apply keeps seeing the same destructive drift. The destroy-protection remedy
// keys on this to route a blocked object of these kinds at `destroy --stage infra`
// instead of the clusters destroy that can never clear it.
var machineSubstrateKinds = map[string]bool{
	ApplyTaskKindManagedMachineOS:     true,
	ApplyTaskKindMachineInfraPrepare:  true,
	ApplyTaskKindMachineInfraFinalize: true,
}

// isOverrideDestructive reports whether an object's --override rebuild would be
// destructive: structurally drifted (recorded and changed in its destructive
// identity, so override rebuilds rather than creates) and of a kind that is not
// reconfigure-only. A reconfigure-only service, a reconcilable-in-place
// StorageCluster OSD-device add, and a missing (greenfield) object are all
// non-destructive and excluded.
func isOverrideDestructive(o ObjectClassification) bool {
	return o.HasStructuralDrift() && !overrideReconfigureOnlyKinds[o.Kind]
}

// OverrideDestructiveDriftedObjects returns the labels of the selected objects
// whose --override rebuild would be destructive. The destroy-protection gate keys
// on this so a scoped apply --override whose only drift is a reconfigure-only
// service, or a reconcilable-in-place StorageCluster OSD-device add, does not need
// to cross the destroy boundary, while one that would reinstall a VM or wipe a
// cluster still does.
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

// objectProtectedKind maps a classified object to the spec.safety.protectedKinds
// category it belongs to — a container or storage cluster, or any machine-substrate
// task (managed-OS install / machine-infra step) to Machine — or "" for an object no
// protectedKind covers. A storage sub-object (pool/filesystem/gateway/export) maps
// to StorageCluster: its data lives inside the protected cluster, and
// protectedKinds cannot name a sub-object directly (validation restricts it to
// {ContainerCluster, StorageCluster, Machine}), so protecting the StorageCluster is
// the only granular way to gate a data-destroying pool/filesystem rebuild.
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

// OverrideDestructiveKindProtected returns the labels of the destructive-drifted
// objects whose kind is in the protected set — the granular destroy-protection gate
// for an environment whose fleet-wide destroyProtection is allow. A reconfigure-only
// or reconcilable object is never destructive, so it never trips this.
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

// OverrideDestructiveMachineSubstrate returns the labels of the blocked
// (destructive-drifted) objects that are machine substrate, and the distinct
// clusters they belong to. The destroy-protection remedy uses these to direct the
// operator at the infra-stage destroy that actually clears them — a clusters-stage
// destroy leaves machine substrate in place, so pointing there would loop.
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
