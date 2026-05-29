package oc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/extensions/records"
	extensionrender "github.com/crmarques/bootwright/internal/extensions/render"
)

type TaskResult struct {
	Skipped bool
	Reason  string
}

type RunConfig struct {
	ClustersDir  string
	Kubeconfig   string
	RunID        string
	StartedAt    time.Time
	PollInterval time.Duration
}

func Apply(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (TaskResult, error) {
	if err := requireKubeconfig(cfg.Kubeconfig); err != nil {
		return TaskResult{}, err
	}
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy)
	if err != nil {
		return TaskResult{}, err
	}
	if ready, _, err := Ready(ctx, runner, cfg.Kubeconfig, plan.Extension); err == nil && ready {
		record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
		if err != nil {
			return TaskResult{}, err
		}
		if found && record.DesiredHash == hash {
			return TaskResult{Skipped: true, Reason: "extension already ready for desired inputs"}, nil
		}
	}
	now := time.Now().UTC()
	record := extensionrecords.Record{
		Cluster:     plan.Cluster,
		Extension:   plan.Name,
		DesiredHash: hash,
		Status:      extensionrecords.RecordStatusApplying,
		Phase:       extensionrecords.RecordPhaseApplying,
		RunID:       cfg.RunID,
		StartedAt:   cfg.StartedAt.UTC(),
		UpdatedAt:   now,
	}
	if err := extensionrecords.SaveRecord(cfg.ClustersDir, record); err != nil {
		return TaskResult{}, err
	}
	observed, err := applyExtension(ctx, runner, cfg.Kubeconfig, plan)
	now = time.Now().UTC()
	record.UpdatedAt = now
	record.ObservedResources = observed
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		record.LastObserved = err.Error()
		_ = extensionrecords.SaveRecord(cfg.ClustersDir, record)
		return TaskResult{}, err
	}
	record.Status = extensionrecords.RecordStatusWaiting
	record.Phase = extensionrecords.RecordPhaseApplied
	record.AppliedAt = &now
	if err := extensionrecords.SaveRecord(cfg.ClustersDir, record); err != nil {
		return TaskResult{}, err
	}
	return TaskResult{}, nil
}

func Wait(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (TaskResult, error) {
	if err := requireKubeconfig(cfg.Kubeconfig); err != nil {
		return TaskResult{}, err
	}
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy)
	if err != nil {
		return TaskResult{}, err
	}
	record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
	if err != nil {
		return TaskResult{}, err
	}
	if found && record.DesiredHash == hash && record.Status == extensionrecords.RecordStatusReady {
		ready, _, err := Ready(ctx, runner, cfg.Kubeconfig, plan.Extension)
		if err == nil && ready {
			return TaskResult{Skipped: true, Reason: "extension already ready for desired inputs"}, nil
		}
	}
	if !found {
		record = extensionrecords.Record{
			Cluster:     plan.Cluster,
			Extension:   plan.Name,
			DesiredHash: hash,
			RunID:       cfg.RunID,
			StartedAt:   cfg.StartedAt.UTC(),
		}
	}
	record.Status = extensionrecords.RecordStatusWaiting
	record.Phase = extensionrecords.RecordPhaseWaiting
	record.UpdatedAt = time.Now().UTC()
	if err := extensionrecords.SaveRecord(cfg.ClustersDir, record); err != nil {
		return TaskResult{}, err
	}
	last, err := WaitReady(ctx, runner, cfg.Kubeconfig, plan.Extension, cfg.PollInterval)
	record.UpdatedAt = time.Now().UTC()
	record.LastObserved = last
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		_ = extensionrecords.SaveRecord(cfg.ClustersDir, record)
		return TaskResult{}, err
	}
	record.Status = extensionrecords.RecordStatusReady
	record.Phase = extensionrecords.RecordPhaseComplete
	if err := extensionrecords.SaveRecord(cfg.ClustersDir, record); err != nil {
		return TaskResult{}, err
	}
	return TaskResult{}, nil
}

func applyExtension(ctx context.Context, runner OCRunner, kubeconfig string, plan extensionplan.ExtensionPlan) ([]string, error) {
	switch plan.Extension.Spec.Type {
	case v1alpha1.ClusterExtensionTypeOLMOperator:
		resources, err := extensionrender.OLMResources(plan.Extension)
		if err != nil {
			return nil, err
		}
		var observed []string
		for _, resource := range resources {
			if _, err := runner.Run(ctx, kubeconfig, applyArgs(plan.Policy, "-"), resource.Content); err != nil {
				return observed, err
			}
			observed = append(observed, extensionrender.ObservedResourceID(resource.Kind, resource.Namespace, resource.Name))
		}
		return observed, nil
	case v1alpha1.ClusterExtensionTypeManifestSet:
		var observed []string
		for _, manifest := range plan.Extension.Spec.ManifestSet.Manifests {
			path := extensionrender.ManifestPath(plan.Extension, manifest)
			if _, err := runner.Run(ctx, kubeconfig, applyArgs(plan.Policy, path), nil); err != nil {
				return observed, err
			}
			observed = append(observed, "Manifest/"+manifest.Path)
		}
		return observed, nil
	default:
		return nil, fmt.Errorf("ClusterExtension/%s spec.type %q is not executable", plan.Name, plan.Extension.Spec.Type)
	}
}

