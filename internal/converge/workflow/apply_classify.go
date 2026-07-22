package workflow

type ObjectClassification struct {
	ObjectKey    string                               `json:"objectKey"`
	Kind         string                               `json:"kind"`
	Label        string                               `json:"label"`
	Cluster      string                               `json:"cluster,omitempty"`
	Class        ConvergeSafetyClassification         `json:"class"`
	counts       map[ConvergeSafetyClassification]int `json:"-"`
	reconcilable int
	Reconcilable bool     `json:"reconcilable,omitempty"`
	TaskIDs      []string `json:"taskIDs,omitempty"`
}

func (o ObjectClassification) Recorded() bool {
	return o.counts[ConvergeSafetyMatch]+o.counts[ConvergeSafetyDrift]+o.counts[ConvergeSafetyForeign] > 0
}

func (o ObjectClassification) HasDrift() bool { return o.counts[ConvergeSafetyDrift] > 0 }

func (o ObjectClassification) HasStructuralDrift() bool {
	return o.counts[ConvergeSafetyDrift]-o.reconcilable > 0
}

func (o ObjectClassification) HasReconcilableDrift() bool { return o.reconcilable > 0 }

func (o ObjectClassification) HasForeign() bool { return o.counts[ConvergeSafetyForeign] > 0 }

const (
	ObjectKindContainerCluster = "ContainerCluster"
	ObjectKindStorageCluster   = "StorageCluster"
)

func objectIdentity(task ApplyTask) (kind, key, label string) {
	e := task.Entry
	switch e.Kind {
	case ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait, ApplyTaskKindClusterInstall:
		k := ObjectKindContainerCluster + "/" + e.Cluster
		return ObjectKindContainerCluster, k, k
	case ApplyTaskKindStorageNodeAccess, ApplyTaskKindStorageInfra, ApplyTaskKindStorageCluster:
		k := ObjectKindStorageCluster + "/" + e.Cluster
		return ObjectKindStorageCluster, k, k
	default:
		lbl := e.Label
		if lbl == "" {
			lbl = e.ID
		}
		return e.Kind, e.Kind + "/" + e.ID, lbl
	}
}

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
		reconcilable, err := taskDriftReconcilable(task, runsDir, class)
		if err != nil {
			return nil, err
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

func taskDriftReconcilable(task ApplyTask, runsDir string, class ConvergeSafetyClassification) (bool, error) {
	if class != ConvergeSafetyDrift {
		return false, nil
	}
	if overrideReconfigureOnlyKinds[task.Entry.Kind] {
		return true, nil
	}
	return applyTaskReconcilableDrift(task, runsDir)
}

func applyTaskReconcilableDrift(task ApplyTask, runsDir string) (bool, error) {
	structuralHash, err := ApplyTaskStructuralHash(task)
	if err != nil {
		return false, err
	}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		return false, err
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil || !found {
		return false, err
	}
	if structuralHash == "" {
		return false, nil
	}
	return IsReconcilableDrift(record, desiredHash, structuralHash), nil
}

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
