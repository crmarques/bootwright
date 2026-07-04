package workflow

// ObjectClassification is the convergence state of one desired-state object — a
// ContainerCluster, StorageCluster, InfraComponent, Machine, addon, and so on —
// aggregated from the converge-safety records of every apply task that backs it.
// It is the unit the apply mode preflight reasons about, computed from the same
// per-task ClassifyConvergeSafety primitive the state-check command consumes, so
// both commands share one comparison.
//
// Objects are classified INDEPENDENTLY: each is compared only against its own
// recorded desired state. A parent object (a StorageCluster) does not inherit
// drift or absence from a new or changed child object (a StoragePool); the child
// is its own entry in the result set.
type ObjectClassification struct {
	ObjectKey string `json:"objectKey"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	// Cluster is the StorageCluster/ContainerCluster the object belongs to (empty
	// for a shared, cluster-less object like a provider or infra-component). The
	// destroy-protection remedy uses it to scope the `destroy --stage infra
	// --clusters ...` command it points a blocked machine-substrate object at.
	Cluster string `json:"cluster,omitempty"`
	// Class is the single most actionable classification for display, chosen with
	// precedence foreign > drift > missing > match. The aggregate decisions the
	// preflight makes use the predicate methods below, not this label.
	Class  ConvergeSafetyClassification         `json:"class"`
	counts map[ConvergeSafetyClassification]int `json:"-"`
	// reconcilable counts the drifted backing tasks whose drift is reconcilable in
	// place (a StorageCluster OSD-device add: full hash changed, structural hash
	// unchanged) rather than a destructive rebuild. HasStructuralDrift subtracts
	// these so continue-mode proceeds and --override does not wipe on a device add.
	reconcilable int
	// Reconcilable is the JSON view of whether all drift on this object is
	// reconcilable in place — surfaced so plan/state-check can distinguish an
	// in-place OSD reconcile from a destructive rebuild.
	Reconcilable bool     `json:"reconcilable,omitempty"`
	TaskIDs      []string `json:"taskIDs,omitempty"`
}

// Recorded reports whether any backing task carries a converge-safety record
// (match, drift, or foreign) — i.e. Bootwright already created or touched this
// object. A wholly unrecorded object is genuinely greenfield.
func (o ObjectClassification) Recorded() bool {
	return o.counts[ConvergeSafetyMatch]+o.counts[ConvergeSafetyDrift]+o.counts[ConvergeSafetyForeign] > 0
}

// HasDrift reports whether any backing task drifted from its recorded desired state.
func (o ObjectClassification) HasDrift() bool { return o.counts[ConvergeSafetyDrift] > 0 }

// HasStructuralDrift reports whether any drift is a destructive-identity change
// (not a reconcilable-in-place OSD-device edit). It drives the continue-mode
// refusal and the --override rebuild: a device-only drift is neither refused nor
// wiped. Reconcilable drift with no structural component is excluded.
func (o ObjectClassification) HasStructuralDrift() bool {
	return o.counts[ConvergeSafetyDrift]-o.reconcilable > 0
}

// HasReconcilableDrift reports whether any drift is reconcilable in place — a
// StorageCluster OSD-device add that `ceph orch apply` converges without a rebuild.
func (o ObjectClassification) HasReconcilableDrift() bool { return o.reconcilable > 0 }

// HasForeign reports whether any backing task was recorded by another manager.
func (o ObjectClassification) HasForeign() bool { return o.counts[ConvergeSafetyForeign] > 0 }

// objectIdentity maps an apply task to the desired-state object it belongs to.
// The multi-task install/storage flows collapse to one object (a ContainerCluster
// spans clusterISO+nodeBoot+installWait; a StorageCluster spans storageInfra +
// storageCluster); every other task is its own object keyed by task identity.
func objectIdentity(task ApplyTask) (kind, key, label string) {
	e := task.Entry
	switch e.Kind {
	case ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait, ApplyTaskKindClusterInstall:
		k := "ContainerCluster/" + e.Cluster
		return "ContainerCluster", k, k
	case ApplyTaskKindStorageInfra, ApplyTaskKindStorageCluster:
		k := "StorageCluster/" + e.Cluster
		return "StorageCluster", k, k
	default:
		lbl := e.Label
		if lbl == "" {
			lbl = e.ID
		}
		return e.Kind, e.Kind + "/" + e.ID, lbl
	}
}

