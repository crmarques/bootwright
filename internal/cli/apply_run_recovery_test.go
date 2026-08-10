package cli

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
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func TestConfigureApplyRunRecoveryKeepsAuditArgvAndResolvesModeAwareInterruption(t *testing.T) {
	for _, tc := range []struct {
		mode workflow.ApplyMode
		want remedy.Action
	}{
		{mode: workflow.ApplyModeCreate, want: remedy.ActionReconcileSameSelection},
		{mode: workflow.ApplyModeReconcile, want: remedy.ActionRetrySameInvocation},
		{mode: workflow.ApplyModeRebuild, want: remedy.ActionRetrySameInvocation},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			invocation := recoveryTestInvocation(tc.mode)
			original := invocation.args()
			opts := workflow.RunOptions{InvocationArgs: append([]string(nil), original...)}
			if err := configureApplyRunRecovery(&opts, invocation); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.InvocationArgs, original) {
				t.Fatalf("immutable audit argv = %#v, want %#v", opts.InvocationArgs, original)
			}
			if opts.InterruptionRecovery.Request.Action != tc.want || !opts.InterruptionRecovery.Valid() {
				t.Fatalf("interruption plan = %#v, want valid action %q", opts.InterruptionRecovery, tc.want)
			}
			intent := retryIntent{}
			if tc.mode == workflow.ApplyModeCreate {
				intent.mode = workflow.ApplyModeReconcile
			}
			wantCommand, err := invocation.retry(intent)
			if err != nil {
				t.Fatal(err)
			}
			if got := opts.InterruptionRecovery.Steps[0].Args; !slices.Equal(got, wantCommand.Args()) {
				t.Fatalf("interruption argv = %#v, want %#v", got, wantCommand.Args())
			}
		})
	}
}

func TestResolveApplyRunRecoveryCoversEveryRegisteredAction(t *testing.T) {
	invocation := recoveryTestInvocation(workflow.ApplyModeCreate)
	container := []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "ocp"}}
	root := []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}
	requests := map[remedy.Action]remedy.Request{
		remedy.ActionRetrySameInvocation:                             {Action: remedy.ActionRetrySameInvocation},
		remedy.ActionApplyAllConsumers:                               {Action: remedy.ActionApplyAllConsumers, Targets: root},
		remedy.ActionResumeControllerDNSMutation:                     {Action: remedy.ActionResumeControllerDNSMutation, Targets: root},
		remedy.ActionReconcileSharedServiceThenRetrySameSelection:    {Action: remedy.ActionReconcileSharedServiceThenRetrySameSelection, Targets: root},
		remedy.ActionReconcileSameSelection:                          {Action: remedy.ActionReconcileSameSelection},
		remedy.ActionReconcileContainerClusterThenRetrySameSelection: {Action: remedy.ActionReconcileContainerClusterThenRetrySameSelection, Targets: container},
		remedy.ActionRebuildSameSelection:                            {Action: remedy.ActionRebuildSameSelection},
		remedy.ActionRegenerateClusterISO:                            {Action: remedy.ActionRegenerateClusterISO, Targets: container},
		remedy.ActionDestroyAndReapplyCluster:                        {Action: remedy.ActionDestroyAndReapplyCluster, Targets: container},
		remedy.ActionRebuildCluster:                                  {Action: remedy.ActionRebuildCluster, Targets: container},
		remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:  {Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}, {Role: remedy.TargetRoleClusterLayer}}},
	}
	registered := remedy.RegisteredActions()
	for _, action := range registered {
		t.Run(string(action), func(t *testing.T) {
			request, ok := requests[action]
			if !ok {
				t.Fatalf("registered action %q has no durable CLI resolver fixture", action)
			}
			plan, err := resolveApplyRunRecovery(request, invocation)
			if err != nil {
				t.Fatalf("resolve registered action %q: %v", action, err)
			}
			if !plan.Matches(request) || !plan.Valid() {
				t.Fatalf("resolved plan for %q is not typed and executable: %#v", action, plan)
			}
		})
	}
	if len(requests) != len(registered) {
		t.Fatalf("durable CLI resolver has %d action fixtures for %d registered actions", len(requests), len(registered))
	}
}