func applyArgs(policy v1alpha1.ClusterExtensionPolicy, file string) []string {
	args := []string{"apply", "-f", file}
	if policy.UseServerSideApply() {
		args = append(args, "--server-side")
	}
	if strings.TrimSpace(policy.FieldManager) != "" {
		args = append(args, "--field-manager", policy.FieldManager)
	}
	return args
}

func requireKubeconfig(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("kubeconfig path is required")
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("kubeconfig %s is missing", path)
		}
		return fmt.Errorf("stat kubeconfig %s: %w", path, err)
	}
	return nil
}

func WaitReady(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterExtension, pollInterval time.Duration) (string, error) {
	timeout, err := time.ParseDuration(extension.Spec.Readiness.Timeout)
	if err != nil {
		return "", err
	}
	if len(extension.Spec.Readiness.Checks) == 0 {
		return "no readiness checks declared", nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if pollInterval <= 0 {
		pollInterval = WaitInterval(timeout)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		ready, observed, err := Ready(ctx, runner, kubeconfig, extension)
		if ready && err == nil {
			return observed, nil
		}
		if observed != "" {
			last = observed
		} else if err != nil {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return last, fmt.Errorf("ClusterExtension/%s readiness timed out after %s; last observed: %s", extension.Metadata.Name, timeout, last)
		case <-ticker.C:
		}
	}
}

func Ready(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterExtension) (bool, string, error) {
	var observed []string
	for _, check := range extension.Spec.Readiness.Checks {
		ready, detail, err := checkReady(ctx, runner, kubeconfig, check)
		if err != nil {
			return false, strings.Join(append(observed, detail), "; "), err
		}
		observed = append(observed, detail)
		if !ready {
			return false, strings.Join(observed, "; "), nil
		}
	}
	return true, strings.Join(observed, "; "), nil
}

func checkReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterExtensionReadinessCheck) (bool, string, error) {
	switch check.Type {
	case v1alpha1.ClusterExtensionReadinessCSVSucceeded:
		return csvSucceeded(ctx, runner, kubeconfig, check.Namespace, check.Subscription)
	case v1alpha1.ClusterExtensionReadinessCondition:
		return conditionReady(ctx, runner, kubeconfig, check)
	case v1alpha1.ClusterExtensionReadinessResourceExists:
		_, err := getResource(ctx, runner, kubeconfig, check.APIVersion, check.Kind, check.Namespace, check.Name)
		if err != nil {
			return false, fmt.Sprintf("%s/%s not found", check.Kind, check.Name), nil
		}
		return true, fmt.Sprintf("%s/%s exists", check.Kind, check.Name), nil
	default:
		return false, "", fmt.Errorf("unsupported readiness check %q", check.Type)
	}
}

func csvSucceeded(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription string) (bool, string, error) {
	sub, err := getNamedResource(ctx, runner, kubeconfig, "subscription.operators.coreos.com", namespace, subscription)
	if err != nil {
		return false, fmt.Sprintf("Subscription/%s/%s unavailable", namespace, subscription), nil
	}
	csv := nestedString(sub, "status", "installedCSV")
	if csv == "" {
		return false, fmt.Sprintf("Subscription/%s/%s installedCSV empty", namespace, subscription), nil
	}
	csvObj, err := getNamedResource(ctx, runner, kubeconfig, "clusterserviceversion.operators.coreos.com", namespace, csv)
	if err != nil {
		return false, fmt.Sprintf("CSV/%s/%s unavailable", namespace, csv), nil
	}
	phase := nestedString(csvObj, "status", "phase")
	if phase != "Succeeded" {
		return false, fmt.Sprintf("CSV/%s/%s phase=%s", namespace, csv, phase), nil
	}
	return true, fmt.Sprintf("CSV/%s/%s Succeeded", namespace, csv), nil
}

func conditionReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterExtensionReadinessCheck) (bool, string, error) {
	if check.Condition == nil {
		return false, "", fmt.Errorf("condition readiness check is missing condition")
	}
	obj, err := getResource(ctx, runner, kubeconfig, check.APIVersion, check.Kind, check.Namespace, check.Name)
	if err != nil {
		return false, fmt.Sprintf("%s/%s unavailable", check.Kind, check.Name), nil
	}
	conditions, _ := nestedValue(obj, "status", "conditions").([]any)
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(condition["type"]) == check.Condition.Type && stringValue(condition["status"]) == check.Condition.Status {
			return true, fmt.Sprintf("%s/%s condition %s=%s", check.Kind, check.Name, check.Condition.Type, check.Condition.Status), nil
		}
	}
	return false, fmt.Sprintf("%s/%s condition %s not %s", check.Kind, check.Name, check.Condition.Type, check.Condition.Status), nil
}

func getResource(ctx context.Context, runner OCRunner, kubeconfig, apiVersion, kind, namespace, name string) (map[string]any, error) {
	return getNamedResource(ctx, runner, kubeconfig, resourceArg(apiVersion, kind), namespace, name)
}

func getNamedResource(ctx context.Context, runner OCRunner, kubeconfig, resource, namespace, name string) (map[string]any, error) {
	args := []string{"get", resource, name, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	out, err := runner.Run(ctx, kubeconfig, args, nil)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, fmt.Errorf("decode oc get %s/%s: %w", resource, name, err)
	}
	return decoded, nil
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

func nestedString(m map[string]any, path ...string) string {
	return stringValue(nestedValue(m, path...))
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func nestedValue(m map[string]any, path ...string) any {
	var current any = m
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}
