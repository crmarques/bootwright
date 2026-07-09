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
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

// HookRunner executes an add-on's hooks at a lifecycle point. The add-on apply
// engine calls it at preApply, postOperatorReady, and postReady; the concrete
// implementation lives in internal/converge/workflow (it needs ansible, secrets,
// and state). A nil HookRunner is a no-op, so add-ons without hooks and unit
// tests need not wire one.
type HookRunner interface {
	Run(ctx context.Context, lifecycle string) error
}

// HookError marks a hook run failure so the caller records a hook-specific,
// non-secret summary rather than naming an oc apply target. Detail must hold no
// secret bytes.
type HookError struct {
	Hook      string
	Lifecycle string
	Detail    string
}

func (e *HookError) Error() string {
	return fmt.Sprintf("hook %q (%s) failed: %s", e.Hook, e.Lifecycle, e.Detail)
}

func (e *HookError) summary() string { return e.Error() }

// EffectRunner executes an add-on's compiled input effects that act on the
// bound cluster before any resource applies (currently the global pull-secret
// merge). The concrete implementation lives in internal/converge/workflow (it
// needs the secret store and binding inputs). A nil EffectRunner is a no-op.
type EffectRunner interface {
	Run(ctx context.Context) error
}

// EffectError marks an input-effect failure so the caller records an
// effect-specific, non-secret summary rather than naming an oc apply target.
// Detail must hold no secret bytes.
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
	// ReadRunner, when set, serves read-only oc calls (readiness polls, the
	// idempotency pre-check, the CSV gate) so their expected NotFound / "no
	// matches for kind" output stays off the console. The mutating oc apply
	// always uses the runner passed to Apply. Nil falls back to that runner.
	ReadRunner OCRunner
	// Hooks, when set, executes the add-on's lifecycle hooks (preApply,
	// postOperatorReady, postReady). Nil disables hooks.
	Hooks HookRunner
	// Effects, when set, executes the add-on's compiled input effects (the
	// global pull-secret merge) before any resource applies. Nil disables them.
	Effects EffectRunner
}

func (c RunConfig) readRunner(fallback OCRunner) OCRunner {
	if c.ReadRunner != nil {
		return c.ReadRunner
	}
	return fallback
}

// runHooks executes the add-on's hooks at the lifecycle, or no-ops when no
// executor is wired (add-ons without hooks, unit tests).
func (c RunConfig) runHooks(ctx context.Context, lifecycle string) error {
	if c.Hooks == nil {
		return nil
	}
	return c.Hooks.Run(ctx, lifecycle)
}

// runEffects executes the add-on's compiled input effects, or no-ops when no
// executor is wired (add-ons without effects, unit tests).
func (c RunConfig) runEffects(ctx context.Context) error {
	if c.Effects == nil {
		return nil
	}
	return c.Effects.Run(ctx)
}

// hasGlobalPullSecretMergeEffect reports whether any accepted input declares
// the global pull-secret merge. Registry credentials must converge on every
// apply (the merge is idempotent and cheap), so such an add-on never takes the
// already-ready short-circuit — the same rule run: always hooks follow.
func hasGlobalPullSecretMergeEffect(extension v1alpha1.ClusterAddon) bool {
	for _, input := range extension.Spec.Accepts.Inputs {
		for _, effect := range input.Effects {
			if effect.Type == v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge {
				return true
			}
		}
	}
	return false
}

