package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func applyInstallRemedialError(err error, invocation resolvedInvocation) error {
	var remedial remedy.Error
	if !errors.As(err, &remedial) {
		return err
	}
	guidance, formatErr := applyRemedialGuidance(remedial.Remedy(), invocation)
	if formatErr != nil {
		return fmt.Errorf("%w; %v", err, formatErr)
	}
	return fmt.Errorf("%w; %s", err, guidance)
}

func applyRemedialGuidance(request remedy.Request, invocation resolvedInvocation) (string, error) {
	switch request.Action {
	case remedy.ActionRetrySameInvocation:
		command, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact retry command: %v", commandErr)
		}
		return fmt.Sprintf("after completing the non-destructive recovery named above, re-run `%s` with exactly the same selected work and intent", command.String()), nil
	case remedy.ActionReconcileSameSelection:
		command, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact reconcile command: %v", commandErr)
		}
		return fmt.Sprintf("re-run `%s` to reconcile exactly this selected work set", command.String()), nil
	case remedy.ActionReconcileContainerClusterThenRetrySameSelection:
		cluster, prepare, commandErr := containerClusterReconcileCommand(request, invocation)
		if commandErr != nil {
			return "", commandErr
		}
		resume, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact original-selection retry command: %v", commandErr)
		}
		return fmt.Sprintf("reconcile ContainerCluster/%s with `%s`, then retry exactly the original selected work with `%s`", cluster, prepare.String(), resume.String()), nil
	case remedy.ActionRebuildSameSelection:
		command, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact confirmed-rebuild command: %v", commandErr)
		}
		return fmt.Sprintf("re-run `%s` to confirm the destructive rebuild again for exactly this selected work set", command.String()), nil
	case remedy.ActionRegenerateClusterISO:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact ISO-regeneration command: %v", targetErr)
		}
		regenerate, commandErr := invocation.regenerateClusterISORetry(cluster)
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact ISO-regeneration command: %v", commandErr)
		}
		resume, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact command that resumes this work after ISO regeneration: %v", commandErr)
		}
		return fmt.Sprintf("regenerate only this cluster's agent ISO with `%s`, then resume the original selected work with `%s`", regenerate.String(), resume.String()), nil
	case remedy.ActionDestroyAndReapplyCluster:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact cluster reset sequence: %v", targetErr)
		}
		destroy, commandErr := invocation.destroyIncompleteClusterRetry(cluster)
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact cluster destroy command: %v", commandErr)
		}
		reapply, commandErr := invocation.reapplyDestroyedClusterRetry(cluster)
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact reapply command after the cluster destroy: %v", commandErr)
		}
		return fmt.Sprintf("deliberately reset only this cluster's incomplete install with `%s`, then reinstall it with `%s`", destroy.String(), reapply.String()), nil
	case remedy.ActionRebuildCluster:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact cluster rebuild command: %v", targetErr)
		}
		command, commandErr := invocation.rebuildInstalledClusterRetry(cluster)
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact cluster rebuild command: %v", commandErr)
		}
		return fmt.Sprintf("deliberately rebuild only ContainerCluster/%s with `%s`", cluster, command.String()), nil
	case remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:
		machineLayer, clusterLayer, targetErr := protectedLayerRemedyTargets(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact protected-layer teardown and rebuild sequence: %v", targetErr)
		}
		var teardown []string
		if clusterLayer {
			command, commandErr := invocation.destroySelectedClusterLayerRetry()
			if commandErr != nil {
				return "", fmt.Errorf("cannot construct the exact protected cluster-layer destroy command: %v", commandErr)
			}
			teardown = append(teardown, "destroy the protected cluster layer with `"+command.String()+"`")
		}
		if machineLayer {
			command, commandErr := invocation.destroySelectedMachineLayerRetry()
			if commandErr != nil {
				return "", fmt.Errorf("cannot construct the exact protected machine-layer destroy command: %v", commandErr)
			}
			teardown = append(teardown, "destroy the protected machine layer with `"+command.String()+"`")
		}
		resume, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact original-selection rebuild command: %v", commandErr)
		}
		return fmt.Sprintf("%s, then resume exactly the original selected work with `%s`", strings.Join(teardown, " and "), resume.String()), nil
	default:
		return "", fmt.Errorf("typed remedy action %q has no CLI formatter, so bootwright refuses to suggest an unsafe command", request.Action)
	}
}

func containerClusterReconcileCommand(request remedy.Request, invocation resolvedInvocation) (string, retryCommand, error) {
	cluster, err := singleContainerClusterRemedyTarget(request)
	if err != nil {
		return "", retryCommand{}, fmt.Errorf("cannot construct the exact host-cluster reconcile sequence: %v", err)
	}
	command, err := invocation.reconcileContainerClusterRetry(cluster)
	if err != nil {
		return "", retryCommand{}, fmt.Errorf("cannot construct the exact host-cluster reconcile command: %v", err)
	}
	return cluster, command, nil
}

func protectedLayerRemedyTargets(request remedy.Request) (bool, bool, error) {
	if len(request.Targets) == 0 {
		return false, false, fmt.Errorf("action %q requires at least one protected layer", request.Action)
	}
	machineLayer := false
	clusterLayer := false
	for _, target := range request.Targets {
		switch target.Role {
		case remedy.TargetRoleMachineLayer:
			if machineLayer {
				return false, false, fmt.Errorf("action %q repeats protected layer role %q", request.Action, target.Role)
			}
			machineLayer = true
		case remedy.TargetRoleClusterLayer:
			if clusterLayer {
				return false, false, fmt.Errorf("action %q repeats protected layer role %q", request.Action, target.Role)
			}
			clusterLayer = true
		default:
			return false, false, fmt.Errorf("action %q does not accept target role %q", request.Action, target.Role)
		}
	}
	return machineLayer, clusterLayer, nil
}

func hasApplyInstallRemedy(err error) bool {
	var remedial remedy.Error
	return errors.As(err, &remedial)
}

func singleContainerClusterRemedyTarget(request remedy.Request) (string, error) {
	var names []string
	for _, target := range request.Targets {
		if target.Role == remedy.TargetRoleContainerCluster && strings.TrimSpace(target.Name) != "" {
			names = append(names, strings.TrimSpace(target.Name))
		}
	}
	if len(names) != 1 {
		return "", fmt.Errorf("action %q requires exactly one container-cluster target, got %v", request.Action, names)
	}
	return names[0], nil
}