func TestControllerRecoveryImmediateGuidanceMatchesPersistedArgv(t *testing.T) {
	root := []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}}
	for _, mode := range []workflow.ApplyMode{workflow.ApplyModeCreate, workflow.ApplyModeReconcile, workflow.ApplyModeRebuild} {
		for _, action := range []remedy.Action{
			remedy.ActionResumeControllerDNSMutation,
			remedy.ActionReconcileSharedServiceThenRetrySameSelection,
		} {
			t.Run(string(mode)+"/"+string(action), func(t *testing.T) {
				invocation := recoveryTestInvocation(mode)
				request := remedy.Request{Action: action, Targets: root}
				guidance, err := applyRemedialGuidance(request, invocation)
				if err != nil {
					t.Fatal(err)
				}
				commands := backtickedBootwrightCommands(guidance)
				plan, err := resolveApplyRunRecovery(request, invocation)
				if err != nil {
					t.Fatal(err)
				}
				if len(commands) != len(plan.Steps) {
					t.Fatalf("immediate commands = %v, persisted steps = %#v", commands, plan.Steps)
				}
				for i := range commands {
					immediate := shellParseWords(t, commands[i])
					if !slices.Equal(immediate, plan.Steps[i].Args) {
						t.Fatalf("controller recovery step %d drifted: immediate=%#v persisted=%#v", i, immediate, plan.Steps[i].Args)
					}
				}
				if action == remedy.ActionResumeControllerDNSMutation {
					wantMode := mode
					if mode == workflow.ApplyModeCreate {
						wantMode = workflow.ApplyModeReconcile
					}
					assertRecoveryFlagValue(t, plan.Steps[0].Args, "--mode", string(wantMode))
					assertRecoveryFlagValue(t, plan.Steps[0].Args, "--stage", "fabric")
					assertRecoveryFlagValue(t, plan.Steps[0].Args, "--through", "machines")
				}
			})
		}
	}
}

