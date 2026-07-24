package oc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

type HookRunner interface {
	Run(ctx context.Context, lifecycle string) ([]string, error)
}

type HookError struct {
	Hook      string
	Lifecycle string
	Detail    string
}

func (e *HookError) Error() string {
	return fmt.Sprintf("hook %q (%s) failed: %s", e.Hook, e.Lifecycle, e.Detail)
}

func (e *HookError) summary() string { return e.Error() }

type EffectRunner interface {
	Run(ctx context.Context) error
}

type EffectError struct {
	Effect string
	Input  string
	Detail string
}

func (e *EffectError) Error() string {
	return fmt.Sprintf("input effect %q (input %q) failed: %s", e.Effect, e.Input, e.Detail)
}

func (e *EffectError) summary() string { return e.Error() }

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
	ReadRunner   OCRunner
	Hooks        HookRunner
	Effects      EffectRunner
	Progress     io.Writer
}

func (c RunConfig) readRunner(fallback OCRunner) OCRunner {
	if c.ReadRunner != nil {
		return c.ReadRunner
	}
	return fallback
}

func (c RunConfig) runHooks(ctx context.Context, lifecycle string) ([]string, error) {
	if c.Hooks == nil {
		return nil, nil
	}
	return c.Hooks.Run(ctx, lifecycle)
}

func (c RunConfig) runEffects(ctx context.Context) error {
	if c.Effects == nil {
		return nil
	}
	return c.Effects.Run(ctx)
}

func hasActiveGlobalPullSecretMergeEffect(plan extensionplan.ExtensionPlan) bool {
	supplied := map[string]bool{}
	for _, input := range plan.Inputs {
		if input.Value != "" {
			supplied[input.Name] = true
		}
	}
	for _, input := range plan.Extension.Spec.Accepts.Inputs {
		if !supplied[input.Name] {
			continue
		}
		for _, effect := range input.Effects {
			if effect.GlobalPullSecretMerge != nil {
				return true
			}
		}
	}
	return false
}

func planDesiredHash(plan extensionplan.ExtensionPlan) (string, error) {
	if plan.DesiredHash != "" {
		return plan.DesiredHash, nil
	}
	return extensionrender.DesiredHash(plan.Extension, plan.Policy, plan.Inputs)
}

