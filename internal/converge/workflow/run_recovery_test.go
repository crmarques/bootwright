package workflow

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func TestRunRecoveryPlanValidationCoversEveryRegisteredAction(t *testing.T) {
	apply := []string{"bootwright", "apply", "--context", "matrix"}
	destroy := []string{"bootwright", "destroy", "--context", "matrix"}
	container := []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "ocp"}}
	root := []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}
	plans := map[remedy.Action]RunRecoveryPlan{
		remedy.ActionRetrySameInvocation:                             NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRetrySameInvocation}, apply),
		remedy.ActionApplyAllConsumers:                               NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionApplyAllConsumers, Targets: root}, apply),
		remedy.ActionResumeControllerDNSMutation:                     NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionResumeControllerDNSMutation, Targets: root}, apply),
		remedy.ActionReconcileSharedServiceThenRetrySameSelection:    NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSharedServiceThenRetrySameSelection, Targets: root}, apply, apply),
		remedy.ActionReconcileSameSelection:                          NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, apply),
		remedy.ActionReconcileContainerClusterThenRetrySameSelection: NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileContainerClusterThenRetrySameSelection, Targets: container}, apply, apply),
		remedy.ActionRebuildSameSelection:                            NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRebuildSameSelection}, apply),
		remedy.ActionRegenerateClusterISO:                            NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRegenerateClusterISO, Targets: container}, apply, apply),
		remedy.ActionDestroyAndReapplyCluster:                        NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyAndReapplyCluster, Targets: container}, destroy, apply),
		remedy.ActionRebuildCluster:                                  NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRebuildCluster, Targets: container}, apply),
		remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:  NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}, {Role: remedy.TargetRoleClusterLayer}}}, destroy, destroy, apply),
	}
	registered := remedy.RegisteredActions()
	for _, action := range registered {
		plan, ok := plans[action]
		if !ok {
			t.Errorf("registered recovery action %q has no persisted-plan validation case", action)
			continue
		}
		if !plan.Valid() {
			t.Errorf("registered recovery action %q rejected its canonical request and step sequence: %#v", action, plan)
		}
	}
	if len(plans) != len(registered) {
		t.Fatalf("persisted-plan validation has %d action cases for %d registered actions", len(plans), len(registered))
	}
}

func TestRunRecoveryPlanRejectsTruncatedAndWrongVerbSequences(t *testing.T) {
	apply := []string{"bootwright", "apply", "--context", "matrix"}
	destroy := []string{"bootwright", "destroy", "--context", "matrix"}
	root := []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}
	container := []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "ocp"}}
	tests := []RunRecoveryPlan{
		NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSharedServiceThenRetrySameSelection, Targets: root}, apply),
		NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, destroy),
		NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyAndReapplyCluster, Targets: container}, apply, apply),
		NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}}}, destroy),
		NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRetrySameInvocation}, []string{}),
	}
	for i, plan := range tests {
		if plan.Valid() {
			t.Errorf("malformed recovery plan %d was accepted: %#v", i, plan)
		}
	}
}

