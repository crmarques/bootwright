package workflow

import (
	"fmt"
	"sort"
	"strings"
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
		var differ []ObjectClassification
		for _, o := range objects {
			if o.HasForeign() || o.HasDrift() {
				differ = append(differ, o)
			}
		}
		if len(differ) > 0 {
			msg := fmt.Sprintf("apply refuses to mutate objects that differ from their recorded desired state: %s; align the desired state, or run `bootwright apply --override` to rebuild drifted objects (foreign objects are never rebuilt)", summarizeApplyObjects(differ))
			if driftsStorageCluster(differ) {
				msg += ". Note: --override on a drifted StorageCluster runs `cephadm rm-cluster --zap-osds` and DESTROYS all OSD data before re-bootstrapping — it is a wipe-and-rebuild, not an in-place edit"
			}
			return fmt.Errorf("%s", msg)
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

// driftsStorageCluster reports whether any differing object is a StorageCluster,
// so the continue-mode refusal can warn that its --override rebuild is a
// data-destroying wipe rather than the in-place edit "rebuild drifted objects"
// implies for a fabric or add-on object.
func driftsStorageCluster(objs []ObjectClassification) bool {
	for _, o := range objs {
		if o.Kind == ApplyTaskKindStorageCluster && o.HasDrift() {
			return true
		}
	}
	return false
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

// OverrideDestructiveDriftedObjects returns the labels of the selected objects
// whose --override rebuild would be destructive: drifted (recorded and changed,
// so override rebuilds rather than creates) and of a kind that is not
// reconfigure-only. The destroy-protection gate keys on this so a scoped apply
// --override whose only drift is a reconfigure-only service does not need to cross
// the destroy boundary, while one that would reinstall a VM or wipe a cluster
// still does. Missing (greenfield) objects are never destructive and are excluded.
func OverrideDestructiveDriftedObjects(objects []ObjectClassification) []string {
	var labels []string
	for _, o := range objects {
		if !o.HasDrift() || overrideReconfigureOnlyKinds[o.Kind] {
			continue
		}
		labels = append(labels, o.Label)
	}
	sort.Strings(labels)
	return labels
}
