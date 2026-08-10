package oc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
)

type ReadinessResult struct {
	Ready           bool
	Detail          string
	CSVObservations []extensionrecords.CSVObservation
}

func WaitReady(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, pollInterval time.Duration, progress io.Writer) (string, error) {
	budget, err := newWaitBudget(extension.Spec.Readiness.Timeout, time.Time{})
	if err != nil {
		return "", err
	}
	result, err := waitReadiness(ctx, runner, kubeconfig, extension, budget, pollInterval, progress)
	return result.Detail, err
}

func waitReadiness(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, budget waitBudget, pollInterval time.Duration, progress io.Writer) (ReadinessResult, error) {
	if len(extension.Spec.Readiness.Checks) == 0 {
		return ReadinessResult{Ready: true, Detail: "no readiness checks declared"}, nil
	}
	var result ReadinessResult
	last, err := pollUntilReady(ctx, budget, pollInterval, progress, true,
		func(checkCtx context.Context) (bool, string, error) {
			var checkErr error
			result, checkErr = ObserveReadiness(checkCtx, runner, kubeconfig, extension)
			return result.Ready, result.Detail, checkErr
		},
		func(diagnosisCtx context.Context, last string, tracker *waitProgress) (string, error) {
			return readinessTimeout(diagnosisCtx, runner, kubeconfig, extension, budget.timeout, last, tracker)
		})
	result.Detail = last
	if err != nil {
		result.Ready = false
	}
	return result, err
}

func readinessTimeout(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, timeout time.Duration, lastObserved string, tracker *waitProgress) (string, error) {
	pending, observed, err := unsatisfiedChecks(ctx, runner, kubeconfig, extension.Spec.Readiness.Checks)
	if err == nil && observed != "" {
		lastObserved = observed
	}
	if len(pending) == 0 {
		return lastObserved, fmt.Errorf("ClusterAddon/%s exhausted its %s overall readiness budget; last observed: %s", extension.Metadata.Name, timeout, lastObserved)
	}
	tracker.line(fmt.Sprintf("diagnosing why ClusterAddon/%s is not ready:", extension.Metadata.Name))
	cause := diagnoseChecks(ctx, runner, kubeconfig, pending, tracker)
	base := fmt.Sprintf("ClusterAddon/%s exhausted its %s overall readiness budget waiting for %s", extension.Metadata.Name, timeout, describeChecks(pending))
	if strings.TrimSpace(cause) != "" {
		return lastObserved, fmt.Errorf("%s: %s", base, cause)
	}
	return lastObserved, fmt.Errorf("%s; last observed: %s", base, lastObserved)
}

const readinessFanout = 4

type readinessOutcome struct {
	ready          bool
	detail         string
	csvObservation *extensionrecords.CSVObservation
	err            error
}

func Ready(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon) (bool, string, error) {
	result, err := ObserveReadiness(ctx, runner, kubeconfig, extension)
	return result.Ready, result.Detail, err
}

func ObserveReadiness(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon) (ReadinessResult, error) {
	outcomes := readinessOutcomes(ctx, runner, kubeconfig, extension.Spec.Readiness.Checks)
	result := ReadinessResult{Ready: true}
	for _, outcome := range outcomes {
		if outcome.csvObservation != nil {
			result.CSVObservations = append(result.CSVObservations, *outcome.csvObservation)
		}
	}
	var observed []string
	for _, outcome := range outcomes {
		if outcome.err != nil {
			result.Ready = false
			result.Detail = strings.Join(append(observed, outcome.detail), "; ")
			return result, outcome.err
		}
		observed = append(observed, outcome.detail)
		if !outcome.ready {
			result.Ready = false
			result.Detail = strings.Join(observed, "; ")
			return result, nil
		}
	}
	result.Detail = strings.Join(observed, "; ")
	return result, nil
}

func readinessOutcomes(ctx context.Context, runner OCRunner, kubeconfig string, checks []v1alpha1.ClusterAddonReadinessCheck) []readinessOutcome {
	outcomes := make([]readinessOutcome, len(checks))
	if len(checks) == 0 {
		return outcomes
	}
	if len(checks) == 1 {
		outcomes[0] = checkReady(ctx, runner, kubeconfig, checks[0])
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
			outcomes[index] = checkReady(ctx, runner, kubeconfig, checks[index])
		}(i)
	}
	wg.Wait()
	return outcomes
}

func checkReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonReadinessCheck) readinessOutcome {
	switch {
	case check.CSVSucceeded != nil:
		ready, detail, observation, err := observeCSV(ctx, runner, kubeconfig, check.CSVSucceeded.Namespace, check.CSVSucceeded.Subscription)
		return readinessOutcome{ready: ready, detail: detail, csvObservation: observation, err: err}
	case check.Condition != nil:
		ready, detail, err := conditionReady(ctx, runner, kubeconfig, *check.Condition)
		return readinessOutcome{ready: ready, detail: detail, err: err}
	case check.ResourceExists != nil:
		exists := check.ResourceExists
		resource := resourceArg(exists.APIVersion, exists.Kind) + "/" + exists.Name
		_, err := getResource(ctx, runner, kubeconfig, exists.APIVersion, exists.Kind, exists.Namespace, exists.Name)
		if err != nil {
			return readinessOutcome{detail: resource + " NotFound"}
		}
		return readinessOutcome{ready: true, detail: resource + " Exists"}
	default:
		return readinessOutcome{err: fmt.Errorf("readiness check must set exactly one arm")}
	}
}

func csvSucceeded(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription string) (bool, string, error) {
	ready, detail, _, err := observeCSV(ctx, runner, kubeconfig, namespace, subscription)
	return ready, detail, err
}

func observeCSV(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription string) (bool, string, *extensionrecords.CSVObservation, error) {
	subscriptionResource := "subscription.operators.coreos.com/" + subscription
	sub, err := getNamedResource(ctx, runner, kubeconfig, "subscription.operators.coreos.com", namespace, subscription)
	if err != nil {
		return false, subscriptionResource + " Unavailable", nil, nil
	}
	csv := nestedString(sub, "status", "installedCSV")
	if csv == "" {
		return false, subscriptionResource + " Pending", nil, nil
	}
	csvResource := "clusterserviceversion.operators.coreos.com/" + csv
	csvObj, err := getNamedResource(ctx, runner, kubeconfig, "clusterserviceversion.operators.coreos.com", namespace, csv)
	if err != nil {
		return false, csvResource + " Unavailable", nil, nil
	}
	phase := nestedString(csvObj, "status", "phase")
	if phase != "Succeeded" {
		return false, csvResource + " " + orUnknown(phase), nil, nil
	}
	version := strings.TrimSpace(nestedString(csvObj, "spec", "version"))
	if version == "" {
		return false, csvResource + " Succeeded (spec.version unavailable)", nil, nil
	}
	observation := &extensionrecords.CSVObservation{
		Namespace:    namespace,
		Subscription: subscription,
		InstalledCSV: csv,
		Version:      version,
		ObservedAt:   time.Now().UTC(),
	}
	return true, csvResource + " Succeeded", observation, nil
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
