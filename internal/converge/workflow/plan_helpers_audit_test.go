package workflow

import "github.com/crmarques/bootwright/api/v1alpha1"

func mustPlanApplyTasks(target ApplyTarget, state v1alpha1.State) []ApplyTask {
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		panic(err)
	}
	return tasks
}
