package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func planPlaybookWithTimeout(t *testing.T, timeout string) ApplyTask {
	t.Helper()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	playbook := provisioningPlaybook("tuned", v1alpha1.CustomPlaybookAnchorBase, anchorKeyFollows,
		v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}})
	playbook.Spec.Timeout = timeout
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{playbook}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	return assertTaskPresent(t, tasks, "playbook.tuned")
}

func TestPlanPlaybookCarriesDefaultTimeout(t *testing.T) {
	if got := planPlaybookWithTimeout(t, "").Timeout; got != 10*time.Minute {
		t.Fatalf("task timeout = %s, want the 10m default", got)
	}
}

func TestPlanPlaybookCarriesAuthoredTimeout(t *testing.T) {
	if got := planPlaybookWithTimeout(t, "90m").Timeout; got != 90*time.Minute {
		t.Fatalf("task timeout = %s, want 90m", got)
	}
}
