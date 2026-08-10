package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func configureApplyRunRecovery(opts *workflow.RunOptions, invocation resolvedInvocation) error {
	request := remedy.Request{Action: remedy.ActionRetrySameInvocation}
	if invocation.flags.mode == workflow.ApplyModeCreate {
		request.Action = remedy.ActionReconcileSameSelection
	}
	plan, err := resolveApplyRunRecovery(request, invocation)
	if err != nil {
		return err
	}
	if !plan.ValidFor(invocation.args()) {
		return fmt.Errorf("cannot construct a validated interruption recovery plan")
	}
	opts.InterruptionRecovery = plan
	opts.ResolveRecovery = func(request remedy.Request) (workflow.RunRecoveryPlan, error) {
		return resolveApplyRunRecovery(request, invocation)
	}
	return nil
}

func configureScopedApplyRunOptions(
	opts *workflow.RunOptions,
	invocation resolvedInvocation,
	runLease *workflow.CommandRunLease,
	overrideAckedReinstalls []string,
	selectedMachines []string,
	availabilityChecker workflow.ClusterAvailabilityChecker,
) error {
	if err := configureApplyRunRecovery(opts, invocation); err != nil {
		return err
	}
	opts.RunLease = runLease
	opts.OverrideAckedReinstalls = overrideAckedReinstalls
	opts.SelectedMachines = selectedMachines
	opts.ClusterAvailabilityChecker = availabilityChecker
	return nil
}

func resolveApplyRunRecovery(request remedy.Request, invocation resolvedInvocation) (workflow.RunRecoveryPlan, error) {
	if !workflow.ValidRunRecoveryRequest(request) {
		return workflow.RunRecoveryPlan{}, fmt.Errorf("typed remedy action %q has malformed targets", request.Action)
	}
	var commands []retryCommand
	switch request.Action {
	case remedy.ActionRetrySameInvocation:
		command, err := invocation.retry(retryIntent{})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact retry command: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionApplyAllConsumers:
		clusters, err := exactClusterRootRemedyTargets(request)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact all-consumer apply command: %v", err)
		}
		allConsumers := invocation
		allConsumers.flags.selection.clusters = strings.Join(clusters, ",")
		allConsumers.flags.selection.machines = ""
		command, err := allConsumers.retry(retryIntent{})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact all-consumer apply command: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionResumeControllerDNSMutation:
		if _, err := exactClusterRootRemedyTargets(request); err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact controller-DNS mutation retry: %v", err)
		}
		command, err := controllerNameResolutionRetryInvocation(invocation)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact controller-DNS mutation retry: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
		repair, resume, err := sharedServiceRepairCommands(request, invocation)
		if err != nil {
			return workflow.RunRecoveryPlan{}, err
		}
		commands = append(commands, repair, resume)
	case remedy.ActionReconcileSameSelection:
		command, err := invocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact reconcile command: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionReconcileContainerClusterThenRetrySameSelection:
		_, prepare, err := containerClusterReconcileCommand(request, invocation)
		if err != nil {
			return workflow.RunRecoveryPlan{}, err
		}
		resume, err := invocation.retry(retryIntent{})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact original-selection retry command: %v", err)
		}
		commands = append(commands, prepare, resume)
	case remedy.ActionRebuildSameSelection:
		command, err := invocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact confirmed-rebuild command: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionRegenerateClusterISO:
		cluster, err := singleContainerClusterRemedyTarget(request)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact ISO-regeneration command: %v", err)
		}
		regenerate, err := invocation.regenerateClusterISORetry(cluster)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact ISO-regeneration command: %v", err)
		}
		resume, err := invocation.retry(retryIntent{})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact command that resumes this work after ISO regeneration: %v", err)
		}
		commands = append(commands, regenerate, resume)
	case remedy.ActionDestroyAndReapplyCluster:
		cluster, err := singleContainerClusterRemedyTarget(request)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact cluster reset sequence: %v", err)
		}
		destroy, err := invocation.destroyIncompleteClusterRetry(cluster)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact cluster destroy command: %v", err)
		}
		reapply, err := invocation.reapplyDestroyedClusterRetry(cluster)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact reapply command after the cluster destroy: %v", err)
		}
		commands = append(commands, destroy, reapply)
	case remedy.ActionRebuildCluster:
		cluster, err := singleContainerClusterRemedyTarget(request)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact cluster rebuild command: %v", err)
		}
		command, err := invocation.rebuildInstalledClusterRetry(cluster)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact cluster rebuild command: %v", err)
		}
		commands = append(commands, command)
	case remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:
		machineLayer, clusterLayer, err := protectedLayerRemedyTargets(request)
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact protected-layer teardown and rebuild sequence: %v", err)
		}
		if clusterLayer {
			command, commandErr := invocation.destroySelectedClusterLayerRetry()
			if commandErr != nil {
				return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact protected cluster-layer destroy command: %v", commandErr)
			}
			commands = append(commands, command)
		}
		if machineLayer {
			command, commandErr := invocation.destroySelectedMachineLayerRetry()
			if commandErr != nil {
				return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact protected machine-layer destroy command: %v", commandErr)
			}
			commands = append(commands, command)
		}
		resume, err := invocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
		if err != nil {
			return workflow.RunRecoveryPlan{}, fmt.Errorf("cannot construct the exact original-selection rebuild command: %v", err)
		}
		commands = append(commands, resume)
	default:
		return workflow.RunRecoveryPlan{}, fmt.Errorf("typed remedy action %q has no CLI recovery resolver", request.Action)
	}
	steps := make([][]string, 0, len(commands))
	for _, command := range commands {
		steps = append(steps, command.Args())
	}
	plan := workflow.NewRunRecoveryPlan(request, steps...)
	if !plan.ValidFor(invocation.args()) {
		return workflow.RunRecoveryPlan{}, fmt.Errorf("typed remedy action %q produced an invalid recovery plan", request.Action)
	}
	return plan, nil
}