func TestRunRecoveryPlanValidForRejectsWidenedOrMalformedArgv(t *testing.T) {
	original := []string{"bootwright", "apply", "--mode", "create", "--authorize", "foreign-daemons", "--yes", "--stage", "machines", "--clusters", "ocp", "--ask-become-pass=false", "--trust-on-first-use=false", "--context", "matrix"}
	reconcile := append([]string(nil), original...)
	reconcile[3] = string(ApplyModeReconcile)
	valid := NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, reconcile)
	if !valid.ValidFor(original) {
		t.Fatalf("canonical same-selection reconcile was rejected: %#v", valid)
	}
	tests := []struct {
		name   string
		mutate func([]string) []string
	}{
		{name: "widens clusters", mutate: func(args []string) []string { args[10] = "ocp,other"; return args }},
		{name: "changes intent", mutate: func(args []string) []string { args[3] = string(ApplyModeRebuild); return args }},
		{name: "adds authorization", mutate: func(args []string) []string { args[5] = "foreign-daemons,data-loss"; return args }},
		{name: "changes context", mutate: func(args []string) []string { args[len(args)-1] = "other"; return args }},
		{name: "duplicates selection", mutate: func(args []string) []string { return append(args, "--clusters", "other") }},
		{name: "adds dry run", mutate: func(args []string) []string { return append(args, "--dry-run") }},
		{name: "adds positional tail", mutate: func(args []string) []string { return append(args, "other") }},
		{name: "adds unknown flag", mutate: func(args []string) []string { return append(args, "--future-effect") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.mutate(append([]string(nil), reconcile...))
			plan := NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionReconcileSameSelection}, args)
			if plan.ValidFor(original) {
				t.Fatalf("tampered plan was accepted: %#v", plan)
			}
		})
	}
	retry := NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRetrySameInvocation}, reconcile)
	if retry.ValidFor(original) {
		t.Fatal("retry-same-invocation accepted argv that was not byte-identical to the audit invocation")
	}
}

func TestTaskInterruptionRecoveryPlanIsModeAndBoundaryAware(t *testing.T) {
	rootTargets := []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}
	tests := []struct {
		name string
		mode ApplyMode
		task ApplyTask
		want remedy.Action
	}{
		{name: "create live proof retries exact create", mode: ApplyModeCreate, task: ApplyTask{Entry: TaskLedgerEntry{Kind: ApplyTaskKindControllerNameResolution}, ExecutionClass: ApplyTaskExecutionLiveProof}, want: remedy.ActionRetrySameInvocation},
		{name: "create downstream reconciles", mode: ApplyModeCreate, task: ApplyTask{Entry: TaskLedgerEntry{Kind: ApplyTaskKindMachineInfraPrepare}}, want: remedy.ActionReconcileSameSelection},
		{name: "reconcile ordinary task retries exact", mode: ApplyModeReconcile, task: ApplyTask{Entry: TaskLedgerEntry{Kind: ApplyTaskKindMachineInfraPrepare}}, want: remedy.ActionRetrySameInvocation},
		{name: "rebuild ordinary task retries exact", mode: ApplyModeRebuild, task: ApplyTask{Entry: TaskLedgerEntry{Kind: ApplyTaskKindMachineInfraPrepare}}, want: remedy.ActionRetrySameInvocation},
	}
	for _, mode := range []ApplyMode{ApplyModeCreate, ApplyModeReconcile, ApplyModeRebuild} {
		tests = append(tests, struct {
			name string
			mode ApplyMode
			task ApplyTask
			want remedy.Action
		}{
			name: string(mode) + " controller mutation resumes typed work",
			mode: mode,
			task: ApplyTask{
				Entry:         TaskLedgerEntry{Kind: ApplyTaskKindControllerNameResolution},
				FailureRemedy: remedy.Request{Action: remedy.ActionResumeControllerDNSMutation, Targets: rootTargets},
			},
			want: remedy.ActionResumeControllerDNSMutation,
		})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := []string{"bootwright", "apply", "--mode", string(tc.mode), "--context", "matrix"}
			opts := RunOptions{
				ApplyMode:      tc.mode,
				InvocationArgs: original,
				ResolveRecovery: func(request remedy.Request) (RunRecoveryPlan, error) {
					args := append([]string(nil), original...)
					switch request.Action {
					case remedy.ActionRetrySameInvocation:
					case remedy.ActionReconcileSameSelection:
						args[3] = string(ApplyModeReconcile)
					case remedy.ActionResumeControllerDNSMutation:
						if tc.mode == ApplyModeCreate {
							args[3] = string(ApplyModeReconcile)
						}
					default:
						return RunRecoveryPlan{}, errors.New("unexpected recovery action")
					}
					return NewRunRecoveryPlan(request, args), nil
				},
			}
			plan := taskInterruptionRunRecoveryPlan(opts, tc.task)
			if plan.Request.Action != tc.want || !plan.Valid() {
				t.Fatalf("interruption recovery = %#v, want valid action %q", plan, tc.want)
			}
		})
	}
}

