package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// StateCheckResource is one resource in the selected apply graph whose recorded
// convergence state differs from the current desired state.
type StateCheckResource struct {
	ResourceID     string                       `json:"resourceID"`
	Kind           string                       `json:"kind"`
	Label          string                       `json:"label"`
	Classification ConvergeSafetyClassification `json:"classification"`
}

// StateCheckRoot summarizes one selected root (a ContainerCluster, a
// StorageCluster, or the shared infrastructure) against recorded reality.
type StateCheckRoot struct {
	Kind      string               `json:"kind"`
	Name      string               `json:"name"`
	Absent    bool                 `json:"absent"`
	Total     int                  `json:"total"`
	Matched   int                  `json:"matched"`
	Resources []StateCheckResource `json:"resources,omitempty"`
}

// StateCheckReport is the result of a non-mutating desired-vs-recorded state
// check over the selected apply graph.
type StateCheckReport struct {
	Roots  []StateCheckRoot `json:"roots"`
	InSync bool             `json:"inSync"`
	// Undeclared lists Bootwright-owned resources still recorded in the ownership
	// store but no longer present in desired state (orphans left by removing an object
	// without destroying it). Reported, never mutated; a full `destroy` reclaims them.
	Undeclared []UndeclaredResource `json:"undeclared,omitempty"`
	// LoadWarnings carries per-record skip reasons for records that could not be
	// read, decoded, or validated: both ownership records (whose orphan would
	// otherwise silently vanish from the report that exists to find orphans) and
	// convergence-safety records (a single corrupt file under runs/safety/ must not
	// brick this read-only report — the resource is skipped and the file named here
	// instead). Reported, never fatal; the apply-time gate stays fail-loud.
	LoadWarnings []string `json:"loadWarnings,omitempty"`
}

// StateCheck classifies every task in the selected apply graph, plus each
// StorageCluster's declared sub-objects (pools, filesystems, object gateways,
// exports), against the durable convergence-safety evidence recorded by the last
// apply, without running playbooks, probing, or writing records. Each resource
// resolves to match (applied with the current desired state), drift (desired
// state changed since it was applied), foreign (a non-Bootwright owner recorded
// it), or missing (never applied). A root whose resources are all missing is
// reported as one absence instead of a flood of per-resource missing lines; a
// present root reports only the resources that are not in sync.
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
		accumulate(rootFor(kind, name), class, stateCheckResource(task, class))
	}

	// Expand each StorageCluster's sub-objects against their own durable records
	// (MarkStorageSubObjectsConvergeSafety writes one per sub-object, keyed
	// "<Kind>/<cluster>.<name>"), so a present storage cluster reports which
	// specific pool or export drifted or is missing instead of collapsing to one
	// StorageCluster line. Sub-objects share their owning cluster's
	// "storage/<cluster>" root, so a never-applied cluster — cluster task and
	// every sub-object missing — still collapses to a single absence.
	//
	// Classified only for storage clusters the selected graph actually plans a
	// StorageCluster task for, mirroring the apply preflight exactly
	// (ClassifyApplyObjects expands sub-objects per StorageCluster task). A stage
	// that plans no storage (e.g. --stage infra) and a managed StorageCluster
	// pulled in only as a data-foundation render reference (no provisioning task,
	// ADR-0004) both carry no task here, so neither reports spurious pool/export
	// drift the identically-scoped apply would never touch (a state check exiting 3
	// where apply is a clean no-op).
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
			accumulate(acc, class, storageSubObjectResource(sub, class))
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

// classifyApplyTaskStateLenient is the read-only state-check variant of
// classifyApplyTaskState: a corrupt or unreadable converge-safety record for the
// task is returned as a non-empty warning (naming the file) and the task is
// skipped by the caller, instead of aborting the whole state-check report. The
// strict classifyApplyTaskState stays fail-loud for apply's preflight gate.
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

// classifyStorageSubObjectLenient mirrors classifyApplyTaskStateLenient for a
// StorageCluster sub-object, degrading a corrupt record to a warning rather than
// bricking the read-only report.
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

func stateCheckResource(task ApplyTask, class ConvergeSafetyClassification) StateCheckResource {
	return StateCheckResource{
		ResourceID:     applyTaskSafetyResourceID(task),
		Kind:           task.Entry.Kind,
		Label:          task.Entry.Label,
		Classification: class,
	}
}

func storageSubObjectResource(sub storageSubObject, class ConvergeSafetyClassification) StateCheckResource {
	return StateCheckResource{
		ResourceID:     sub.resourceID(),
		Kind:           sub.Kind,
		Label:          sub.resourceID(),
		Classification: class,
	}
}