// ClassifyApplyObjects classifies every desired-state object in the selected apply
// graph against the durable convergence-safety records, returning one entry per
// object in stable task order. It is the shared comparison the apply mode preflight
// and (in time) the state-check report both build on. Storage sub-objects
// (StoragePool/Filesystem/ObjectGateway/Export) have no backing apply task — they are
// operations inside the storage task — so each StorageCluster's sub-objects are
// expanded from its desired state and classified independently here, once per cluster.
func ClassifyApplyObjects(tasks []ApplyTask, runsDir string) ([]ObjectClassification, error) {
	order := make([]string, 0, len(tasks))
	objs := map[string]*ObjectClassification{}
	add := func(kind, key, label, cluster string, class ConvergeSafetyClassification, reconcilable bool, taskID string) {
		o := objs[key]
		if o == nil {
			o = &ObjectClassification{ObjectKey: key, Kind: kind, Label: label, Cluster: cluster, counts: map[ConvergeSafetyClassification]int{}}
			objs[key] = o
			order = append(order, key)
		}
		o.counts[class]++
		if reconcilable {
			o.reconcilable++
		}
		if taskID != "" {
			o.TaskIDs = append(o.TaskIDs, taskID)
		}
	}
	expandedStorage := map[string]bool{}
	for _, task := range tasks {
		kind, key, label := objectIdentity(task)
		class, err := classifyApplyTaskState(task, runsDir)
		if err != nil {
			return nil, err
		}
		reconcilable, err := applyTaskReconcilableDrift(task, runsDir)
		if err != nil {
			return nil, err
		}
		// Reconfigure-only kinds (fabric services, node-config, add-ons, virtctl,
		// storage attachment) have no destructive identity: their --override is an
		// idempotent re-apply, so ANY drift on them is reconcilable in place. Continue
		// re-applies a day-2 edit (a changed add-on, node label/taint, DNS/LB record)
		// instead of refusing and forcing --override. A destructive kind is unaffected.
		if class == ConvergeSafetyDrift && overrideReconfigureOnlyKinds[task.Entry.Kind] {
			reconcilable = true
		}
		add(kind, key, label, task.Entry.Cluster, class, reconcilable, task.Entry.ID)
		if task.Entry.Kind == ApplyTaskKindStorageCluster && !expandedStorage[task.Entry.Cluster] {
			expandedStorage[task.Entry.Cluster] = true
			for _, sub := range storageSubObjects(task.State, task.Entry.Cluster) {
				subClass, err := classifyStorageSubObject(task.State, sub, runsDir)
				if err != nil {
					return nil, err
				}
				subReconcilable, err := storageSubObjectReconcilableDrift(task.State, sub, runsDir)
				if err != nil {
					return nil, err
				}
				add(sub.Kind, sub.resourceID(), sub.resourceID(), task.Entry.Cluster, subClass, subReconcilable, "")
			}
		}
	}
	out := make([]ObjectClassification, 0, len(order))
	for _, key := range order {
		o := objs[key]
		o.Class = objectDisplayClass(o.counts)
		o.Reconcilable = o.HasReconcilableDrift() && !o.HasStructuralDrift()
		out = append(out, *o)
	}
	return out, nil
}

// applyTaskReconcilableDrift reports whether a task's drift (if any) is
// reconcilable in place — the full desired hash changed but the structural hash
// is unchanged (a StorageCluster OSD-device add). A missing, foreign, matching,
// or structural-hash-less task is never reconcilable. Kept separate from
// classifyApplyTaskState so that primitive's signature (and its callers) stay put.
func applyTaskReconcilableDrift(task ApplyTask, runsDir string) (bool, error) {
	structuralHash, err := ApplyTaskStructuralHash(task)
	if err != nil {
		return false, err
	}
	if structuralHash == "" {
		return false, nil
	}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		return false, err
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil || !found {
		return false, err
	}
	return IsReconcilableDrift(record, desiredHash, structuralHash), nil
}

// objectDisplayClass picks the most actionable label for an object aggregated from
// its tasks: foreign and drift surface first; a partially applied object (some
// tasks matched, some never applied) reads as missing/incomplete; a fully matched
// object reads as match.
func objectDisplayClass(c map[ConvergeSafetyClassification]int) ConvergeSafetyClassification {
	switch {
	case c[ConvergeSafetyForeign] > 0:
		return ConvergeSafetyForeign
	case c[ConvergeSafetyDrift] > 0:
		return ConvergeSafetyDrift
	case c[ConvergeSafetyMissing] > 0:
		return ConvergeSafetyMissing
	case c[ConvergeSafetyMatch] > 0:
		return ConvergeSafetyMatch
	default:
		return ConvergeSafetyMissing
	}
}