func TestRunApplyTaskGraphPersistsTaskBoundaryRecoveryBeforeLaunchAndCancellation(t *testing.T) {
	original := []string{"bootwright", "apply", "--mode", "create", "--stage", "machines", "--context", "matrix"}
	reconcile := []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "machines", "--context", "matrix"}
	for _, stopAt := range []string{"proof", "downstream"} {
		t.Run(stopAt, func(t *testing.T) {
			dir := t.TempDir()
			runsDir := filepath.Join(dir, "runs")
			opts := schedulerRunOptions(dir)
			opts.ApplyMode = ApplyModeCreate
			opts.InvocationArgs = append([]string(nil), original...)
			opts.ResolveRecovery = func(request remedy.Request) (RunRecoveryPlan, error) {
				switch request.Action {
				case remedy.ActionRetrySameInvocation:
					return NewRunRecoveryPlan(request, original), nil
				case remedy.ActionReconcileSameSelection:
					return NewRunRecoveryPlan(request, reconcile), nil
				default:
					return RunRecoveryPlan{}, errors.New("unexpected recovery action")
				}
			}
			tasks := []ApplyTask{
				{
					Entry:          TaskLedgerEntry{ID: "proof", Kind: ApplyTaskKindControllerNameResolution, Label: "controller proof", Status: TaskStatusPending},
					Playbook:       "proof",
					State:          minimalState(),
					ExecutionClass: ApplyTaskExecutionLiveProof,
					FailureRemedy: remedy.Request{
						Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
						Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}},
					},
				},
				{
					Entry:    TaskLedgerEntry{ID: "downstream", Kind: ApplyTaskKindMachineInfraPrepare, Label: "downstream mutation", Status: TaskStatusPending, Dependencies: []string{"proof"}},
					Playbook: "downstream",
					State:    minimalState(),
				},
			}
			runner := newGatedPlaybookRunner("proof", "downstream")
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan struct {
				ledger RunLedger
				err    error
			}, 1)
			go func() {
				ledger, err := RunApplyTaskGraph(ctx, io.Discard, io.Discard, runsDir, opts,
					ApplyTarget{Name: "machines", PhaseNames: []string{ApplyPhaseMachines}}, "", tasks,
					ConcurrencyLimits{Parallelism: 2}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
				result <- struct {
					ledger RunLedger
					err    error
				}{ledger: ledger, err: err}
			}()
			waitRecoveryTestSignal(t, runner.entered["proof"])
			wantAction := remedy.ActionRetrySameInvocation
			wantArgs := original
			if stopAt == "downstream" {
				runner.releasePlaybook("proof")
				waitRecoveryTestSignal(t, runner.entered["downstream"])
				wantAction = remedy.ActionReconcileSameSelection
				wantArgs = reconcile
			}
			current, found, err := LoadRunLedger(runsDir)
			if err != nil || !found {
				t.Fatalf("LoadRunLedger while %s is active: found=%v err=%v", stopAt, found, err)
			}
			assertRecoveryTestPlan(t, current.Recovery, wantAction, wantArgs)
			cancel()
			completed := waitRecoveryTestResult(t, result)
			if completed.err == nil || completed.ledger.Status != RunStatusCancelled {
				t.Fatalf("cancelled %s run: status=%q err=%v", stopAt, completed.ledger.Status, completed.err)
			}
			var terminal remedy.Error
			if errors.As(completed.err, &terminal) {
				t.Fatalf("cancelled %s task was wrapped with terminal remedy %#v", stopAt, terminal.Remedy())
			}
			assertRecoveryTestPlan(t, completed.ledger.Recovery, wantAction, wantArgs)
			archived, found, err := LoadArchivedRunLedger(runsDir, completed.ledger.RunID)
			if err != nil || !found {
				t.Fatalf("LoadArchivedRunLedger after %s cancellation: found=%v err=%v", stopAt, found, err)
			}
			assertRecoveryTestPlan(t, archived.Recovery, wantAction, wantArgs)
		})
	}
}

