package status

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

func TestLedgerNextStepsUsesOnlyTheValidatedRecoveryPlan(t *testing.T) {
	original := []string{"bootwright", "apply", "--mode", "create", "--clusters", "original", "--context", "matrix"}
	steps := [][]string{
		{"bootwright", "apply", "--mode", "reconcile", "--stage", "fabric", "--clusters", "dc1; bootwright destroy --yes", "--context", "matrix"},
		append([]string(nil), original...),
	}
	ledger := workflow.RunLedger{
		InvocationArgs: original,
		Recovery: workflow.NewRunRecoveryPlan(remedy.Request{
			Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
			Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "dc1; bootwright destroy --yes"}},
		}, steps...),
		Status: workflow.RunStatusFailed,
	}

	hints := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(hints) != len(steps) {
		t.Fatalf("recovery hints = %v, want %d ordered steps", hints, len(steps))
	}
	for i := range steps {
		if want := shellquote.QuoteWords(steps[i]); hints[i] != want {
			t.Fatalf("recovery hint %d = %q, want exact stored argv %q", i, hints[i], want)
		}
	}
	if hints[0] == shellquote.QuoteWords(original) {
		t.Fatalf("the action-specific first recovery step was replaced by immutable audit argv: %v", hints)
	}
}

func TestLedgerNextStepsFailsClosedWithoutAValidatedRecoveryPlan(t *testing.T) {
	original := []string{"bootwright", "apply", "--mode", "create", "--clusters", "dc1", "--context", "matrix"}
	validStep := []string{"bootwright", "apply", "--mode", "reconcile", "--clusters", "dc1", "--context", "matrix"}
	tests := []struct {
		name     string
		original []string
		plan     workflow.RunRecoveryPlan
	}{
		{name: "missing plan", original: original},
		{name: "missing original audit context", plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, validStep)},
		{name: "unknown action", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.Action("future-action")}, validStep)},
		{name: "action requires targets", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionApplyAllConsumers}, validStep)},
		{name: "wrong target role", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionApplyAllConsumers, Targets: []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "dc1"}}}, validStep)},
		{name: "blank named target", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionResumeControllerDNSMutation, Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot}}}, validStep)},
		{name: "duplicate named targets", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionApplyAllConsumers, Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "dc1"}, {Role: remedy.TargetRoleClusterRoot, Name: "dc1"}}}, validStep)},
		{name: "single target action has two", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRebuildCluster, Targets: []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "dc1"}, {Role: remedy.TargetRoleContainerCluster, Name: "dc2"}}}, validStep)},
		{name: "protected layers empty", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection}, validStep)},
		{name: "protected layer repeats role", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}, {Role: remedy.TargetRoleMachineLayer}}}, validStep)},
		{name: "missing steps", original: original, plan: workflow.UnresolvedRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection})},
		{name: "arbitrary executable", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, []string{"sh", "-c", "bootwright destroy --yes", "--context", "matrix"})},
		{name: "unsupported verb", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, []string{"bootwright", "context", "delete", "--context", "matrix"})},
		{name: "missing step context", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, []string{"bootwright", "apply", "--mode", "reconcile"})},
		{name: "mismatched context", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, []string{"bootwright", "apply", "--mode", "reconcile", "--context", "other"})},
		{name: "preview step", original: original, plan: workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, []string{"bootwright", "apply", "--dry-run", "--context", "matrix"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := workflow.RunLedger{InvocationArgs: tc.original, Recovery: tc.plan, Status: workflow.RunStatusFailed}
			hints := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
			if len(hints) != 1 || hints[0] != unavailableLedgerRetryGuidance {
				t.Fatalf("unsafe ledger hints = %v, want command-free fail-closed guidance", hints)
			}
			if strings.Contains(hints[0], "bootwright") || strings.Contains(hints[0], "--yes") {
				t.Fatalf("fail-closed guidance inferred a runnable command: %q", hints[0])
			}
		})
	}
}

func TestLedgerNextStepsUsesTaskBoundaryRecoveryForStaleAndCancelledRuns(t *testing.T) {
	original := []string{"bootwright", "apply", "--mode", "create", "--stage", "machines", "--context", "matrix"}
	tests := []struct {
		name     string
		status   workflow.RunStatus
		activity workflow.RunActivity
		action   remedy.Action
		step     []string
	}{
		{name: "stale live proof keeps exact create", status: workflow.RunStatusRunning, activity: workflow.RunActivity{State: workflow.RunActivityStale}, action: remedy.ActionRetrySameInvocation, step: original},
		{name: "cancelled live proof keeps exact create", status: workflow.RunStatusCancelled, action: remedy.ActionRetrySameInvocation, step: original},
		{name: "stale downstream mutation reconciles", status: workflow.RunStatusRunning, activity: workflow.RunActivity{State: workflow.RunActivityStale}, action: remedy.ActionReconcileSameSelection, step: []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "machines", "--context", "matrix"}},
		{name: "cancelled downstream mutation reconciles", status: workflow.RunStatusCancelled, action: remedy.ActionReconcileSameSelection, step: []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "machines", "--context", "matrix"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := workflow.RunLedger{
				InvocationArgs: original,
				Recovery:       workflow.NewRunRecoveryPlan(remedy.Request{Action: tc.action}, tc.step),
				Status:         tc.status,
			}
			hints := LedgerNextSteps(ledger, tc.activity, nil)
			if len(hints) != 1 || hints[0] != shellquote.QuoteWords(tc.step) {
				t.Fatalf("task-boundary hints = %v, want exact stored step %q", hints, shellquote.QuoteWords(tc.step))
			}
		})
	}
}

func TestLedgerNextStepsKeepsFailureLogsAfterRecoveryPlan(t *testing.T) {
	args := []string{"bootwright", "destroy", "--context", "matrix"}
	ledger := workflow.RunLedger{
		InvocationArgs: args,
		Recovery:       workflow.NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRetrySameInvocation}, args),
		Status:         workflow.RunStatusFailed,
		Tasks:          []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusFailed, LogPath: "/var/log/bootwright/task.log"}},
	}
	hints := LedgerNextSteps(ledger, workflow.RunActivity{}, []string{"existing guidance"})
	want := []string{shellquote.QuoteWords(args), "inspect /var/log/bootwright/task.log", "existing guidance"}
	if strings.Join(hints, "\n") != strings.Join(want, "\n") {
		t.Fatalf("failed-run hints = %v, want %v", hints, want)
	}
}

func TestLedgerNextStepsDoesNotInventAContextlessWatchCommand(t *testing.T) {
	ledger := workflow.RunLedger{Status: workflow.RunStatusRunning}
	hints := LedgerNextSteps(ledger, workflow.RunActivity{State: workflow.RunActivityActive}, nil)
	if len(hints) != 1 || strings.Contains(hints[0], "bootwright") {
		t.Fatalf("active legacy ledger must emit command-free guidance, got %v", hints)
	}
	original := []string{"bootwright", "apply", "--mode", "reconcile", "--context", "matrix"}
	ledger.InvocationArgs = original
	hints = LedgerNextSteps(ledger, workflow.RunActivity{State: workflow.RunActivityActive}, nil)
	if len(hints) != 1 || hints[0] != "bootwright status --watch --context matrix" {
		t.Fatalf("active ledger must derive watch only from its recorded context, got %v", hints)
	}
}
