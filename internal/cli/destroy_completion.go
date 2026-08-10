package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func recordStorageDestroyCompletion(ownershipDir, contextName, runLogPath string, state v1alpha1.State, ledger workflow.RunLedger, storageScopeNames []string, skipUnreachable bool, retry retryCommand) (converge.PartialStorageDestroy, error) {
	expectedNodes := workflow.StorageDestroyExpectedNodesForLedger(state, ledger)
	expectedSeedHosts := workflow.StorageDestroyExpectedSeedHostsForLedger(state, ledger)
	partial, err := converge.RecordPartialStorageDestroy(ownershipDir, contextName, runLogPath, expectedNodes, expectedSeedHosts, skipUnreachable)
	if err != nil {
		return partial, fmt.Errorf("storage teardown completion could not be proved: %w; keeping the converge records, captured secrets and history of storage cluster(s) %s — re-run `%s` once every topology node can produce the terminal proof", err, strings.Join(storageScopeNames, ", "), retry.String())
	}
	return partial, nil
}

func destroyGraphCompletion(ledger workflow.RunLedger, invocation resolvedInvocation) (workflow.DestroyOutcome, error) {
	outcome := workflow.SucceededDestroyTaskKinds(ledger)
	var skipped []string
	for _, task := range ledger.Tasks {
		if task.Status != workflow.TaskStatusSkipped || !workflow.IsDestroyTaskKind(task.Kind) || !workflow.DestroyTaskNeedsCompletionProof(task) {
			continue
		}
		label := task.Label
		if label == "" {
			label = task.ID
		}
		reason := task.SkippedReason
		if reason == "" {
			reason = "no remote hosts matched task limit"
		}
		skipped = append(skipped, label+" ("+reason+")")
	}
	if len(skipped) == 0 {
		return outcome, nil
	}
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		return outcome, err
	}
	return outcome, fmt.Errorf("destroy could not prove completion of required selected teardown task(s): %s; their convergence/install records and substrate-release authorization were kept, so the next apply cannot mistake skipped work for destroyed state. Restore the selected hosts to the generated inventory or correct the desired-state selection, then re-run `%s`", strings.Join(skipped, ", "), retry.String())
}