func TestRunOneApplyTaskDoesNotAttachTerminalRemedyToDeadline(t *testing.T) {
	dir := t.TempDir()
	opts := schedulerRunOptions(dir)
	runner := &blockingApplyRunner{started: make(chan struct{})}
	task := ApplyTask{
		Entry:    TaskLedgerEntry{ID: "deadline", Kind: ApplyTaskKindProvider, Label: "deadline", Status: TaskStatusPending},
		Playbook: "deadline",
		State:    minimalState(),
		Timeout:  10 * time.Millisecond,
		FailureRemedy: remedy.Request{
			Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
			Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}},
		},
	}
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), "deadline-run", opts, task, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if result.err == nil || !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("deadline result = %v, want context deadline identity", result.err)
	}
	var terminal remedy.Error
	if errors.As(result.err, &terminal) {
		t.Fatalf("deadline was wrapped with terminal remedy %#v", terminal.Remedy())
	}
}

func TestRunApplyTaskGraphKeepsFirstTypedRecoveryWhileLaterWorkRuns(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	original := []string{"bootwright", "apply", "--mode", "create", "--stage", "machines", "--context", "matrix"}
	reconcile := []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "machines", "--context", "matrix"}
	request := remedy.Request{Action: remedy.ActionReconcileSharedServiceThenRetrySameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}}
	repair := []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "fabric", "--clusters", "ocp", "--context", "matrix"}
	terminalPlan := NewRunRecoveryPlan(request, repair, original)
	opts := schedulerRunOptions(dir)
	opts.ApplyMode = ApplyModeCreate
	opts.InvocationArgs = original
	opts.ResolveRecovery = func(got remedy.Request) (RunRecoveryPlan, error) {
		switch got.Action {
		case remedy.ActionReconcileSameSelection:
			return NewRunRecoveryPlan(got, reconcile), nil
		case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
			return terminalPlan, nil
		default:
			return RunRecoveryPlan{}, errors.New("unexpected recovery action")
		}
	}
	runner := &recoveryFailThenGateRunner{laterStarted: make(chan struct{}), releaseLater: make(chan struct{})}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "first", Kind: ApplyTaskKindProvider, Label: "first", Status: TaskStatusPending}, Playbook: "first", State: minimalState(), FailureRemedy: request},
		{Entry: TaskLedgerEntry{ID: "later", Kind: ApplyTaskKindProvider, Label: "later", Status: TaskStatusPending}, Playbook: "later", State: minimalState()},
	}
	completed := make(chan struct {
		ledger RunLedger
		err    error
	}, 1)
	go func() {
		ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts,
			ApplyTarget{Name: "fabric", PhaseNames: []string{ApplyPhaseFabric}}, "", tasks,
			ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		completed <- struct {
			ledger RunLedger
			err    error
		}{ledger: ledger, err: err}
	}()
	waitRecoveryTestSignal(t, runner.laterStarted)
	current, found, err := LoadRunLedger(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLedger while later task runs: found=%v err=%v", found, err)
	}
	assertRecoveryTestPlan(t, current.Recovery, request.Action, repair, original)
	close(runner.releaseLater)
	result := waitRecoveryTestResult(t, completed)
	if result.err == nil || result.ledger.Status != RunStatusFailed {
		t.Fatalf("multi-failure run: status=%q err=%v", result.ledger.Status, result.err)
	}
	assertRecoveryTestPlan(t, result.ledger.Recovery, request.Action, repair, original)
}

