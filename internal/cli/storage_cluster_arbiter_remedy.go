package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
)

func arbiterLivePlanRefusal(err error, state v1alpha1.State, runsDir string, invocation resolvedInvocation) error {
	var liveErr *arbiter.LivePlanError
	if !errors.As(err, &liveErr) {
		return err
	}
	switch liveErr.Failure {
	case arbiter.LivePlanStretchModeDisabled:
		action, proofErr := arbiterStorageCreatePrerequisite(state, runsDir, liveErr.Cluster)
		if proofErr != nil {
			return fmt.Errorf("%w; Bootwright could not prove that an apply would run the storage-cluster task and establish the authored stretch mode: %v. Inspect and repair the recorded convergence evidence before deciding whether this is an incomplete bootstrap or an unsupported live shape change; no bootwright retry command safely performs that transition", err, proofErr)
		}
		if action != workflow.ApplyTransitionCreate {
			return fmt.Errorf("%w; the storage-cluster task classifies as %s rather than create, so a plain apply may skip or refuse and is not a safe remedy. Diagnose why the live monmap disagrees with the recorded bootstrap shape, or perform a separately reviewed teardown and fresh apply after protecting the data; no bootwright retry command safely performs that transition", err, action)
		}
		apply, applyErr := invocation.applyClustersRetry([]string{liveErr.Cluster})
		if applyErr != nil {
			return fmt.Errorf("%w; the storage-cluster task is positively classified to create, but Bootwright cannot construct its exact apply prerequisite: %v", err, applyErr)
		}
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			return fmt.Errorf("%w; the storage-cluster task is positively classified to create and its exact apply prerequisite is `%s`, but Bootwright cannot construct the exact original replacement retry: %v", err, apply.String(), retryErr)
		}
		if invocation.flags.dryRun {
			return fmt.Errorf("%w; the storage-cluster task has no completed convergence record and is positively classified to create. Preview the exact apply prerequisite with `%s`; that continuation remains read-only because the original invocation was a preview. Establishing the authored stretch mode requires a separate explicit real apply decision for that same scope; after it completes, retry the original read-only replacement with `%s`", err, apply.String(), retry.String())
		}
		return fmt.Errorf("%w; the storage-cluster task has no completed convergence record and is positively classified to create, so establish the authored stretch mode with `%s`, then retry the original replacement with `%s`", err, apply.String(), retry.String())
	case arbiter.LivePlanTiebreakerMissing:
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			return fmt.Errorf("%w; Bootwright cannot construct the exact original replacement retry: %v", err, retryErr)
		}
		cephRepair := shellquote.QuoteWords([]string{"ceph", "mon", "set_new_tiebreaker", liveErr.DesiredMon})
		return fmt.Errorf("%w; on the cluster, first prove mon.%s is in the monmap, in quorum, and carries the authored stretch location, then repair the missing live tiebreaker with `%s`; after verifying tiebreaker_mon is mon.%s, retry the original replacement with `%s`", err, liveErr.DesiredMon, cephRepair, liveErr.DesiredMon, retry.String())
	case arbiter.LivePlanResidueAmbiguous:
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			return fmt.Errorf("%w; Bootwright cannot construct the exact original replacement retry: %v", err, retryErr)
		}
		return fmt.Errorf("%w; inspect the exact undeclared mons %s and their cephadm hosts on the cluster, identify which are proven residue from interrupted replacements, and retire only that proven residue out of band. Once the live monmap and orchestrator host list are unambiguous, retry the original replacement with `%s`", err, strings.Join(liveErr.StrayMons, ", "), retry.String())
	case arbiter.LivePlanStateUnreadable:
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			return fmt.Errorf("%w; Bootwright cannot construct the exact original replacement retry: %v", err, retryErr)
		}
		return fmt.Errorf("%w; restore access to the cluster and obtain a readable `ceph mon dump` before changing the tiebreaker, then retry the exact original read-only or mutating invocation with `%s`", err, retry.String())
	default:
		return fmt.Errorf("%w; unrecognized typed arbiter evidence %q is fail-closed and has no executable remedy", err, liveErr.Failure)
	}
}

func arbiterStorageCreatePrerequisite(state v1alpha1.State, runsDir, cluster string) (workflow.ApplyTransitionAction, error) {
	tasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), state)
	if err != nil {
		return "", err
	}
	var storageTasks []workflow.ApplyTask
	for _, task := range tasks {
		if task.Entry.Kind == workflow.ApplyTaskKindStorageCluster && task.Entry.Cluster == cluster {
			storageTasks = append(storageTasks, task)
		}
	}
	if len(storageTasks) != 1 {
		return "", fmt.Errorf("expected exactly one storage-cluster task for %s, found %d", cluster, len(storageTasks))
	}
	objects, err := workflow.ClassifyApplyObjects(storageTasks, runsDir)
	if err != nil {
		return "", err
	}
	for _, transition := range workflow.ClassifyApplyTransitions(objects, workflow.ApplyModeReconcile) {
		if transition.Kind == workflow.ObjectKindStorageCluster {
			return transition.Action, nil
		}
	}
	return "", fmt.Errorf("storage-cluster task for %s produced no StorageCluster transition", cluster)
}

func arbiterPrepareRunError(err error, applyInvocation, replaceInvocation resolvedInvocation) error {
	prepared := applyInstallRemedialError(err, applyInvocation)
	retry, retryErr := replaceInvocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; after resolving the typed machine-preparation failure, Bootwright cannot safely construct the exact replacement retry: %v", prepared, retryErr)
	}
	return fmt.Errorf("%w; after the machine-preparation remedy completes, retry the exact original replacement with `%s`", prepared, retry.String())
}
