package workflow

import "github.com/crmarques/bootwright/api/v1alpha1"

// mustPlanApplyTasks plans apply tasks for a state the test already knows is
// valid, failing the test via panic if planning errors. It replaces the former
// error-swallowing PlanApplyTasks production wrapper, which was test-only.
func mustPlanApplyTasks(target ApplyTarget, state v1alpha1.State) []ApplyTask {
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		panic(err)
	}
	return tasks
}
