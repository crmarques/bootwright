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
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
	"github.com/crmarques/bootwright/internal/addons/steps"
	"github.com/crmarques/bootwright/internal/host/safefs"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"go.yaml.in/yaml/v3"
)

type addonStepExecutor struct {
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

func newAddonStepExecutor(stdout, stderr io.Writer, runsDir, runID, kubeconfig string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) *addonStepExecutor {
	plan := extensionPlanView{Name: task.Extension.Name, Cluster: task.Extension.Cluster, Addon: task.Extension.Extension, Policy: task.Extension.Policy}
	binding, inputs := addonBindingInputs(task.State, task.Extension.Binding, plan.Name)
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	return &addonStepExecutor{
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

func (e *addonStepExecutor) Run(ctx context.Context, lifecycle string) ([]string, error) {
	var observed []string
	for _, step := range steps.At(e.plan.Addon, lifecycle) {
		stepObserved, err := e.runStep(ctx, step)
		observed = append(observed, stepObserved...)
		if err == nil {
			continue
		}
		detail := conciseApplyTaskFailure(err.Error())
		if recordErr := extensionrecords.SetStep(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, step.Name, extensionrecords.StepRecord{
			Lifecycle: lifecycle,
			Status:    extensionrecords.RecordStatusFailed,
			RanAt:     time.Now().UTC(),
			LastError: detail,
		}); recordErr != nil {
			fmt.Fprintf(e.stderr, "warning: could not record failed step %s (%s): %v; a prior ready record may skip it on the next run\n", step.Name, lifecycle, recordErr)
		}
		if v1alpha1.ClusterAddonStepFailureMode(step) == v1alpha1.PlaybookFailureContinue {
			fmt.Fprintf(e.stderr, "step %s (%s) failed, continuing: %s\n", step.Name, lifecycle, detail)
			continue
		}
		return observed, &extensionoc.StepError{Step: step.Name, Lifecycle: lifecycle, Detail: detail}
	}
	return observed, nil
}

func (e *addonStepExecutor) runStep(ctx context.Context, step v1alpha1.ClusterAddonStep) ([]string, error) {
	digest, err := e.stepDigest(step)
	if err != nil {
		return nil, err
	}
	if v1alpha1.ClusterAddonStepRun(step) == v1alpha1.PlaybookRunOnChange && e.stepConverged(step.Name, digest) {
		return nil, nil
	}
	stepRoot := filepath.Join(e.runsDir, "history", e.runID, "tasks", e.taskID, "steps", step.Name)
	defer os.RemoveAll(stepRoot)
	outputs := map[string]string{}
	if step.Playbook != "" {
		captured, err := e.runStepPlaybook(ctx, step, stepRoot)
		if err != nil {
			return nil, err
		}
		outputs = captured
	}
	observed, err := e.applyStepManifests(ctx, step, outputs)
	if err != nil {
		return observed, err
	}
	if err := e.reclaimSecretStepOutputs(step); err != nil {
		return observed, err
	}
	anchor, _ := v1alpha1.ClusterAddonStepAnchor(step)
	return observed, extensionrecords.SetStep(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, step.Name, extensionrecords.StepRecord{
		Lifecycle: anchor,
		Status:    extensionrecords.RecordStatusReady,
		Digest:    digest,
		RanAt:     time.Now().UTC(),
	})
}

func (e *addonStepExecutor) stepDigest(step v1alpha1.ClusterAddonStep) (string, error) {
	content, err := steps.ContentDigest(e.plan.Addon.SourcePath, step)
	if err != nil {
		return "", fmt.Errorf("ClusterAddon/%s step %s: %w; fix or remove the unreadable content so bootwright can prove what would run", e.plan.Name, step.Name, err)
	}
	projection := struct {
		Content    string                              `json:"content"`
		Inputs     []v1alpha1.ClusterAddonBindingInput `json:"inputs,omitempty"`
		Target     v1alpha1.ClusterAddonStepTarget     `json:"target"`
		Manifests  []v1alpha1.ClusterAddonStepManifest `json:"manifests,omitempty"`
		ExtraVars  map[string]any                      `json:"extraVars,omitempty"`
		SecretRefs []v1alpha1.SecretRef                `json:"secretRefs,omitempty"`
		Outputs    []v1alpha1.ClusterAddonStepOutput   `json:"outputs,omitempty"`
	}{
		Content:    content,
		Inputs:     e.inputs,
		Target:     step.Target,
		Manifests:  step.Manifests,
		ExtraVars:  step.ExtraVars,
		SecretRefs: step.SecretRefs,
		Outputs:    step.Outputs,
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("encode ClusterAddon/%s step %s digest input: %w", e.plan.Name, step.Name, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (e *addonStepExecutor) stepConverged(name, digest string) bool {
	record, found, err := extensionrecords.LoadRecord(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name)
	if err != nil || !found {
		return false
	}
	step, ok := record.Steps[name]
	return ok && step.Status == extensionrecords.RecordStatusReady && step.Digest == digest
}

func (e *addonStepExecutor) applyStepManifests(ctx context.Context, step v1alpha1.ClusterAddonStep, outputs map[string]string) ([]string, error) {
	if len(step.Manifests) == 0 {
		return nil, nil
	}
	renderDir := filepath.Join(e.runsDir, "history", e.runID, "tasks", e.taskID, "steps", step.Name, "manifests")
	var observed []string
	for i, manifest := range step.Manifests {
		object, err := e.renderStepManifest(step, manifest, outputs)
		if err != nil {
			return observed, err
		}
		data, err := yaml.Marshal(object)
		if err != nil {
			return observed, fmt.Errorf("marshal step manifest %s: %w", manifest.Path, err)
		}
		path := filepath.Join(renderDir, fmt.Sprintf("%02d-%s", i, filepath.Base(manifest.Path)))
		if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
			return observed, err
		}
		if _, err := e.ocRunner.Run(ctx, e.kubeconfig, extensionoc.ApplyArgs(e.plan.Policy, path), nil); err != nil {
			return observed, err
		}
		observed = append(observed, stepManifestResourceID(object))
		if manifest.ReclaimRendered {
			_ = os.Remove(path)
		}
	}
	return observed, nil
}

func stepManifestResourceID(object map[string]any) string {
	metadata, _ := object["metadata"].(map[string]any)
	kind, _ := object["kind"].(string)
	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)
	return extensionrender.ObservedResourceID(kind, namespace, name)
}

func (e *addonStepExecutor) renderStepManifest(step v1alpha1.ClusterAddonStep, manifest v1alpha1.ClusterAddonStepManifest, outputs map[string]string) (map[string]any, error) {
	path := filepath.Join(filepath.Dir(e.plan.Addon.SourcePath), manifest.Path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read step manifest %s: %w", manifest.Path, err)
	}
	return steps.RenderManifest(raw, func(token steps.Token) (string, error) {
		return e.resolveManifestToken(step, token, outputs)
	})
}

func (e *addonStepExecutor) resolveManifestToken(step v1alpha1.ClusterAddonStep, token steps.Token, outputs map[string]string) (string, error) {
	switch token.Kind {
	case steps.TokenCluster:
		return e.plan.Cluster, nil
	case steps.TokenOutput:
		value, ok := outputs[token.Arg]
		if !ok {
			return "", fmt.Errorf("step %s manifest references unknown output %q", step.Name, token.Arg)
		}
		return value, nil
	case steps.TokenInput:
		return e.resolveInputToken(token.Arg)
	case steps.TokenSecret:
		return e.resolveSecretToken(token.Arg)
	case steps.TokenExportDetails:
		return e.resolveExportDetailsToken(token.Arg)
	default:
		return "", fmt.Errorf("step %s manifest has unknown token kind %q", step.Name, token.Kind)
	}
}

func (e *addonStepExecutor) resolveInputToken(arg string) (string, error) {
	for _, in := range e.inputs {
		if in.Name != arg {
			continue
		}
		return in.Value, nil
	}
	return "", fmt.Errorf("input %q has no value", arg)
}

func (e *addonStepExecutor) resolveSecretToken(name string) (string, error) {
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	data, err := store.Read(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary})
	if err != nil {
		return "", fmt.Errorf("read secret %q for step manifest: %w", name, err)
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

func stepExtraVarPairs(step v1alpha1.ClusterAddonStep, addonName, cluster, outputsDir, secretsDir, kubeconfig string, refs map[string]any, inputs []v1alpha1.ClusterAddonBindingInput) ([]string, error) {
	anchor, _ := v1alpha1.ClusterAddonStepAnchor(step)
	pairs := []string{
		"bootwright_step_name=" + step.Name,
		"bootwright_step_anchor=" + anchor,
		"bootwright_addon_name=" + addonName,
		"bootwright_bound_cluster=" + cluster,
		"bootwright_step_outputs_dir=" + outputsDir,
		"bootwright_step_secrets_dir=" + secretsDir,
		"bootwright_kubeconfig=" + kubeconfig,
	}
	refsPair, err := jsonVarPair("bootwright_step_refs", refs)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", step.Name, err)
	}
	pairs = append(pairs, refsPair)
	inputsPair, err := jsonVarPair("bootwright_step_inputs", bindingInputValues(inputs))
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", step.Name, err)
	}
	pairs = append(pairs, inputsPair)
	if len(step.ExtraVars) > 0 {
		data, err := json.Marshal(step.ExtraVars)
		if err != nil {
			return nil, fmt.Errorf("step %s extraVars: %w", step.Name, err)
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

func stepSecretNames(step v1alpha1.ClusterAddonStep) []string {
	names := make([]string, 0, len(step.SecretRefs))
	for _, ref := range step.SecretRefs {
		names = append(names, ref.Name)
	}
	return names
}

func stepReferencedClusters(state v1alpha1.State, binding extensionplan.BindingPlan, addonName string, addon v1alpha1.ClusterAddon) (containers, storage []string) {
	containers = []string{binding.Cluster}
	if len(addon.Spec.Steps) == 0 {
		return containers, nil
	}
	_, inputs := addonBindingInputs(state, binding.Binding, addonName)
	for _, step := range addon.Spec.Steps {
		c, s := steps.TargetClusters(state, addon, binding.Cluster, step, inputs)
		containers = appendUniqueStrings(containers, c...)
		storage = appendUniqueStrings(storage, s...)
	}
	return containers, storage
}

func stepCrossClusterDependencies(state v1alpha1.State, binding extensionplan.BindingPlan, addonName string, addon v1alpha1.ClusterAddon, installPhasePlanned bool, storageDepsByCluster map[string][]string) []string {
	if len(addon.Spec.Steps) == 0 {
		return nil
	}
	_, inputs := addonBindingInputs(state, binding.Binding, addonName)
	var deps []string
	for _, step := range addon.Spec.Steps {
		containers, storage := steps.TargetClusters(state, addon, binding.Cluster, step, inputs)
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