func Apply(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (TaskResult, error) {
	if err := requireKubeconfig(cfg.Kubeconfig); err != nil {
		return TaskResult{}, err
	}
	hash, err := planDesiredHash(plan)
	if err != nil {
		return TaskResult{}, err
	}
	if len(plan.Extension.Spec.Readiness.Checks) > 0 &&
		!hooks.HasAlwaysAt(plan.Extension, v1alpha1.ClusterAddonHookPreApply, v1alpha1.ClusterAddonHookPostOperatorReady) &&
		!hasActiveGlobalPullSecretMergeEffect(plan) {
		if ready, _, err := Ready(ctx, cfg.readRunner(runner), cfg.Kubeconfig, plan.Extension); err == nil && ready {
			record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
			if err != nil {
				return TaskResult{}, err
			}
			if found && completeReadyRecord(record, hash) && hooksReady(record, plan.Extension, v1alpha1.ClusterAddonHookPreApply, v1alpha1.ClusterAddonHookPostOperatorReady) {
				return TaskResult{Skipped: true, Reason: "add-on already ready for desired inputs"}, nil
			}
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
	observed, failedID, err := applyExtension(ctx, runner, cfg, plan)
	now = time.Now().UTC()
	record.UpdatedAt = now
	record.ObservedResources = observed
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		var gate *csvGateError
		var catalogGate *catalogGateError
		var hookErr *HookError
		var effectErr *EffectError
		switch {
		case errors.As(err, &gate):
			record.LastObserved = gate.summary()
		case errors.As(err, &catalogGate):
			record.LastObserved = catalogGate.summary()
		case errors.As(err, &hookErr):
			record.LastObserved = hookErr.summary()
		case errors.As(err, &effectErr):
			record.LastObserved = effectErr.summary()
		default:
			record.LastObserved = applyFailureSummary(failedID)
		}
		if saveErr := extensionrecords.SaveRecord(cfg.ClustersDir, record); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
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
	hash, err := planDesiredHash(plan)
	if err != nil {
		return TaskResult{}, err
	}
	record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
	if err != nil {
		return TaskResult{}, err
	}
	if found && completeReadyRecord(record, hash) &&
		hooksReady(record, plan.Extension, v1alpha1.ClusterAddonHookPostReady) &&
		!hooks.HasAlwaysAt(plan.Extension, v1alpha1.ClusterAddonHookPostReady) {
		ready, _, err := Ready(ctx, cfg.readRunner(runner), cfg.Kubeconfig, plan.Extension)
		if err == nil && ready {
			return TaskResult{Skipped: true, Reason: "add-on already ready for desired inputs"}, nil
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
	last, err := WaitReady(ctx, cfg.readRunner(runner), cfg.Kubeconfig, plan.Extension, cfg.PollInterval, cfg.Progress)
	record.UpdatedAt = time.Now().UTC()
	record.LastObserved = last
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		if saveErr := extensionrecords.SaveRecord(cfg.ClustersDir, record); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		return TaskResult{}, err
	}
	postReadyObserved, err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPostReady)
	record.ObservedResources = append(record.ObservedResources, postReadyObserved...)
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		var hookErr *HookError
		if errors.As(err, &hookErr) {
			record.LastObserved = hookErr.summary()
		}
		if saveErr := extensionrecords.SaveRecord(cfg.ClustersDir, record); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		return TaskResult{}, err
	}
	record.Status = extensionrecords.RecordStatusReady
	record.Phase = extensionrecords.RecordPhaseComplete
	if err := extensionrecords.SaveRecord(cfg.ClustersDir, record); err != nil {
		return TaskResult{}, err
	}
	return TaskResult{}, nil
}

func completeReadyRecord(record extensionrecords.Record, hash string) bool {
	return record.DesiredHash == hash &&
		record.Status == extensionrecords.RecordStatusReady &&
		record.Phase == extensionrecords.RecordPhaseComplete
}

func hooksReady(record extensionrecords.Record, extension v1alpha1.ClusterAddon, lifecycles ...string) bool {
	for _, lifecycle := range lifecycles {
		for _, hook := range hooks.At(extension, lifecycle) {
			if v1alpha1.ClusterAddonHookRun(hook) == v1alpha1.ProvisioningPlaybookRunAlways {
				continue
			}
			item, ok := record.Hooks[hook.Name]
			if !ok || item.Status != extensionrecords.RecordStatusReady {
				return false
			}
		}
	}
	return true
}

func applyExtension(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (observed []string, failedID string, err error) {
	kubeconfig := cfg.Kubeconfig
	if err := cfg.runEffects(ctx); err != nil {
		return observed, "", err
	}
	preApplyObserved, err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPreApply)
	observed = append(observed, preApplyObserved...)
	if err != nil {
		return observed, "", err
	}
	switch plan.Extension.Spec.Type {
	case v1alpha1.ClusterAddonTypeOLM:
		catalog, err := extensionrender.CatalogResources(plan.Extension)
		if err != nil {
			return nil, "", err
		}
		observed, failedID, err = applyResources(ctx, runner, kubeconfig, plan.Policy, catalog, observed)
		if err != nil {
			return observed, failedID, err
		}
		if len(catalog) > 0 {
			olm := plan.Extension.Spec.OLM
			if err := waitCatalogSourceReady(ctx, cfg.readRunner(runner), kubeconfig, olm.Subscription.SourceNamespace, olm.CatalogSource.Name, plan.Extension.Spec.Readiness.Timeout, cfg.PollInterval, cfg.Progress); err != nil {
				return observed, "", err
			}
		}
		operator, err := extensionrender.OperatorResources(plan.Extension)
		if err != nil {
			return observed, "", err
		}
		observed, failedID, err = applyResources(ctx, runner, kubeconfig, plan.Policy, operator, observed)
		if err != nil {
			return observed, failedID, err
		}
		custom, err := extensionrender.CustomResources(plan.Extension)
		if err != nil {
			return observed, "", err
		}
		subscriptionOLM := plan.Extension.Spec.OLM
		if err := waitCSVSucceeded(ctx, cfg.readRunner(runner), kubeconfig, subscriptionOLM.Namespace.Name, subscriptionOLM.Subscription.Name, plan.Extension.Spec.Readiness.Timeout, cfg.PollInterval, cfg.Progress); err != nil {
			return observed, "", err
		}
		postOperatorReadyObserved, err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPostOperatorReady)
		observed = append(observed, postOperatorReadyObserved...)
		if err != nil {
			return observed, "", err
		}
		if len(custom) > 0 {
			observed, failedID, err = applyResources(ctx, runner, kubeconfig, plan.Policy, custom, observed)
			if err != nil {
				return observed, failedID, err
			}
		}
		return observed, "", nil
	case v1alpha1.ClusterAddonTypeManifestSet:
		for _, manifest := range plan.Extension.Spec.ManifestSet.Manifests {
			id := "Manifest/" + manifest.Path
			path := extensionrender.ManifestPath(plan.Extension, manifest)
			if _, err := runner.Run(ctx, kubeconfig, ApplyArgs(plan.Policy, path), nil); err != nil {
				return observed, id, err
			}
			observed = append(observed, id)
		}
		return observed, "", nil
	default:
		return nil, "", fmt.Errorf("ClusterAddon/%s spec.type %q is not executable", plan.Name, plan.Extension.Spec.Type)
	}
}

func applyResources(ctx context.Context, runner OCRunner, kubeconfig string, policy addons.ClusterAddonPolicy, resources []extensionrender.ManifestResource, observed []string) ([]string, string, error) {
	for _, resource := range resources {
		id := extensionrender.ObservedResourceID(resource.Kind, resource.Namespace, resource.Name)
		if _, err := runner.Run(ctx, kubeconfig, ApplyArgs(policy, "-"), resource.Content); err != nil {
			return observed, id, err
		}
		observed = append(observed, id)
	}
	return observed, "", nil
}

func waitCSVSucceeded(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription, timeoutStr string, pollInterval time.Duration, progress io.Writer) error {
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
	tracker := startWaitProgress(progress, fmt.Sprintf("waiting for the operator CSV of Subscription/%s/%s to reach Succeeded", namespace, subscription), timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		ready, detail, err := csvSucceeded(ctx, runner, kubeconfig, namespace, subscription)
		if err == nil && ready {
			tracker.done(detail)
			return nil
		}
		if detail != "" {
			last = detail
		} else if err != nil {
			last = err.Error()
		}
		tracker.observe(last)
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			diagCtx, diagCancel := context.WithTimeout(parent, 30*time.Second)
			cause := diagnoseCSVGate(diagCtx, runner, kubeconfig, namespace, subscription, tracker)
			diagCancel()
			return &csvGateError{namespace: namespace, subscription: subscription, timeout: timeout, lastObserved: last, cause: cause}
		case <-ticker.C:
		}
	}
}