func Apply(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (TaskResult, error) {
	if err := requireKubeconfig(cfg.Kubeconfig); err != nil {
		return TaskResult{}, err
	}
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy, plan.Inputs)
	if err != nil {
		return TaskResult{}, err
	}
	// Only a check-backed Ready() is evidence the add-on is still live on-cluster.
	// With no declared checks Ready() is vacuously true, so a matching record would
	// skip forever even after someone deletes the add-on's resources with oc. Fall
	// through and re-apply (oc apply is idempotent) when there are no checks. A
	// run: always hook at preApply/postOperatorReady must run every apply, so it
	// blocks the pre-check short-circuit (applyExtension re-runs, idempotently, and
	// the executor skips unchanged onChange hooks by their own per-hook digest).
	if len(plan.Extension.Spec.Readiness.Checks) > 0 &&
		!hooks.HasAlwaysAt(plan.Extension, v1alpha1.ClusterAddonHookPreApply, v1alpha1.ClusterAddonHookPostOperatorReady) &&
		!hasGlobalPullSecretMergeEffect(plan.Extension) {
		if ready, _, err := Ready(ctx, cfg.readRunner(runner), cfg.Kubeconfig, plan.Extension); err == nil && ready {
			record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
			if err != nil {
				return TaskResult{}, err
			}
			if found && record.DesiredHash == hash {
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
		// The apply error carries the raw oc stdout/stderr, which can echo back
		// user-inlined Secret bytes from the applied manifest. Observed-state
		// records must never hold secret bytes (specs/security.md), so persist a
		// non-secret summary; the full output is kept only in the sanctioned apply
		// log the runner already wrote. A CSV-gate timeout is not an apply failure
		// (the operator resources applied; only the CSV never went Succeeded), so
		// it gets its own summary rather than naming the already-applied
		// Subscription as a failed apply target.
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
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy, plan.Inputs)
	if err != nil {
		return TaskResult{}, err
	}
	record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
	if err != nil {
		return TaskResult{}, err
	}
	if found && record.DesiredHash == hash && record.Status == extensionrecords.RecordStatusReady &&
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
	last, err := WaitReady(ctx, cfg.readRunner(runner), cfg.Kubeconfig, plan.Extension, cfg.PollInterval)
	record.UpdatedAt = time.Now().UTC()
	record.LastObserved = last
	if err != nil {
		record.Status = extensionrecords.RecordStatusFailed
		if saveErr := extensionrecords.SaveRecord(cfg.ClustersDir, record); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		return TaskResult{}, err
	}
	// postReady hooks run once the add-on's readiness checks pass (e.g. a day-2
	// registration). A hookErr from a fail-mode hook fails the task.
	if err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPostReady); err != nil {
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

// applyExtension applies each resource in order. On failure it returns the
// resources already applied plus the identifier of the resource that failed, so
// callers can record a non-secret failure summary without persisting the raw oc
// output (which may echo user-inlined secret bytes).
//
// For OLM add-ons it applies the operator-install set (Namespace, OperatorGroup,
// Subscription) first, then waits for the operator's CSV to reach Succeeded —
// which establishes the operator's CRDs — before applying the custom resources.
// Without that gate the custom resources race the operator install and fail with
// "no matches for kind", forcing the operator to re-run apply to converge.
func applyExtension(ctx context.Context, runner OCRunner, cfg RunConfig, plan extensionplan.ExtensionPlan) (observed []string, failedID string, err error) {
	kubeconfig := cfg.Kubeconfig
	// Input effects run first: a pull-secret merge must land before anything
	// (the shipped catalog, the operator, preApply-hook workloads) pulls images.
	if err := cfg.runEffects(ctx); err != nil {
		return observed, "", err
	}
	if err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPreApply); err != nil {
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
			// A gate timeout returns a typed catalogGateError: the CatalogSource
			// applied successfully, only its registry never reported READY, so the
			// failure summary must not name it as a failed apply target.
			if err := waitCatalogSourceReady(ctx, cfg.readRunner(runner), kubeconfig, olm.Subscription.SourceNamespace, olm.CatalogSource.Name, plan.Extension.Spec.Readiness.Timeout, cfg.PollInterval); err != nil {
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
		// The CSV gate runs when there are custom resources OR a postOperatorReady
		// hook: both need the operator's CRDs established (a hook that produces the
		// external-cluster Secret + StorageCluster runs after the operator is ready).
		hasPostOperatorReady := hooks.HasLifecycle(plan.Extension, v1alpha1.ClusterAddonHookPostOperatorReady)
		if len(custom) > 0 || hasPostOperatorReady {
			olm := plan.Extension.Spec.OLM
			// A gate timeout returns a typed csvGateError (handled by the caller),
			// so failedID stays empty: the operator resources applied successfully
			// and naming the Subscription as a failed apply target would be wrong.
			if err := waitCSVSucceeded(ctx, cfg.readRunner(runner), kubeconfig, olm.Namespace.Name, olm.Subscription.Name, plan.Extension.Spec.Readiness.Timeout, cfg.PollInterval); err != nil {
				return observed, "", err
			}
			if err := cfg.runHooks(ctx, v1alpha1.ClusterAddonHookPostOperatorReady); err != nil {
				return observed, "", err
			}
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
			if _, err := runner.Run(ctx, kubeconfig, applyArgs(plan.Policy, path), nil); err != nil {
				return observed, id, err
			}
			observed = append(observed, id)
		}
		return observed, "", nil
	default:
		return nil, "", fmt.Errorf("ClusterAddon/%s spec.type %q is not executable", plan.Name, plan.Extension.Spec.Type)
	}
}

// applyResources applies each rendered resource via stdin, appending its
// identifier to observed. On failure it returns observed so far plus the failed
// resource's identifier.
func applyResources(ctx context.Context, runner OCRunner, kubeconfig string, policy addons.ClusterAddonPolicy, resources []extensionrender.ManifestResource, observed []string) ([]string, string, error) {
	for _, resource := range resources {
		id := extensionrender.ObservedResourceID(resource.Kind, resource.Namespace, resource.Name)
		if _, err := runner.Run(ctx, kubeconfig, applyArgs(policy, "-"), resource.Content); err != nil {
			return observed, id, err
		}
		observed = append(observed, id)
	}
	return observed, "", nil
}

// waitCSVSucceeded polls until the Subscription's installed CSV reports phase
// Succeeded (the operator's CRDs are then established), bounded by timeout. It
// uses the supplied runner — the caller passes the read/quiet runner so the
// inevitable pre-install NotFound / empty-CSV polls stay off the console.
func waitCSVSucceeded(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription, timeoutStr string, pollInterval time.Duration) error {
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return err
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
		ready, detail, err := csvSucceeded(ctx, runner, kubeconfig, namespace, subscription)
		if err == nil && ready {
			return nil
		}
		if detail != "" {
			last = detail
		} else if err != nil {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return &csvGateError{namespace: namespace, subscription: subscription, timeout: timeout, lastObserved: last}
		case <-ticker.C:
		}
	}
}

// waitCatalogSourceReady polls until the shipped CatalogSource's registry
// reports a READY connection (OLM can then resolve packages from it), bounded
// by the add-on readiness timeout. Like the CSV gate it takes the read/quiet
// runner so the expected pre-READY polls stay off the console.
func waitCatalogSourceReady(ctx context.Context, runner OCRunner, kubeconfig, namespace, name, timeoutStr string, pollInterval time.Duration) error {
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return err
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
		ready, detail := catalogSourceReady(ctx, runner, kubeconfig, namespace, name)
		if ready {
			return nil
		}
		if detail != "" {
			last = detail
		}
		select {
		case <-ctx.Done():
			return &catalogGateError{namespace: namespace, name: name, timeout: timeout, lastObserved: last}
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

// catalogGateError marks a catalog-gate timeout: the CatalogSource applied
// successfully but its registry never reported a READY connection within the
// readiness timeout. Its strings hold only resource identifiers and connection
// state — no user manifest bytes — so the summary is safe to persist.
type catalogGateError struct {
	namespace    string
	name         string
	timeout      time.Duration
	lastObserved string
}

func (e *catalogGateError) Error() string {
	return fmt.Sprintf("CatalogSource/%s/%s did not reach connectionState READY within %s; last observed: %s", e.namespace, e.name, e.timeout, e.lastObserved)
}

func (e *catalogGateError) summary() string {
	return fmt.Sprintf("CatalogSource/%s/%s did not reach connectionState READY within %s; see the apply log for details", e.namespace, e.name, e.timeout)
}

// csvGateError marks a CSV-gate timeout: the operator-install resources applied
// successfully but the operator's CSV never reached Succeeded within the
// readiness timeout. It is distinct from an oc apply failure so callers persist a
// gate-specific summary rather than naming the already-applied Subscription as a
// failed apply target. Its strings hold only resource identifiers and CSV phase
// detail — no user manifest bytes — so the summary is safe to persist.
type csvGateError struct {
	namespace    string
	subscription string
	timeout      time.Duration
	lastObserved string
}

func (e *csvGateError) Error() string {
	return fmt.Sprintf("Subscription/%s/%s operator CSV did not reach Succeeded within %s; last observed: %s", e.namespace, e.subscription, e.timeout, e.lastObserved)
}

func (e *csvGateError) summary() string {
	return fmt.Sprintf("operator CSV for Subscription/%s/%s did not reach Succeeded within %s; see the apply log for details", e.namespace, e.subscription, e.timeout)
}

// applyFailureSummary returns a non-secret one-line description of an apply
// failure for the observed-state record. It names the failed resource (a Kind/
// Namespace/Name or Manifest/path identifier, never secret bytes) and points the
// operator at the apply log for the raw oc output.
func applyFailureSummary(failedID string) string {
	if strings.TrimSpace(failedID) == "" {
		return "oc apply failed; see the apply log for details"
	}
	return fmt.Sprintf("oc apply failed at %s; see the apply log for details", failedID)
}

func applyArgs(policy addons.ClusterAddonPolicy, file string) []string {
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

func WaitReady(ctx context.Context, runner OCRunner, kubeconfig string, extension v1alpha1.ClusterAddon, pollInterval time.Duration) (string, error) {
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
	switch check.Type {
	case v1alpha1.ClusterAddonReadinessCSVSucceeded:
		return csvSucceeded(ctx, runner, kubeconfig, check.Namespace, check.Subscription)
	case v1alpha1.ClusterAddonReadinessCondition:
		return conditionReady(ctx, runner, kubeconfig, check)
	case v1alpha1.ClusterAddonReadinessResourceExists:
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

func conditionReady(ctx context.Context, runner OCRunner, kubeconfig string, check v1alpha1.ClusterAddonReadinessCheck) (bool, string, error) {
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
