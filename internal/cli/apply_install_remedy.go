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
	request := remedial.Remedy()
	switch request.Action {
	case remedy.ActionRetrySameInvocation:
		command, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact retry command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; after completing the non-destructive recovery named above, re-run `%s` with exactly the same selected work and intent", err, command.String())
	case remedy.ActionReconcileSameSelection:
		command, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact reconcile command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; re-run `%s` to reconcile exactly this selected work set", err, command.String())
	case remedy.ActionRebuildSameSelection:
		command, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact confirmed-rebuild command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; re-run `%s` to confirm the destructive rebuild again for exactly this selected work set", err, command.String())
	case remedy.ActionRegenerateClusterISO:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return fmt.Errorf("%w; cannot construct the exact ISO-regeneration command: %v", err, targetErr)
		}
		regenerate, commandErr := invocation.regenerateClusterISORetry(cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact ISO-regeneration command: %v", err, commandErr)
		}
		resume, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return fmt.Errorf("%w; regenerate the ISO with `%s`; cannot construct the exact command that resumes this work: %v", err, regenerate.String(), commandErr)
		}
		return fmt.Errorf("%w; regenerate only this cluster's agent ISO with `%s`, then resume the original selected work with `%s`", err, regenerate.String(), resume.String())
	case remedy.ActionDestroyAndReapplyCluster:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster reset sequence: %v", err, targetErr)
		}
		destroy, commandErr := invocation.destroyIncompleteClusterRetry(cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster destroy command: %v", err, commandErr)
		}
		reapply, commandErr := invocation.reapplyDestroyedClusterRetry(cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; destroy the incomplete cluster with `%s`; cannot construct the exact reapply command: %v", err, destroy.String(), commandErr)
		}
		return fmt.Errorf("%w; deliberately reset only this cluster's incomplete install with `%s`, then reinstall it with `%s`", err, destroy.String(), reapply.String())
	case remedy.ActionRebuildCluster:
		cluster, targetErr := singleContainerClusterRemedyTarget(request)
		if targetErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster rebuild command: %v", err, targetErr)
		}
		command, commandErr := invocation.rebuildInstalledClusterRetry(cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster rebuild command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; deliberately rebuild only ContainerCluster/%s with `%s`", err, cluster, command.String())
	default:
		return fmt.Errorf("%w; typed remedy action %q has no CLI formatter, so bootwright refuses to suggest an unsafe command", err, request.Action)
	}
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
