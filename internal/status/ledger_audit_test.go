package status

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

func TestLedgerNextStepsReplaysOnlyTheRecordedMutatingInvocation(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "apply range cluster mode reclaim authorization ssh yes and hostile values",
			args: []string{
				"bootwright", "apply",
				"--mode", "rebuild",
				"--authorize", "foreign-daemons,data-loss",
				"--yes",
				"--stage", "deps",
				"--through", "base",
				"--clusters", "dc1; bootwright destroy --yes",
				"--reclaim-devices", "all",
				"--ask-become-pass=false",
				"--trust-on-first-use=false",
				"--verbose",
				"--context", "matrix'$(touch /tmp/not-run)",
				"--ssh-id-file", "/tmp/operator key;echo owned",
				"--ssh-user", "operator name",
				"--ssh-ask-sudo-password",
				"--ssh-user-for-provisioned",
			},
		},
		{
			name: "destroy machines recovery purge authorization ssh without yes",
			args: []string{
				"bootwright", "destroy",
				"--authorize", "protected,unreachable-nodes",
				"--stage", "infra",
				"--machines", "worker-2,worker-1",
				"--recover-ceph-ownership", "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
				"--purge-history",
				"--ask-become-pass=true",
				"--verbose",
				"--context", "matrix",
				"--ssh-id-file", "/tmp/id",
				"--ssh-user", "operator",
				"--ssh-ask-sudo-password",
				"--ssh-user-for-provisioned",
			},
		},
		{
			name: "apply cluster without yes",
			args: []string{"bootwright", "apply", "--mode", "reconcile", "--clusters", "dc1", "--ask-become-pass=false", "--trust-on-first-use=true", "--context", "matrix"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := workflow.RunLedger{
				Target:         "all destroy",
				Scope:          "wider-cluster",
				Machines:       []string{"wider-machine"},
				InvocationArgs: append([]string(nil), tc.args...),
				Status:         workflow.RunStatusFailed,
			}
			steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
			if len(steps) == 0 {
				t.Fatal("a failed run must emit a retry hint")
			}
			if want := shellquote.QuoteWords(tc.args); steps[0] != want {
				t.Fatalf("retry hint = %q, want exact recorded argv %q", steps[0], want)
			}
			if !containsArg(tc.args, "--yes") && strings.Contains(steps[0], "--yes") {
				t.Fatalf("retry hint invented --yes: %q", steps[0])
			}
		})
	}
}

func TestLedgerNextStepsFailsClosedWithoutAValidatedExactInvocation(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "legacy ledger"},
		{name: "arbitrary executable", args: []string{"sh", "-c", "bootwright destroy --yes", "--context", "matrix"}},
		{name: "unsupported verb", args: []string{"bootwright", "context", "delete", "--context", "matrix"}},
		{name: "missing context", args: []string{"bootwright", "destroy", "--yes"}},
		{name: "empty context", args: []string{"bootwright", "apply", "--context", ""}},
		{name: "preview cannot be a mutating run", args: []string{"bootwright", "apply", "--dry-run", "--context", "matrix"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := workflow.RunLedger{Target: "all destroy", Scope: "everything", InvocationArgs: tc.args, Status: workflow.RunStatusFailed}
			steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
			if len(steps) == 0 || steps[0] != unavailableLedgerRetryGuidance {
				t.Fatalf("unsafe ledger hint = %v, want command-free fail-closed guidance", steps)
			}
			if strings.Contains(steps[0], "bootwright") || strings.Contains(steps[0], "--yes") {
				t.Fatalf("fail-closed guidance must not infer a runnable command: %q", steps[0])
			}
		})
	}
}

func TestLedgerNextStepsKeepsFailureLogsAfterExactRetry(t *testing.T) {
	args := []string{"bootwright", "destroy", "--context", "matrix"}
	ledger := workflow.RunLedger{
		InvocationArgs: args,
		Status:         workflow.RunStatusFailed,
		Tasks:          []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusFailed, LogPath: "/var/log/bootwright/task.log"}},
	}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, []string{"existing guidance"})
	want := []string{shellquote.QuoteWords(args), "inspect /var/log/bootwright/task.log", "existing guidance"}
	if strings.Join(steps, "\n") != strings.Join(want, "\n") {
		t.Fatalf("failed-run hints = %v, want %v", steps, want)
	}
}

func TestLedgerNextStepsDoesNotInventAContextlessWatchCommand(t *testing.T) {
	ledger := workflow.RunLedger{Status: workflow.RunStatusRunning}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{State: workflow.RunActivityActive}, nil)
	if len(steps) != 1 || strings.Contains(steps[0], "bootwright") {
		t.Fatalf("active legacy ledger must emit command-free guidance, got %v", steps)
	}
	existing := []string{"bootwright status --watch --context matrix"}
	ledger.InvocationArgs = []string{"bootwright", "apply", "--mode", "reconcile", "--context", "matrix"}
	steps = LedgerNextSteps(ledger, workflow.RunActivity{State: workflow.RunActivityActive}, nil)
	if len(steps) != 1 || steps[0] != existing[0] {
		t.Fatalf("active ledger must derive watch only from its recorded context, got %v", steps)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