func TestProtectedLayerRecoveryProjectsAuthorizeAllAcrossVerbs(t *testing.T) {
	invocation := recoveryTestInvocation(workflow.ApplyModeRebuild)
	invocation.flags.authorizations = []string{authorizeAll}
	request := remedy.Request{
		Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection,
		Targets: []remedy.Target{
			{Role: remedy.TargetRoleMachineLayer},
			{Role: remedy.TargetRoleClusterLayer},
		},
	}
	plan, err := resolveApplyRunRecovery(request, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ValidFor(invocation.args()) || len(plan.Steps) != 3 {
		t.Fatalf("authorize-all protected recovery = %#v, want two destroy steps and one apply step", plan)
	}
	for _, step := range plan.Steps[:2] {
		assertRecoveryFlagValue(t, step.Args, "--authorize", "data-loss,unowned-devices,protected")
	}
	assertRecoveryFlagValue(t, plan.Steps[2].Args, "--authorize", authorizeAll)
	guidance, err := applyRemedialGuidance(request, invocation)
	if err != nil {
		t.Fatal(err)
	}
	commands := backtickedBootwrightCommands(guidance)
	for i := range commands {
		if got := shellParseWords(t, commands[i]); !slices.Equal(got, plan.Steps[i].Args) {
			t.Fatalf("authorize-all step %d drifted: immediate=%#v persisted=%#v", i, got, plan.Steps[i].Args)
		}
	}
}

func TestMalformedTypedRecoveryIsCommandFreeImmediatelyAndDurably(t *testing.T) {
	invocation := recoveryTestInvocation(workflow.ApplyModeCreate)
	request := remedy.Request{
		Action: remedy.ActionRebuildCluster,
		Targets: []remedy.Target{
			{Role: remedy.TargetRoleContainerCluster, Name: "ocp"},
			{Role: remedy.TargetRoleClusterRoot, Name: "ocp"},
		},
	}
	guidance, guidanceErr := applyRemedialGuidance(request, invocation)
	if guidanceErr == nil || guidance != "" {
		t.Fatalf("malformed immediate recovery: guidance=%q err=%v", guidance, guidanceErr)
	}
	if commands := backtickedBootwrightCommands(guidanceErr.Error()); len(commands) != 0 {
		t.Fatalf("malformed immediate recovery advertised commands %v", commands)
	}
	plan, planErr := resolveApplyRunRecovery(request, invocation)
	if planErr == nil || plan.Request.Action != "" || len(plan.Steps) != 0 {
		t.Fatalf("malformed durable recovery: plan=%#v err=%v", plan, planErr)
	}
}

func TestCancelledApplyHasNoTerminalImmediateRemedyAndArchivesInterruptionPlan(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	invocation := recoveryTestInvocation(workflow.ApplyModeCreate)
	opts := workflow.RunOptions{
		ContextName:        "matrix",
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
		ApplyMode:          workflow.ApplyModeCreate,
		InvocationArgs:     invocation.args(),
	}
	if err := configureApplyRunRecovery(&opts, invocation); err != nil {
		t.Fatal(err)
	}
	runner := &recoveryCancellationRunner{started: make(chan struct{})}
	task := workflow.ApplyTask{
		Entry:          workflow.TaskLedgerEntry{ID: "proof", Kind: workflow.ApplyTaskKindControllerNameResolution, Label: "controller proof", Status: workflow.TaskStatusPending},
		Playbook:       "proof",
		ExecutionClass: workflow.ApplyTaskExecutionLiveProof,
		FailureRemedy: remedy.Request{
			Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
			Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "ocp"}},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	completed := make(chan struct {
		ledger workflow.RunLedger
		err    error
	}, 1)
	go func() {
		ledger, err := workflow.RunApplyTaskGraph(ctx, io.Discard, io.Discard, runsDir, opts,
			workflow.ApplyTarget{Name: "machines", PhaseNames: []string{workflow.ApplyPhaseMachines}}, "", []workflow.ApplyTask{task},
			workflow.ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		completed <- struct {
			ledger workflow.RunLedger
			err    error
		}{ledger: ledger, err: err}
	}()
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for live proof")
	}
	cancel()
	var result struct {
		ledger workflow.RunLedger
		err    error
	}
	select {
	case result = <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled apply")
	}
	if result.err == nil || !errors.Is(result.err, context.Canceled) || result.ledger.Status != workflow.RunStatusCancelled {
		t.Fatalf("cancelled apply: status=%q err=%v", result.ledger.Status, result.err)
	}
	if hasApplyInstallRemedy(result.err) {
		t.Fatalf("cancelled apply returned a terminal typed remedy: %v", result.err)
	}
	immediate := applyInstallRemedialError(result.err, invocation)
	if commands := backtickedBootwrightCommands(immediate.Error()); len(commands) != 0 {
		t.Fatalf("cancelled apply advertised terminal immediate commands %v", commands)
	}
	archived, found, err := workflow.LoadArchivedRunLedger(runsDir, result.ledger.RunID)
	if err != nil || !found {
		t.Fatalf("LoadArchivedRunLedger: found=%v err=%v", found, err)
	}
	hints := status.LedgerNextSteps(archived, workflow.RunActivity{}, nil)
	want := retryCommand{args: invocation.args()}.String()
	if len(hints) == 0 || hints[0] != want {
		t.Fatalf("archived cancellation recovery = %v, want exact interruption argv first: %q", hints, want)
	}
}

func recoveryTestInvocation(mode workflow.ApplyMode) resolvedInvocation {
	return resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            mode,
			selection:       runSelection{stage: "machines", clusters: "ocp"},
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: false,
		},
	}
}

func assertRecoveryFlagValue(t *testing.T, args []string, flag, want string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == want {
			return
		}
	}
	t.Fatalf("argv %#v missing %s %q", args, flag, want)
}

type recoveryCancellationRunner struct {
	started chan struct{}
}

func (r *recoveryCancellationRunner) Run(ctx context.Context, _ ansible.RunSpec) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

func (r *recoveryCancellationRunner) Command(ansible.RunSpec) []string {
	return []string{"proof"}
}
