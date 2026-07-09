package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type StateCheckResource struct {
	ResourceID     string                       `json:"resourceID"`
	Kind           string                       `json:"kind"`
	Label          string                       `json:"label"`
	Classification ConvergeSafetyClassification `json:"classification"`
	Reconcilable   bool                         `json:"reconcilable,omitempty"`
}

type StateCheckRoot struct {
	Kind      string               `json:"kind"`
	Name      string               `json:"name"`
	Absent    bool                 `json:"absent"`
	Total     int                  `json:"total"`
	Matched   int                  `json:"matched"`
	Resources []StateCheckResource `json:"resources,omitempty"`
}

type StateCheckReport struct {
	Roots        []StateCheckRoot     `json:"roots"`
	InSync       bool                 `json:"inSync"`
	Undeclared   []UndeclaredResource `json:"undeclared,omitempty"`
	LoadWarnings []string             `json:"loadWarnings,omitempty"`
}

func StateCheck(tasks []ApplyTask, target ApplyTarget, state v1alpha1.State, runsDir string) (StateCheckReport, error) {
	type rootAcc struct {
		kind, name string
		total      int
		matched    int
		missing    int
		resources  []StateCheckResource
	}
	order := make([]string, 0)
	roots := map[string]*rootAcc{}
	rootFor := func(kind, name string) *rootAcc {
		key := kind + "/" + name
		acc := roots[key]
		if acc == nil {
			acc = &rootAcc{kind: kind, name: name}
			roots[key] = acc
			order = append(order, key)
		}
		return acc
	}
	accumulate := func(acc *rootAcc, class ConvergeSafetyClassification, resource StateCheckResource) {
		acc.total++
		switch class {
		case ConvergeSafetyMatch:
			acc.matched++
		case ConvergeSafetyMissing:
			acc.missing++
			acc.resources = append(acc.resources, resource)
		default:
			acc.resources = append(acc.resources, resource)
		}
	}

	var loadWarnings []string
	for _, task := range tasks {
		kind := task.Entry.ClusterKind
		name := task.Entry.Cluster
		if name == "" {
			kind, name = "infrastructure", "infrastructure"
		}
		class, warning, err := classifyApplyTaskStateLenient(task, runsDir)
		if err != nil {
			return StateCheckReport{}, err
		}
		if warning != "" {
			loadWarnings = append(loadWarnings, warning)
			continue
		}
		reconcilable, err := taskDriftReconcilable(task, runsDir, class)
		if err != nil {
			return StateCheckReport{}, err
		}
		accumulate(rootFor(kind, name), class, stateCheckResource(task, class, reconcilable))
	}

	storagePlanned := map[string]bool{}
	for _, task := range tasks {
		if task.Entry.Kind == ApplyTaskKindStorageCluster {
			storagePlanned[task.Entry.Cluster] = true
		}
	}
	for _, cluster := range state.StorageClusters {
		if !storagePlanned[cluster.Metadata.Name] || !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		acc := rootFor(ApplyClusterKindStorage, cluster.Metadata.Name)
		for _, sub := range storageSubObjects(state, cluster.Metadata.Name) {
			class, warning, err := classifyStorageSubObjectLenient(state, sub, runsDir)
			if err != nil {
				return StateCheckReport{}, err
			}
			if warning != "" {
				loadWarnings = append(loadWarnings, warning)
				continue
			}
			reconcilable, err := storageSubObjectReconcilableDrift(state, sub, runsDir)
			if err != nil {
				return StateCheckReport{}, err
			}
			accumulate(acc, class, storageSubObjectResource(sub, class, reconcilable))
		}
	}

	report := StateCheckReport{InSync: true, LoadWarnings: loadWarnings}
	for _, key := range order {
		acc := roots[key]
		root := StateCheckRoot{Kind: acc.kind, Name: acc.name, Total: acc.total, Matched: acc.matched}
		switch {
		case acc.total > 0 && acc.missing == acc.total:
			root.Absent = true
			report.InSync = false
		case len(acc.resources) > 0:
			root.Resources = acc.resources
			report.InSync = false
		}
		report.Roots = append(report.Roots, root)
	}
	sort.SliceStable(report.Roots, func(i, j int) bool {
		if report.Roots[i].Kind != report.Roots[j].Kind {
			return report.Roots[i].Kind < report.Roots[j].Kind
		}
		return report.Roots[i].Name < report.Roots[j].Name
	})
	return report, nil
}

func classifyApplyTaskStateLenient(task ApplyTask, runsDir string) (ConvergeSafetyClassification, string, error) {
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		return "", "", err
	}
	record, found, warning, err := loadConvergeSafetyRecordLenient(runsDir, applyTaskSafetyResourceID(task))
	if err != nil {
		return "", "", err
	}
	if warning != "" {
		return "", warning, nil
	}
	if !found {
		return ConvergeSafetyMissing, "", nil
	}
	return ClassifyConvergeSafety(record, desiredHash, ConvergeSafetyOwner), "", nil
}

func classifyStorageSubObjectLenient(state v1alpha1.State, sub storageSubObject, runsDir string) (ConvergeSafetyClassification, string, error) {
	desiredHash, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		return "", "", err
	}
	record, found, warning, err := loadConvergeSafetyRecordLenient(runsDir, sub.resourceID())
	if err != nil {
		return "", "", err
	}
	if warning != "" {
		return "", warning, nil
	}
	if !found {
		return ConvergeSafetyMissing, "", nil
	}
	return ClassifyConvergeSafety(record, desiredHash, ConvergeSafetyOwner), "", nil
}

func classifyApplyTaskState(task ApplyTask, runsDir string) (ConvergeSafetyClassification, error) {
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		return "", err
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil {
		return "", err
	}
	if !found {
		return ConvergeSafetyMissing, nil
	}
	return ClassifyConvergeSafety(record, desiredHash, ConvergeSafetyOwner), nil
}

func stateCheckResource(task ApplyTask, class ConvergeSafetyClassification, reconcilable bool) StateCheckResource {
	return StateCheckResource{
		ResourceID:     applyTaskSafetyResourceID(task),
		Kind:           task.Entry.Kind,
		Label:          task.Entry.Label,
		Classification: class,
		Reconcilable:   reconcilable,
	}
}

func storageSubObjectResource(sub storageSubObject, class ConvergeSafetyClassification, reconcilable bool) StateCheckResource {
	return StateCheckResource{
		ResourceID:     sub.resourceID(),
		Kind:           sub.Kind,
		Label:          sub.resourceID(),
		Classification: class,
		Reconcilable:   reconcilable,
	}
}