func waitCatalogSourceReady(ctx context.Context, runner OCRunner, kubeconfig, namespace, name, timeoutStr string, pollInterval time.Duration, progress io.Writer) error {
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
	tracker := startWaitProgress(progress, fmt.Sprintf("waiting for CatalogSource/%s/%s to reach connectionState READY", namespace, name), timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		ready, detail := catalogSourceReady(ctx, runner, kubeconfig, namespace, name)
		if ready {
			tracker.done(detail)
			return nil
		}
		if detail != "" {
			last = detail
		}
		tracker.observe(last)
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			diagCtx, diagCancel := context.WithTimeout(parent, 30*time.Second)
			cause := diagnoseCatalogGate(diagCtx, runner, kubeconfig, namespace, name, tracker)
			diagCancel()
			return &catalogGateError{namespace: namespace, name: name, timeout: timeout, lastObserved: last, cause: cause}
		case <-ticker.C:
		}
	}
}

func catalogSourceReady(ctx context.Context, runner OCRunner, kubeconfig, namespace, name string) (bool, string) {
	obj, err := getNamedResource(ctx, runner, kubeconfig, "catalogsource.operators.coreos.com", namespace, name)
	if err != nil {
		return false, fmt.Sprintf("CatalogSource/%s/%s unavailable", namespace, name)
	}
	state := nestedString(obj, "status", "connectionState", "lastObservedState")
	if state != "READY" {
		return false, fmt.Sprintf("CatalogSource/%s/%s connectionState=%s", namespace, name, state)
	}
	return true, fmt.Sprintf("CatalogSource/%s/%s READY", namespace, name)
}

