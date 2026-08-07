package oc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func WaitReady(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, pollInterval time.Duration, progress io.Writer) (string, error) {
	timeout, err := parsePositiveDuration(extension.Spec.Readiness.Timeout)
	if err != nil {
		return "", err
	}
	if len(extension.Spec.Readiness.Checks) == 0 {
		return "no readiness checks declared", nil
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
		ready, observed, err := Ready(ctx, runner, kubeconfig, extension)
		if ready && err == nil {
			tracker.done(currentObservation(observed))
			return observed, nil
		}
		if observed != "" {
			last = observed
		} else if err != nil {
			last = err.Error()
		}
		tracker.observe(currentObservation(last))
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return last, parent.Err()
			}
			return readinessTimeout(parent, runner, kubeconfig, extension, timeout, last, tracker)
		case <-ticker.C:
		}
	}
}

func readinessTimeout(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, timeout time.Duration, lastObserved string, tracker *waitProgress) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, diagnosisBudget)
	defer cancel()
	pending, observed, err := unsatisfiedChecks(ctx, runner, kubeconfig, extension.Spec.Readiness.Checks)
	if err == nil && observed != "" {
		lastObserved = observed
	}
	if len(pending) == 0 {
		return lastObserved, fmt.Errorf("ClusterAddon/%s readiness timed out after %s; last observed: %s", extension.Metadata.Name, timeout, lastObserved)
	}
	tracker.line(fmt.Sprintf("diagnosing why ClusterAddon/%s is not ready:", extension.Metadata.Name))
	cause := diagnoseChecks(ctx, runner, kubeconfig, pending, tracker)
	base := fmt.Sprintf("ClusterAddon/%s readiness timed out after %s waiting for %s", extension.Metadata.Name, timeout, describeChecks(pending))
	if strings.TrimSpace(cause) != "" {
		return lastObserved, fmt.Errorf("%s: %s", base, cause)
	}
	return lastObserved, fmt.Errorf("%s; last observed: %s", base, lastObserved)
}

const readinessFanout = 4

type readinessOutcome struct {
	ready  bool
	detail string
	err    error
}

func Ready(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon) (bool, string, error) {
	outcomes := readinessOutcomes(ctx, runner, kubeconfig, extension.Spec.Readiness.Checks)
	var observed []string
	for _, outcome := range outcomes {
		if outcome.err != nil {
			return false, strings.Join(append(observed, outcome.detail), "; "), outcome.err
		}
		observed = append(observed, outcome.detail)
		if !outcome.ready {
			return false, strings.Join(observed, "; "), nil
		}
	}
	return true, strings.Join(observed, "; "), nil
}

func readinessOutcomes(ctx context.Context, runner OCRunner, kubeconfig string, checks []v1alpha1.ClusterAddonReadinessCheck) []readinessOutcome {
	outcomes := make([]readinessOutcome, len(checks))
	if len(checks) == 0 {
		return outcomes
	}
	if len(checks) == 1 {
		ready, detail, err := checkReady(ctx, runner, kubeconfig, checks[0])
		outcomes[0] = readinessOutcome{ready: ready, detail: detail, err: err}
		return outcomes
	}
	slots := make(chan struct{}, readinessFanout)
	var wg sync.WaitGroup
	for i := range checks {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			ready, detail, err := checkReady(ctx, runner, kubeconfig, checks[index])
			outcomes[index] = readinessOutcome{ready: ready, detail: detail, err: err}
		}(i)
	}
	wg.Wait()
	return outcomes
}

func checkReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonReadinessCheck) (bool, string, error) {
	switch {
	case check.CSVSucceeded != nil:
		return csvSucceeded(ctx, runner, kubeconfig, check.CSVSucceeded.Namespace, check.CSVSucceeded.Subscription)
	case check.Condition != nil:
		return conditionReady(ctx, runner, kubeconfig, *check.Condition)
	case check.ResourceExists != nil:
		exists := check.ResourceExists
		resource := resourceArg(exists.APIVersion, exists.Kind) + "/" + exists.Name
		_, err := getResource(ctx, runner, kubeconfig, exists.APIVersion, exists.Kind, exists.Namespace, exists.Name)
		if err != nil {
			return false, resource + " NotFound", nil
		}
		return true, resource + " Exists", nil
	default:
		return false, "", fmt.Errorf("readiness check must set exactly one arm")
	}
}

func csvSucceeded(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription string) (bool, string, error) {
	subscriptionResource := "subscription.operators.coreos.com/" + subscription
	sub, err := getNamedResource(ctx, runner, kubeconfig, "subscription.operators.coreos.com", namespace, subscription)
	if err != nil {
		return false, subscriptionResource + " Unavailable", nil
	}
	csv := nestedString(sub, "status", "installedCSV")
	if csv == "" {
		return false, subscriptionResource + " Pending", nil
	}
	csvResource := "clusterserviceversion.operators.coreos.com/" + csv
	csvObj, err := getNamedResource(ctx, runner, kubeconfig, "clusterserviceversion.operators.coreos.com", namespace, csv)
	if err != nil {
		return false, csvResource + " Unavailable", nil
	}
	phase := nestedString(csvObj, "status", "phase")
	if phase != "Succeeded" {
		return false, csvResource + " " + orUnknown(phase), nil
	}
	return true, csvResource + " Succeeded", nil
}

func conditionReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonConditionReadiness) (bool, string, error) {
	resource := resourceArg(check.APIVersion, check.Kind) + "/" + check.Name
	obj, err := getResource(ctx, runner, kubeconfig, check.APIVersion, check.Kind, check.Namespace, check.Name)
	if err != nil {
		return false, resource + " Unavailable", nil
	}
	phase := nestedString(obj, "status", "phase")
	conditions, _ := nestedValue(obj, "status", "conditions").([]any)
	newest := newestHeartbeat(conditions)
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(condition["type"]) != check.Condition.Type {
			continue
		}
		if stringValue(condition["status"]) == check.Condition.Status {
			return true, resource + " " + firstNonEmpty(phase, check.Condition.Type), nil
		}
		state := firstNonEmpty(phase, stringValue(condition["reason"]))
		if state == "" {
			state = check.Condition.Type + "=" + orUnknown(stringValue(condition["status"]))
		}
		if lag, stale := conditionLag(condition, newest); stale {
			state += fmt.Sprintf(" (%s=%s unchanged for %s while this object's other conditions keep updating)",
				check.Condition.Type, orUnknown(stringValue(condition["status"])), lag)
		}
		return false, resource + " " + state, nil
	}
	return false, resource + " " + firstNonEmpty(phase, "Unknown"), nil
}

func currentObservation(detail string) string {
	if idx := strings.LastIndex(detail, "; "); idx >= 0 {
		return detail[idx+2:]
	}
	return detail
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}

func getResource(ctx context.Context, runner OCRunner, kubeconfig, apiVersion, kind, namespace, name string) (map[string]any, error) {
	return getNamedResource(ctx, runner, kubeconfig, resourceArg(apiVersion, kind), namespace, name)
}

func resourceArg(apiVersion, kind string) string {
	group := apiVersion
	if slash := strings.Index(apiVersion, "/"); slash >= 0 {
		group = apiVersion[:slash]
	}
	if group == "" || group == "v1" {
		return kind
	}
	return strings.ToLower(kind) + "." + group
}