func TestRunApplyTaskGraphReplacesInterruptionPlanWithTypedFailureRecovery(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	original := []string{"bootwright", "apply", "--mode", "create", "--stage", "machines", "--context", "matrix"}
	reconcile := []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "machines", "--context", "matrix"}
	request := remedy.Request{
		Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
		Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}},
	}
	repair := []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "fabric", "--clusters", "ocp", "--context", "matrix"}
	resume := append([]string(nil), original...)
	actualPlan := NewRunRecoveryPlan(request, repair, resume)
	opts := schedulerRunOptions(dir)
	opts.ApplyMode = ApplyModeCreate
	opts.InvocationArgs = append([]string(nil), original...)
	opts.ResolveRecovery = func(got remedy.Request) (RunRecoveryPlan, error) {
		switch got.Action {
		case remedy.ActionReconcileSameSelection:
			return NewRunRecoveryPlan(got, reconcile), nil
		case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
			return actualPlan, nil
		default:
			return RunRecoveryPlan{}, errors.New("unexpected recovery action")
		}
	}
	runner := &recordingApplyRunner{failures: map[string]error{"typed-failure": errors.New("boom")}}
	task := ApplyTask{
		Entry:         TaskLedgerEntry{ID: "typed-failure", Kind: ApplyTaskKindProvider, Label: "typed failure", Status: TaskStatusPending},
		Playbook:      "typed-failure",
		State:         minimalState(),
		FailureRemedy: request,
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts,
		ApplyTarget{Name: "fabric", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task},
		ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err == nil || ledger.Status != RunStatusFailed {
		t.Fatalf("typed failure run: status=%q err=%v", ledger.Status, err)
	}
	actualPlan.Request.Targets[0].Name = "changed-after-run"
	actualPlan.Steps[0].Args[0] = "changed-after-run"
	assertRecoveryTestPlan(t, ledger.Recovery, request.Action, repair, resume)
	if !slices.Equal(ledger.InvocationArgs, original) {
		t.Fatalf("immutable audit argv = %#v, want %#v", ledger.InvocationArgs, original)
	}
	archived, found, loadErr := LoadArchivedRunLedger(runsDir, ledger.RunID)
	if loadErr != nil || !found {
		t.Fatalf("LoadArchivedRunLedger: found=%v err=%v", found, loadErr)
	}
	assertRecoveryTestPlan(t, archived.Recovery, request.Action, repair, resume)
}

func assertRecoveryTestPlan(t *testing.T, plan RunRecoveryPlan, action remedy.Action, steps ...[]string) {
	t.Helper()
	if plan.Request.Action != action || len(plan.Steps) != len(steps) {
		t.Fatalf("recovery plan = %#v, want action %q with %d steps", plan, action, len(steps))
	}
	for i := range steps {
		if !slices.Equal(plan.Steps[i].Args, steps[i]) {
			t.Fatalf("recovery step %d = %#v, want %#v", i, plan.Steps[i].Args, steps[i])
		}
	}
}

func waitRecoveryTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for scheduled task")
	}
}

func waitRecoveryTestResult(t *testing.T, result <-chan struct {
	ledger RunLedger
	err    error
}) struct {
	ledger RunLedger
	err    error
} {
	t.Helper()
	select {
	case completed := <-result:
		return completed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled scheduler")
		return struct {
			ledger RunLedger
			err    error
		}{}
	}
}

type recoveryFailThenGateRunner struct {
	laterStarted chan struct{}
	releaseLater chan struct{}
}

func (r *recoveryFailThenGateRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	if spec.Playbook == "first" {
		return errors.New("first failed")
	}
	close(r.laterStarted)
	select {
	case <-r.releaseLater:
		return errors.New("later failed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *recoveryFailThenGateRunner) Command(spec ansible.RunSpec) []string {
	return []string{spec.Playbook}
}