type catalogGateError struct {
	namespace    string
	name         string
	timeout      time.Duration
	lastObserved string
	cause        string
}

func (e *catalogGateError) Error() string {
	base := fmt.Sprintf("CatalogSource/%s/%s did not reach connectionState READY within %s", e.namespace, e.name, e.timeout)
	if strings.TrimSpace(e.cause) != "" {
		return base + ": " + e.cause
	}
	return base + "; last observed: " + e.lastObserved
}

func (e *catalogGateError) summary() string {
	base := fmt.Sprintf("CatalogSource/%s/%s did not reach connectionState READY within %s", e.namespace, e.name, e.timeout)
	if strings.TrimSpace(e.cause) != "" {
		return base + ": " + e.cause
	}
	return base + "; see the apply log for details"
}

type csvGateError struct {
	namespace    string
	subscription string
	timeout      time.Duration
	lastObserved string
	cause        string
}

func (e *csvGateError) Error() string {
	base := fmt.Sprintf("operator CSV for Subscription/%s/%s did not reach Succeeded within %s", e.namespace, e.subscription, e.timeout)
	if strings.TrimSpace(e.cause) != "" {
		return base + ": " + e.cause
	}
	return base + "; last observed: " + e.lastObserved
}

func (e *csvGateError) summary() string {
	base := fmt.Sprintf("operator CSV for Subscription/%s/%s did not reach Succeeded within %s", e.namespace, e.subscription, e.timeout)
	if strings.TrimSpace(e.cause) != "" {
		return base + ": " + e.cause
	}
	return base + "; see the apply log for details"
}

func applyFailureSummary(failedID string) string {
	if strings.TrimSpace(failedID) == "" {
		return "oc apply failed; see the apply log for details"
	}
	return fmt.Sprintf("oc apply failed at %s; see the apply log for details", failedID)
}

func ApplyArgs(policy addons.ClusterAddonPolicy, file string) []string {
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
	tracker := startWaitProgress(progress, fmt.Sprintf("waiting for ClusterAddon/%s readiness checks", extension.Metadata.Name), timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		ready, observed, err := Ready(ctx, runner, kubeconfig, extension)
		if ready && err == nil {
			tracker.done(observed)
			return observed, nil
		}
		if observed != "" {
			last = observed
		} else if err != nil {
			last = err.Error()
		}
		tracker.observe(last)
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return last, parent.Err()
			}
			return last, fmt.Errorf("ClusterAddon/%s readiness timed out after %s; last observed: %s", extension.Metadata.Name, timeout, last)
		case <-ticker.C:
		}
	}
}

func Ready(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon) (bool, string, error) {
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

func checkReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonReadinessCheck) (bool, string, error) {
	switch {
	case check.CSVSucceeded != nil:
		return csvSucceeded(ctx, runner, kubeconfig, check.CSVSucceeded.Namespace, check.CSVSucceeded.Subscription)
	case check.Condition != nil:
		return conditionReady(ctx, runner, kubeconfig, *check.Condition)
	case check.ResourceExists != nil:
		exists := check.ResourceExists
		_, err := getResource(ctx, runner, kubeconfig, exists.APIVersion, exists.Kind, exists.Namespace, exists.Name)
		if err != nil {
			return false, fmt.Sprintf("%s/%s not found", exists.Kind, exists.Name), nil
		}
		return true, fmt.Sprintf("%s/%s exists", exists.Kind, exists.Name), nil
	default:
		return false, "", fmt.Errorf("readiness check must set exactly one arm")
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

func conditionReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonConditionReadiness) (bool, string, error) {
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

func parsePositiveDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration %s must be greater than 0", value)
	}
	return duration, nil
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
