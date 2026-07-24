package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
	"github.com/crmarques/bootwright/internal/host/safefs"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"go.yaml.in/yaml/v3"
)

type addonHookExecutor struct {
	stdout, stderr io.Writer
	runsDir        string
	runID          string
	taskID         string
	logPath        string
	kubeconfig     string
	opts           RunOptions
	state          v1alpha1.State
	plan           extensionPlanView
	ocRunner       extensionoc.OCRunner
	runnerFactory  ApplyTaskRunnerFactory
	binding        v1alpha1.ClusterAddonBinding
	inputs         []v1alpha1.ClusterAddonBindingInput
}

type extensionPlanView struct {
	Name    string
	Cluster string
	Addon   v1alpha1.ClusterAddon
	Policy  addons.ClusterAddonPolicy
}

func newAddonHookExecutor(stdout, stderr io.Writer, runsDir, runID, kubeconfig string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) *addonHookExecutor {
	plan := extensionPlanView{Name: task.Extension.Name, Cluster: task.Extension.Cluster, Addon: task.Extension.Extension, Policy: task.Extension.Policy}
	binding, inputs := addonBindingInputs(task.State, task.Extension.Binding, plan.Name)
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	return &addonHookExecutor{
		stdout:        stdout,
		stderr:        stderr,
		runsDir:       runsDir,
		runID:         runID,
		taskID:        task.Entry.ID,
		logPath:       logPath,
		kubeconfig:    kubeconfig,
		opts:          opts,
		state:         task.State,
		plan:          plan,
		ocRunner:      extensionoc.CommandRunner{LogPath: logPath, Stdout: stdout, Stderr: stderr, RedactLog: true},
		runnerFactory: runnerFactory,
		binding:       binding,
		inputs:        inputs,
	}
}

func (e *addonHookExecutor) Run(ctx context.Context, lifecycle string) ([]string, error) {
	var observed []string
	for _, hook := range hooks.At(e.plan.Addon, lifecycle) {
		hookObserved, err := e.runHook(ctx, hook)
		observed = append(observed, hookObserved...)
		if err == nil {
			continue
		}
		detail := conciseApplyTaskFailure(err.Error())
		if recordErr := extensionrecords.SetHook(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, hook.Name, extensionrecords.HookRecord{
			Lifecycle: lifecycle,
			Status:    extensionrecords.RecordStatusFailed,
			RanAt:     time.Now().UTC(),
			LastError: detail,
		}); recordErr != nil {
			fmt.Fprintf(e.stderr, "warning: could not record failed hook %s (%s): %v; a prior ready record may skip it on the next run\n", hook.Name, lifecycle, recordErr)
		}
		if v1alpha1.ClusterAddonHookFailureMode(hook) == v1alpha1.ProvisioningPlaybookFailureContinue {
			fmt.Fprintf(e.stderr, "hook %s (%s) failed, continuing: %s\n", hook.Name, lifecycle, detail)
			continue
		}
		return observed, &extensionoc.HookError{Hook: hook.Name, Lifecycle: lifecycle, Detail: detail}
	}
	return observed, nil
}

func (e *addonHookExecutor) runHook(ctx context.Context, hook v1alpha1.ClusterAddonHook) ([]string, error) {
	digest := e.hookDigest(hook)
	if v1alpha1.ClusterAddonHookRun(hook) == v1alpha1.ProvisioningPlaybookRunOnChange && e.hookConverged(hook.Name, digest) {
		return nil, nil
	}
	hookRoot := filepath.Join(e.runsDir, "history", e.runID, "tasks", e.taskID, "hooks", hook.Name)
	defer os.RemoveAll(hookRoot)
	outputs := map[string]string{}
	if hook.Playbook != "" {
		captured, err := e.runHookPlaybook(ctx, hook, hookRoot)
		if err != nil {
			return nil, err
		}
		outputs = captured
	}
	observed, err := e.applyHookManifests(ctx, hook, outputs)
	if err != nil {
		return observed, err
	}
	if err := e.reclaimSecretHookOutputs(hook); err != nil {
		return observed, err
	}
	return observed, extensionrecords.SetHook(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, hook.Name, extensionrecords.HookRecord{
		Lifecycle: hook.Lifecycle,
		Status:    extensionrecords.RecordStatusReady,
		Digest:    digest,
		RanAt:     time.Now().UTC(),
	})
}

