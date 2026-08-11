package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
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
	if !workflow.ValidRunRecoveryRequest(request) {
		return "", fmt.Errorf("cannot construct recovery guidance for invalid typed remedy request %q", request.Action)
	}
	switch request.Action {
	case remedy.ActionRetrySameInvocation:
		command, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact retry command: %v", commandErr)
		}
		return fmt.Sprintf("after completing the non-destructive recovery named above, re-run `%s` with exactly the same selected work and intent", command.String()), nil
	case remedy.ActionApplyAllConsumers:
		clusters, targetErr := exactClusterRootRemedyTargets(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact all-consumer apply command: %v", targetErr)
		}
		allConsumers := invocation
		allConsumers.flags.selection.clusters = strings.Join(clusters, ",")
		allConsumers.flags.selection.machines = ""
		command, commandErr := allConsumers.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact all-consumer apply command: %v", commandErr)
		}
		return fmt.Sprintf("run `%s` to apply exactly every cluster root that consumes the shared machine service while preserving this run's stage range and intent", command.String()), nil
	case remedy.ActionResumeControllerDNSMutation:
		if _, targetErr := exactClusterRootRemedyTargets(request); targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact controller-DNS mutation retry: %v", targetErr)
		}
		command, commandErr := controllerNameResolutionRetryInvocation(invocation)
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact controller-DNS mutation retry: %v", commandErr)
		}
		return fmt.Sprintf("re-run `%s` to resume the selected controller-DNS mutation and every dependent task with its safe mode and exact work set", command.String()), nil
	case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
		repair, resume, commandErr := sharedServiceRepairCommands(request, invocation)
		if commandErr != nil {
			return "", commandErr
		}
		return fmt.Sprintf("repair only the shared service for its exact consumer closure with `%s`, then retry exactly the original selected work with `%s`", repair.String(), resume.String()), nil
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
		resume, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return "", fmt.Errorf("cannot construct the exact original-selection resume command after the cluster reinstall: %v", commandErr)
		}
		return fmt.Sprintf("deliberately reset only this cluster's incomplete install with `%s`, reinstall only that cluster with `%s`, then resume exactly the original selected work with `%s`", destroy.String(), reapply.String(), resume.String()), nil
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
		targets, targetErr := protectedLayerRemedyTargets(request)
		if targetErr != nil {
			return "", fmt.Errorf("cannot construct the exact protected-layer teardown and rebuild sequence: %v", targetErr)
		}
		var teardown []string
		if targets.clusterLayer {
			command, commandErr := invocation.destroyClusterLayerRetryForRoots(targets.clusterRoots)
			if commandErr != nil {
				return "", fmt.Errorf("cannot construct the exact protected cluster-layer destroy command: %v", commandErr)
			}
			teardown = append(teardown, "destroy the protected cluster layer with `"+command.String()+"`")
		}
		if targets.machineLayer {
			command, commandErr := invocation.destroyMachineLayerRetryForRoots(targets.machineRoots)
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

func sharedServiceRepairCommands(request remedy.Request, invocation resolvedInvocation) (retryCommand, retryCommand, error) {
	clusters, err := exactClusterRootRemedyTargets(request)
	if err != nil {
		return retryCommand{}, retryCommand{}, fmt.Errorf("cannot construct the exact shared-service repair sequence: %v", err)
	}
	repairInvocation := invocation
	repairInvocation.flags.selection.stage = converge.PhaseFabric
	repairInvocation.flags.selection.through = ""
	repairInvocation.flags.selection.clusters = strings.Join(clusters, ",")
	repairInvocation.flags.selection.machines = ""
	repairInvocation.flags.reclaimDevices = ""
	repair, err := repairInvocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
	if err != nil {
		return retryCommand{}, retryCommand{}, fmt.Errorf("cannot construct the exact shared-service repair command: %v", err)
	}
	resume, err := invocation.retry(retryIntent{})
	if err != nil {
		return retryCommand{}, retryCommand{}, fmt.Errorf("cannot construct the exact original-selection retry command: %v", err)
	}
	return repair, resume, nil
}

func exactClusterRootRemedyTargets(request remedy.Request) ([]string, error) {
	if len(request.Targets) == 0 {
		return nil, fmt.Errorf("action %q requires at least one cluster root", request.Action)
	}
	seen := map[string]bool{}
	clusters := make([]string, 0, len(request.Targets))
	for _, target := range request.Targets {
		name := strings.TrimSpace(target.Name)
		if target.Role != remedy.TargetRoleClusterRoot || name == "" {
			return nil, fmt.Errorf("action %q requires only named %q targets", request.Action, remedy.TargetRoleClusterRoot)
		}
		if seen[name] {
			return nil, fmt.Errorf("action %q repeats cluster root %q", request.Action, name)
		}
		seen[name] = true
		clusters = append(clusters, name)
	}
	sort.Strings(clusters)
	return clusters, nil
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

type protectedLayerTargets struct {
	machineLayer bool
	clusterLayer bool
	machineRoots []string
	clusterRoots []string
}

func protectedLayerRemedyTargets(request remedy.Request) (protectedLayerTargets, error) {
	var out protectedLayerTargets
	if len(request.Targets) == 0 {
		return out, fmt.Errorf("action %q requires at least one protected layer", request.Action)
	}
	seenMachineRoots := map[string]bool{}
	seenClusterRoots := map[string]bool{}
	for _, target := range request.Targets {
		name := strings.TrimSpace(target.Name)
		switch target.Role {
		case remedy.TargetRoleMachineLayer:
			if name != "" || out.machineLayer {
				return out, fmt.Errorf("action %q repeats or names protected layer role %q", request.Action, target.Role)
			}
			out.machineLayer = true
		case remedy.TargetRoleClusterLayer:
			if name != "" || out.clusterLayer {
				return out, fmt.Errorf("action %q repeats or names protected layer role %q", request.Action, target.Role)
			}
			out.clusterLayer = true
		case remedy.TargetRoleMachineLayerRoot:
			if name == "" || seenMachineRoots[name] {
				return out, fmt.Errorf("action %q has a blank or repeated machine-layer cluster root", request.Action)
			}
			seenMachineRoots[name] = true
			out.machineRoots = append(out.machineRoots, name)
		case remedy.TargetRoleClusterLayerRoot:
			if name == "" || seenClusterRoots[name] {
				return out, fmt.Errorf("action %q has a blank or repeated cluster-layer cluster root", request.Action)
			}
			seenClusterRoots[name] = true
			out.clusterRoots = append(out.clusterRoots, name)
		default:
			return out, fmt.Errorf("action %q does not accept target role %q", request.Action, target.Role)
		}
	}
	if out.machineLayer != (len(out.machineRoots) > 0) || out.clusterLayer != (len(out.clusterRoots) > 0) || !out.machineLayer && !out.clusterLayer {
		return out, fmt.Errorf("action %q requires typed desired-state roots for every protected layer", request.Action)
	}
	out.machineRoots = workflow.UnionClusterNames(out.machineRoots)
	out.clusterRoots = workflow.UnionClusterNames(out.clusterRoots)
	return out, nil
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
