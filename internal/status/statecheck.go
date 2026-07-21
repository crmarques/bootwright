package status

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func StateCheck(state v1alpha1.State, clusterScope, stageTarget string, applyTarget workflow.ApplyTarget, runsDir, ownershipDir, contextName string) (workflow.StateCheckReport, error) {
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
	report, err := workflow.StateCheck(tasks, applyTarget, state, runsDir)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
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