func (e *addonHookExecutor) hookDigest(hook v1alpha1.ClusterAddonHook) string {
	projection := struct {
		Content    string                              `json:"content"`
		Inputs     []v1alpha1.ClusterAddonBindingInput `json:"inputs,omitempty"`
		Target     v1alpha1.ClusterAddonHookTarget     `json:"target"`
		Manifests  []v1alpha1.ClusterAddonHookManifest `json:"manifests,omitempty"`
		ExtraVars  map[string]any                      `json:"extraVars,omitempty"`
		SecretRefs []v1alpha1.SecretRef                `json:"secretRefs,omitempty"`
		Outputs    []v1alpha1.ClusterAddonHookOutput   `json:"outputs,omitempty"`
	}{
		Content:    hooks.ContentDigest(e.plan.Addon.SourcePath, hook),
		Inputs:     e.inputs,
		Target:     hook.Target,
		Manifests:  hook.Manifests,
		ExtraVars:  hook.ExtraVars,
		SecretRefs: hook.SecretRefs,
		Outputs:    hook.Outputs,
	}
	data, _ := json.Marshal(projection)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (e *addonHookExecutor) hookConverged(name, digest string) bool {
	record, found, err := extensionrecords.LoadRecord(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name)
	if err != nil || !found {
		return false
	}
	hook, ok := record.Hooks[name]
	return ok && hook.Status == extensionrecords.RecordStatusReady && hook.Digest == digest
}

func (e *addonHookExecutor) applyHookManifests(ctx context.Context, hook v1alpha1.ClusterAddonHook, outputs map[string]string) ([]string, error) {
	if len(hook.Manifests) == 0 {
		return nil, nil
	}
	renderDir := filepath.Join(e.runsDir, "history", e.runID, "tasks", e.taskID, "hooks", hook.Name, "manifests")
	var observed []string
	for i, manifest := range hook.Manifests {
		object, err := e.renderHookManifest(hook, manifest, outputs)
		if err != nil {
			return observed, err
		}
		data, err := yaml.Marshal(object)
		if err != nil {
			return observed, fmt.Errorf("marshal hook manifest %s: %w", manifest.Path, err)
		}
		path := filepath.Join(renderDir, fmt.Sprintf("%02d-%s", i, filepath.Base(manifest.Path)))
		if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
			return observed, err
		}
		if _, err := e.ocRunner.Run(ctx, e.kubeconfig, extensionoc.ApplyArgs(e.plan.Policy, path), nil); err != nil {
			return observed, err
		}
		observed = append(observed, hookManifestResourceID(object))
		if manifest.ReclaimRendered {
			_ = os.Remove(path)
		}
	}
	return observed, nil
}

func hookManifestResourceID(object map[string]any) string {
	metadata, _ := object["metadata"].(map[string]any)
	kind, _ := object["kind"].(string)
	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)
	return extensionrender.ObservedResourceID(kind, namespace, name)
}

func (e *addonHookExecutor) renderHookManifest(hook v1alpha1.ClusterAddonHook, manifest v1alpha1.ClusterAddonHookManifest, outputs map[string]string) (map[string]any, error) {
	path := filepath.Join(filepath.Dir(e.plan.Addon.SourcePath), manifest.Path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hook manifest %s: %w", manifest.Path, err)
	}
	return hooks.RenderManifest(raw, func(token hooks.Token) (string, error) {
		return e.resolveManifestToken(hook, token, outputs)
	})
}

func (e *addonHookExecutor) resolveManifestToken(hook v1alpha1.ClusterAddonHook, token hooks.Token, outputs map[string]string) (string, error) {
	switch token.Kind {
	case hooks.TokenCluster:
		return e.plan.Cluster, nil
	case hooks.TokenOutput:
		value, ok := outputs[token.Arg]
		if !ok {
			return "", fmt.Errorf("hook %s manifest references unknown output %q", hook.Name, token.Arg)
		}
		return value, nil
	case hooks.TokenInput:
		return e.resolveInputToken(token.Arg)
	case hooks.TokenSecret:
		return e.resolveSecretToken(token.Arg)
	case hooks.TokenExportDetails:
		return e.resolveExportDetailsToken(token.Arg)
	default:
		return "", fmt.Errorf("hook %s manifest has unknown token kind %q", hook.Name, token.Kind)
	}
}

func (e *addonHookExecutor) resolveInputToken(arg string) (string, error) {
	for _, in := range e.inputs {
		if in.Name != arg {
			continue
		}
		return in.Value, nil
	}
	return "", fmt.Errorf("input %q has no value", arg)
}

