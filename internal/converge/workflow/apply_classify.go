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
	// Class is the single most actionable classification for display, chosen with
	// precedence foreign > drift > missing > match. The aggregate decisions the
	// preflight makes use the predicate methods below, not this label.
	Class   ConvergeSafetyClassification         `json:"class"`
	counts  map[ConvergeSafetyClassification]int `json:"-"`
	TaskIDs []string                             `json:"taskIDs,omitempty"`
}

// Recorded reports whether any backing task carries a converge-safety record
// (match, drift, or foreign) — i.e. Bootwright already created or touched this
// object. A wholly unrecorded object is genuinely greenfield.
func (o ObjectClassification) Recorded() bool {
	return o.counts[ConvergeSafetyMatch]+o.counts[ConvergeSafetyDrift]+o.counts[ConvergeSafetyForeign] > 0
}

// HasDrift reports whether any backing task drifted from its recorded desired state.
func (o ObjectClassification) HasDrift() bool { return o.counts[ConvergeSafetyDrift] > 0 }

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
// and (in time) the state-check report both build on.
func ClassifyApplyObjects(tasks []ApplyTask, runsDir string) ([]ObjectClassification, error) {
	order := make([]string, 0, len(tasks))
	objs := map[string]*ObjectClassification{}
	for _, task := range tasks {
		kind, key, label := objectIdentity(task)
		class, err := classifyApplyTaskState(task, runsDir)
		if err != nil {
			return nil, err
		}
		o := objs[key]
		if o == nil {
			o = &ObjectClassification{ObjectKey: key, Kind: kind, Label: label, counts: map[ConvergeSafetyClassification]int{}}
			objs[key] = o
			order = append(order, key)
		}
		o.counts[class]++
		o.TaskIDs = append(o.TaskIDs, task.Entry.ID)
	}
	out := make([]ObjectClassification, 0, len(order))
	for _, key := range order {
		o := objs[key]
		o.Class = objectDisplayClass(o.counts)
		out = append(out, *o)
	}
	return out, nil
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
