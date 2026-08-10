package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

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