func (e *addonHookExecutor) resolveSecretToken(name string) (string, error) {
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	data, err := store.Read(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary})
	if err != nil {
		return "", fmt.Errorf("read secret %q for hook manifest: %w", name, err)
	}
	return string(data), nil
}

func addonBindingInputs(state v1alpha1.State, bindingName, addonName string) (v1alpha1.ClusterAddonBinding, []v1alpha1.ClusterAddonBindingInput) {
	for _, binding := range state.ClusterAddonBindings {
		if binding.Metadata.Name != bindingName {
			continue
		}
		for _, addon := range addoninputs.EffectiveBindingAddons(state, binding) {
			if addon.AddonRef.Name == addonName {
				return binding, addon.Inputs
			}
		}
		return binding, nil
	}
	return v1alpha1.ClusterAddonBinding{}, nil
}

func hookExtraVarPairs(hook v1alpha1.ClusterAddonHook, addonName, cluster, outputsDir, secretsDir, kubeconfig string, refs map[string]any, inputs []v1alpha1.ClusterAddonBindingInput) ([]string, error) {
	pairs := []string{
		"bootwright_hook_name=" + hook.Name,
		"bootwright_hook_lifecycle=" + hook.Lifecycle,
		"bootwright_addon_name=" + addonName,
		"bootwright_bound_cluster=" + cluster,
		"bootwright_hook_outputs_dir=" + outputsDir,
		"bootwright_hook_secrets_dir=" + secretsDir,
		"bootwright_kubeconfig=" + kubeconfig,
	}
	refsPair, err := jsonVarPair("bootwright_hook_refs", refs)
	if err != nil {
		return nil, fmt.Errorf("hook %s: %w", hook.Name, err)
	}
	pairs = append(pairs, refsPair)
	inputsPair, err := jsonVarPair("bootwright_hook_inputs", bindingInputValues(inputs))
	if err != nil {
		return nil, fmt.Errorf("hook %s: %w", hook.Name, err)
	}
	pairs = append(pairs, inputsPair)
	if len(hook.ExtraVars) > 0 {
		data, err := json.Marshal(hook.ExtraVars)
		if err != nil {
			return nil, fmt.Errorf("hook %s extraVars: %w", hook.Name, err)
		}
		pairs = append(pairs, string(data))
	}
	return pairs, nil
}

func bindingInputValues(inputs []v1alpha1.ClusterAddonBindingInput) map[string]any {
	out := map[string]any{}
	for _, input := range inputs {
		out[input.Name] = input.Value
	}
	return out
}

func jsonVarPair(name string, value any) (string, error) {
	data, err := json.Marshal(map[string]any{name: value})
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", name, err)
	}
	return string(data), nil
}

func hookSecretNames(hook v1alpha1.ClusterAddonHook) []string {
	names := make([]string, 0, len(hook.SecretRefs))
	for _, ref := range hook.SecretRefs {
		names = append(names, ref.Name)
	}
	return names
}

func hookReferencedClusters(state v1alpha1.State, binding extensionplan.BindingPlan, addonName string, addon v1alpha1.ClusterAddon) (containers, storage []string) {
	containers = []string{binding.Cluster}
	if len(addon.Spec.Hooks) == 0 {
		return containers, nil
	}
	_, inputs := addonBindingInputs(state, binding.Binding, addonName)
	for _, hook := range addon.Spec.Hooks {
		c, s := hooks.TargetClusters(state, addon, binding.Cluster, hook, inputs)
		containers = appendUniqueStrings(containers, c...)
		storage = appendUniqueStrings(storage, s...)
	}
	return containers, storage
}

func hookCrossClusterDependencies(state v1alpha1.State, binding extensionplan.BindingPlan, addonName string, addon v1alpha1.ClusterAddon, installPhasePlanned bool, storageDepsByCluster map[string][]string) []string {
	if len(addon.Spec.Hooks) == 0 {
		return nil
	}
	_, inputs := addonBindingInputs(state, binding.Binding, addonName)
	var deps []string
	for _, hook := range addon.Spec.Hooks {
		containers, storage := hooks.TargetClusters(state, addon, binding.Cluster, hook, inputs)
		for _, name := range storage {
			deps = appendUniqueStrings(deps, storageDepsByCluster[name]...)
		}
		if installPhasePlanned {
			for _, name := range containers {
				if name != binding.Cluster {
					deps = appendUniqueStrings(deps, "wait."+name)
				}
			}
		}
	}
	return deps
}
