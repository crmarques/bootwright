package oc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func WaitStepRequirements(ctx context.Context, runner OCRunner, kubeconfig, addon, step string, checks []v1alpha1.ClusterAddonReadinessCheck, timeoutStr string, startedAt time.Time, pollInterval time.Duration, progress io.Writer) error {
	if len(checks) == 0 {
		return nil
	}
	budget, err := newWaitBudget(timeoutStr, startedAt)
	if err != nil {
		return err
	}
	pending := append([]v1alpha1.ClusterAddonReadinessCheck(nil), checks...)
	_, err = pollUntilReady(ctx, budget, pollInterval, progress, false,
		func(checkCtx context.Context) (bool, string, error) {
			observedPending, detail, checkErr := unsatisfiedChecks(checkCtx, runner, kubeconfig, checks)
			if checkErr == nil {
				pending = observedPending
			}
			return len(observedPending) == 0, detail, checkErr
		},
		func(diagnosisCtx context.Context, last string, tracker *waitProgress) (string, error) {
			tracker.line(fmt.Sprintf("diagnosing why step %s of ClusterAddon/%s is still waiting for the API its manifests need:", step, addon))
			cause := diagnoseChecks(diagnosisCtx, runner, kubeconfig, pending, tracker)
			return last, stepRequirementTimeout(step, pending, budget.timeout, last, cause)
		})
	return err
}

func stepRequirementTimeout(step string, pending []v1alpha1.ClusterAddonReadinessCheck, timeout time.Duration, lastObserved, cause string) error {
	base := fmt.Sprintf("step %s requires %s, which did not appear before the %s overall readiness budget expired", step, describeChecks(pending), timeout)
	if strings.TrimSpace(cause) != "" {
		return fmt.Errorf("%s: %s", base, cause)
	}
	if strings.TrimSpace(lastObserved) != "" {
		return fmt.Errorf("%s; last observed: %s", base, lastObserved)
	}
	return fmt.Errorf("%s; see the apply log for details", base)
}

func unsatisfiedChecks(ctx context.Context, runner OCRunner, kubeconfig string, checks []v1alpha1.ClusterAddonReadinessCheck) ([]v1alpha1.ClusterAddonReadinessCheck, string, error) {
	outcomes := readinessOutcomes(ctx, runner, kubeconfig, checks)
	var pending []v1alpha1.ClusterAddonReadinessCheck
	details := make([]string, 0, len(outcomes))
	for i, outcome := range outcomes {
		if outcome.err != nil {
			return nil, "", outcome.err
		}
		if outcome.detail != "" {
			details = append(details, outcome.detail)
		}
		if !outcome.ready {
			pending = append(pending, checks[i])
		}
	}
	return pending, strings.Join(details, "; "), nil
}

func describeChecks(checks []v1alpha1.ClusterAddonReadinessCheck) string {
	described := make([]string, 0, len(checks))
	for _, check := range checks {
		described = append(described, describeCheck(check))
	}
	return strings.Join(described, " and ")
}

func describeCheck(check v1alpha1.ClusterAddonReadinessCheck) string {
	switch {
	case check.CSVSucceeded != nil:
		return "subscription.operators.coreos.com/" + check.CSVSucceeded.Subscription + " CSV Succeeded"
	case check.Condition != nil:
		condition := check.Condition
		return resourceArg(condition.APIVersion, condition.Kind) + "/" + condition.Name + " " + condition.Condition.Type + "=" + condition.Condition.Status
	case check.ResourceExists != nil:
		exists := check.ResourceExists
		return resourceArg(exists.APIVersion, exists.Kind) + "/" + exists.Name
	default:
		return "an unrecognized requirement"
	}
}
