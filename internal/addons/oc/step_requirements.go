package oc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func WaitStepRequirements(ctx context.Context, runner OCRunner, kubeconfig, addon, step string, checks []v1alpha1.ClusterAddonReadinessCheck, timeoutStr string, pollInterval time.Duration, progress io.Writer) error {
	if len(checks) == 0 {
		return nil
	}
	timeout, err := parsePositiveDuration(timeoutStr)
	if err != nil {
		return err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if pollInterval <= 0 {
		pollInterval = WaitInterval(timeout)
	}
	tracker := startWaitProgress(progress)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		pending, detail, err := unsatisfiedChecks(ctx, runner, kubeconfig, checks)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			tracker.done(detail)
			return nil
		}
		if detail != "" {
			last = detail
		}
		tracker.observe(currentObservation(last))
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			diagCtx, diagCancel := context.WithTimeout(parent, diagnosisBudget)
			tracker.line(fmt.Sprintf("diagnosing why step %s of ClusterAddon/%s is still waiting for the API its manifests need:", step, addon))
			cause := diagnoseChecks(diagCtx, runner, kubeconfig, pending, tracker)
			diagCancel()
			return stepRequirementTimeout(step, pending, timeout, last, cause)
		case <-ticker.C:
		}
	}
}

func stepRequirementTimeout(step string, pending []v1alpha1.ClusterAddonReadinessCheck, timeout time.Duration, lastObserved, cause string) error {
	base := fmt.Sprintf("step %s requires %s, which did not appear within %s", step, describeChecks(pending), timeout)
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
