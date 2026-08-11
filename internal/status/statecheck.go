package status

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func StateCheck(state v1alpha1.State, clusterScope, stageTarget string, applyTarget workflow.ApplyTarget, clustersDir, runsDir, ownershipDir, contextName string) (workflow.StateCheckReport, error) {
	fullState := state
	sel, err := clusteraccess.Resolve(state, stageTarget, clusterScope)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	state = sel.RenderState
	if sel.Active {
		applyTarget.StorageClusterNames = sel.StorageWorkNames()
	}
	tasks, err := workflow.PlanApplyTasksCheckedWithHashState(applyTarget, state, fullState)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	report, err := workflow.StateCheck(tasks, applyTarget, state, runsDir, contextName)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	report.AddonCSVs = addonCSVReports(tasks, clustersDir, report.Roots)
	if records, skipped, lerr := ownership.LoadContextWithWarnings(ownershipDir, contextName); lerr == nil {
		report.Undeclared = workflow.OwnershipOrphans(fullState, records)
		for _, warning := range skipped {
			report.LoadWarnings = append(report.LoadWarnings, warning.Error())
		}
	} else {
		report.LoadWarnings = append(report.LoadWarnings, lerr.Error())
	}
	return report, nil
}

func addonCSVReports(tasks []workflow.ApplyTask, clustersDir string, roots []workflow.StateCheckRoot) []workflow.AddonCSVReport {
	absentClusters := map[string]bool{}
	for _, root := range roots {
		if root.Kind == workflow.ApplyClusterKindContainer && root.Absent {
			absentClusters[root.Name] = true
		}
	}
	var reports []workflow.AddonCSVReport
	for _, task := range tasks {
		if task.Entry.Kind != workflow.ApplyTaskKindClusterAddon || task.Extension == nil || absentClusters[task.Entry.Cluster] {
			continue
		}
		record, found, loadErr := extensionrecords.LoadRecord(clustersDir, task.Entry.Cluster, task.Entry.Addon)
		used := make([]bool, len(record.CSVObservations))
		for _, check := range task.Extension.Extension.Spec.Readiness.Checks {
			if check.CSVSucceeded == nil {
				continue
			}
			report := workflow.AddonCSVReport{
				Cluster: task.Entry.Cluster, Addon: task.Entry.Addon,
				Namespace: check.CSVSucceeded.Namespace, Subscription: check.CSVSucceeded.Subscription,
			}
			switch {
			case loadErr != nil:
				report.Note = "add-on record unavailable: " + loadErr.Error()
			case !found:
				report.Note = "no ready CSV observation recorded"
			default:
				for index := range record.CSVObservations {
					observation := record.CSVObservations[index]
					if used[index] || observation.Namespace != report.Namespace || observation.Subscription != report.Subscription {
						continue
					}
					used[index] = true
					report.Recorded = &observation
					break
				}
				if report.Recorded == nil {
					report.Note = "no ready CSV observation recorded"
				}
			}
			reports = append(reports, report)
		}
	}
	sort.SliceStable(reports, func(i, j int) bool {
		left := reports[i].Cluster + "\x00" + reports[i].Addon + "\x00" + reports[i].Namespace + "\x00" + reports[i].Subscription
		right := reports[j].Cluster + "\x00" + reports[j].Addon + "\x00" + reports[j].Namespace + "\x00" + reports[j].Subscription
		return left < right
	})
	return reports
}
